package picker

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const limitedLegacy = "LIMITED\talpha\tdocs/, CLAUDE/\tany repository of example-org\n"

// marker is alpha's limit marker.
func (h rig) marker() string {
	return filepath.Join(h.p.StateDir, "declined", "path-limit-alpha")
}

func TestPathLimitYesDropsTheLimitAndAppliesTheTeam(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("limited", limitedLegacy)
	h.OnDeclare("drop-limit", "CHANGED\talpha\n")
	h.Script.Gum = []string{"CHOOSE:Remove alpha's limit now"}
	if err := h.p.offerPathLimits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !h.Fake.Called("env PYTHONPATH=" + h.Host.RootDir + " /usr/bin/python3 -P -m helpers.agent_bus.declare drop-limit --file " + h.Host.HostVars + " --team alpha") {
		t.Fatalf("calls %v", h.Fake.Calls())
	}
	if h.plays() != 1 {
		t.Fatalf("the apply play ran %d times, want once", h.plays())
	}
	if want := "gum choose --header Remove alpha's limit, so any path in its repositories can be named? --selected Remove alpha's limit now " +
		"Remove alpha's limit now Not now (ask again next time) Keep alpha's limit and never ask again"; h.Script.GumLog[0] != want {
		t.Fatalf("gum %q", h.Script.GumLog)
	}
	for _, want := range []string{
		"One question about a team already on this machine, whichever checkout you are in.\n",
		"Team alpha (for any repository of example-org) was made when teams were limited to some folders: its agents may only point each other at files under docs/, CLAUDE/.\n",
	} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
	if _, err := os.Stat(h.marker()); err == nil {
		t.Fatal("a limit that was removed leaves no marker")
	}
}

// declinedOnce is an offer answered never: remembered in marker as never, said, and not made
// again.
func declinedOnce(t *testing.T, h rig, offer func() error, marker, said string) {
	t.Helper()
	if err := offer(); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Clean(marker)); err != nil || string(body) != neverRecord {
		t.Fatalf("marker %q: %v", body, err)
	}
	if !strings.Contains(h.ErrOut.String(), said) {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if err := offer(); err != nil {
		t.Fatal(err)
	}
	if len(h.Script.GumLog) != 1 || h.plays() != 0 {
		t.Fatalf("asked again: %v, plays %d", h.Script.GumLog, h.plays())
	}
}

// notNow is an offer answered not now (or left with Esc): nothing kept, asked again next time.
func notNow(t *testing.T, h rig, offer func() error, marker, answer string) {
	t.Helper()
	h.Script.Gum = []string{answer, answer}
	for range 2 {
		if err := offer(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("not now keeps no marker: %v", err)
		}
	}
	if len(h.Script.GumLog) != 2 || h.plays() != 0 {
		t.Fatalf("not asked again: %v, plays %d", h.Script.GumLog, h.plays())
	}
	if !strings.Contains(h.ErrOut.String(), "Not now: ccy-teams asks again next time.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPathLimitNeverIsRememberedAndNeverAskedAgain(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("limited", limitedLegacy)
	h.Script.Gum = []string{"CHOOSE:Keep alpha's limit and never ask again"}
	declinedOnce(t, h, func() error { return h.p.offerPathLimits(context.Background()) }, h.marker(),
		"Kept. ccy-teams will not ask about alpha's limit again; the menu of alpha's row has Ask me about alpha's limit again.\n")
}

func TestPathLimitNotNowAndEscAreAskedAgain(t *testing.T) {
	for _, answer := range []string{"CHOOSE:Not now (ask again next time)", "CANCEL"} {
		h := newRig(t)
		h.OnDeclare("limited", limitedLegacy)
		notNow(t, h, func() error { return h.p.offerPathLimits(context.Background()) }, h.marker(), answer)
	}
}

// An answer the old yes/no prompt kept (an empty marker) was given with no "later" to choose:
// it is taken as not now, once, and the offer is made again (the owner's feedback on Plan 00201).
func TestAnOldNoIsTakenAsNotNow(t *testing.T) {
	for _, tc := range []struct {
		name, marker, answer string
		offer                func(h rig) error
	}{
		{"description", "description-alpha", "CHOOSE:Not now (ask again next time)", func(h rig) error { return h.p.offerDescriptions(context.Background()) }},
		{"limit", "path-limit-alpha", "CHOOSE:Not now (ask again next time)", func(h rig) error { return h.p.offerPathLimits(context.Background()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRig(t)
			h.OnDeclare("limited", limitedLegacy)
			h.OnDeclare("undescribed", undescribedAlpha)
			marker := filepath.Join(h.p.StateDir, "declined", tc.marker)
			teamtest.MustWrite(t, marker, "")
			h.Script.Gum = []string{tc.answer}
			if err := tc.offer(h); err != nil {
				t.Fatal(err)
			}
			if len(h.Script.GumLog) != 1 {
				t.Fatalf("an old no is asked again: %q", h.Script.GumLog)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("the old marker is gone: %v", err)
			}
		})
	}
}

func TestPathLimitFailuresAreFatal(t *testing.T) {
	h := newRig(t)
	h.OnErr(h.DeclarePrefix("limited"), "declare: cannot read", 78)
	err := h.p.offerPathLimits(context.Background())
	if code, ok := flow.ExitCodeOf(err); !ok || code != term.ExitFailure {
		t.Fatalf("limited failing: %v", err)
	}

	h = newRig(t)
	h.OnDeclare("limited", limitedLegacy)
	h.OnErr(h.DeclarePrefix("drop-limit"), "declare: refused", 78)
	h.Script.Gum = []string{"CHOOSE:Remove alpha's limit now"}
	err = h.p.offerPathLimits(context.Background())
	if code, ok := flow.ExitCodeOf(err); !ok || code != term.ExitFailure || h.plays() != 0 {
		t.Fatalf("drop-limit failing: %v, plays %d", err, h.plays())
	}
}

func TestPathLimitApplyFailureIsSaidAndGoesOn(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("limited", limitedLegacy)
	h.OnDeclare("drop-limit", "CHANGED\talpha\n")
	h.OnErr(h.Host.Play, "play failed", 1)
	h.Script.Gum = []string{"CHOOSE:Remove alpha's limit now"}
	if err := h.p.offerPathLimits(context.Background()); err != nil {
		t.Fatalf("a failed apply does not end the picker: %v", err)
	}
	if !strings.Contains(h.ErrOut.String(), "(ccy-teams apply alpha failed: see above.)\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

const undescribedAlpha = "UNDESCRIBED\talpha\tany repository of example-org\n"

func (h rig) declined() string {
	return filepath.Join(h.p.StateDir, "declined", "description-alpha")
}

func TestDescriptionYesAsksChecksDescribesAndAppliesTheTeam(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("undescribed", undescribedAlpha)
	h.OnDeclare("check", "")
	h.OnDeclare("describe", "CHANGED\tteam alpha description\n")
	h.Script.Gum = []string{"CHOOSE:Describe alpha now", "CHOOSE:Builds the example site"}
	if err := h.p.offerDescriptions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := "gum choose --header Team alpha (for any repository of example-org) has no description. --selected Describe alpha now " +
		"Describe alpha now Not now (ask again next time) Never ask about alpha"; h.Script.GumLog[0] != want {
		t.Fatalf("gum %q", h.Script.GumLog)
	}
	if !strings.HasPrefix(h.Script.GumLog[1], "gum input --header What is team alpha for?") {
		t.Fatalf("gum %q", h.Script.GumLog)
	}
	if !h.Fake.Called(h.DeclarePrefix("describe") + " --file " + h.Host.HostVars + " --team alpha --description Builds the example site") {
		t.Fatalf("calls %v", h.Fake.Calls())
	}
	if h.plays() != 1 {
		t.Fatalf("the apply play ran %d times, want once", h.plays())
	}
	if _, err := os.Stat(h.declined()); err == nil {
		t.Fatal("a description that was written leaves no marker")
	}
}

func TestDescriptionNeverIsRememberedAndNeverAskedAgain(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("undescribed", undescribedAlpha)
	h.Script.Gum = []string{"CHOOSE:Never ask about alpha"}
	declinedOnce(t, h, func() error { return h.p.offerDescriptions(context.Background()) }, h.declined(),
		"Kept. ccy-teams will not ask about alpha's description again; the menu of alpha's row has Ask me to describe alpha again.\n")
}

func TestDescriptionNotNowAndEscAreAskedAgain(t *testing.T) {
	for _, answer := range []string{"CHOOSE:Not now (ask again next time)", "CANCEL"} {
		h := newRig(t)
		h.OnDeclare("undescribed", undescribedAlpha)
		notNow(t, h, func() error { return h.p.offerDescriptions(context.Background()) }, h.declined(), answer)
	}
}

func TestATeamMarkedNeverCanBeAskedAboutAgainFromItsRowMenu(t *testing.T) {
	h := newRig(t)
	teamtest.MustWrite(t, h.declined(), neverRecord)
	teamtest.MustWrite(t, h.marker(), neverRecord)
	r := h.run1(t)
	at := slices.IndexFunc(r.snap.Rows, func(row board.Row) bool { return row.Kind == board.KindTeam && row.Team == "alpha" })
	if at < 0 {
		t.Fatal("no team row for alpha")
	}
	row := r.snap.Rows[at]
	labels := h.labels(r, row)
	for _, want := range []string{"Ask me to describe alpha again", "Ask me about alpha's limit again"} {
		if !slices.Contains(labels, want) {
			t.Fatalf("team menu %q lacks %q", labels, want)
		}
	}
	for _, id := range []string{actAskDescription, actAskLimit} {
		if _, err := r.do(context.Background(), id, row); err != nil {
			t.Fatal(err)
		}
	}
	for _, marker := range []string{h.declined(), h.marker()} {
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("%s is still kept: %v", marker, err)
		}
	}
	if labels := h.labels(r, row); slices.ContainsFunc(labels, func(l string) bool { return strings.HasPrefix(l, "Ask me") }) {
		t.Fatalf("a team not marked never offers no undo: %q", labels)
	}
}

func TestARefusedDescriptionIsAskedAgainWithWhatWasTyped(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("undescribed", undescribedAlpha)
	h.OnDeclare("check", "")
	h.OnErr(h.DeclarePrefix("check")+" --file "+h.Host.HostVars+" --description bad", "declare: --description: nope", 64)
	h.OnDeclare("describe", "CHANGED\tteam alpha description\n")
	h.Script.Gum = []string{"CHOOSE:Describe alpha now", "CHOOSE:bad", "CHOOSE:Good one"}
	if err := h.p.offerDescriptions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "declare: --description: nope") || !strings.HasSuffix(h.Script.GumLog[2], "--value bad --width 0") {
		t.Fatalf("stderr %q gum %q", h.ErrOut.String(), h.Script.GumLog)
	}
	if !h.Fake.Called(h.DeclarePrefix("describe") + " --file " + h.Host.HostVars + " --team alpha --description Good one") {
		t.Fatal("the good one was not written")
	}
}

func TestADescriptionWhoseApplyFailsFailsTheFlowAndNamesTheRetry(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("undescribed", undescribedAlpha)
	h.OnDeclare("check", "")
	h.OnDeclare("describe", "CHANGED\tteam alpha description\n")
	h.OnErr(h.Host.Play, "play failed", 1)
	h.Script.Gum = []string{"CHOOSE:Describe alpha now", "CHOOSE:Builds the example site"}
	err := h.p.offerDescriptions(context.Background())
	code, left := flow.ExitCodeOf(err)
	if !left || code != term.ExitFailure {
		t.Fatalf("a failed apply must end the flow with a failure, got %v", err)
	}
	if !strings.Contains(h.ErrOut.String(), "ccy-teams apply alpha") {
		t.Fatalf("stderr %q lacks the retry", h.ErrOut.String())
	}
}

// The description is written and applied in one hold of the play lock.
func TestADescriptionIsWrittenAndAppliedInOneHoldOfThePlayLock(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("undescribed", undescribedAlpha)
	h.OnDeclare("check", "")
	heldAtWrite := false
	h.Fake.OnFunc(strings.Fields(h.DeclarePrefix("describe")), func(execx.Cmd) (execx.Result, error) {
		heldAtWrite = h.PlayLockHeld(t)
		return execx.Result{Stdout: []byte("CHANGED\tteam alpha description\n")}, nil
	})
	h.Script.Gum = []string{"CHOOSE:Describe alpha now", "CHOOSE:Builds the example site"}
	if err := h.p.offerDescriptions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !heldAtWrite || h.plays() != 1 || h.PlayLockHeld(t) {
		t.Fatalf("held at the write %v, plays %d, held after %v", heldAtWrite, h.plays(), h.PlayLockHeld(t))
	}
}

// The owner's defect: a description given while another play run holds the lock. The wait is a
// screen naming the holder; stopping it writes nothing, so nothing is left saved but not applied.
func TestStoppingTheWaitForThePlayLockLeavesTheDescriptionUnwritten(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("undescribed", undescribedAlpha)
	h.OnDeclare("check", "")
	h.OnDeclare("describe", "CHANGED\tteam alpha description\n")
	h.HoldPlayLock(t)
	h.Script.Gum = []string{"CHOOSE:Describe alpha now", "CHOOSE:Builds the example site"}
	h.Script.Fzf = []string{"|EXIT"}
	h.Script.Columns = 200 // the holder on one line; ops tests the wrap on a narrow terminal
	if err := h.p.offerDescriptions(context.Background()); err != nil {
		t.Fatalf("stopping the wait ends the picker: %v", err)
	}
	if h.Fake.Called(h.DeclarePrefix("describe")) || h.plays() != 0 {
		t.Fatal("the description was written, or the play ran, without the lock")
	}
	if !strings.Contains(h.HeaderOf(0), "Held by run.bash playbooks/imports/play-basic-configs.yml (pid 999)") {
		t.Fatalf("the wait screen does not name the holder: %s", h.HeaderOf(0))
	}
	for _, want := range []string{
		"Stopped waiting for the play lock; nothing was changed.",
		"Team alpha has no description yet: what you typed (Builds the example site) was not saved. ccy-teams asks again the next time it starts.",
	} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Errorf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
	if strings.Contains(h.ErrOut.String(), "in the host_vars") {
		t.Errorf("a description never written is said to be in the host_vars: %q", h.ErrOut.String())
	}
}

func TestStoppingTheWaitForThePlayLockKeepsThePathLimit(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("limited", limitedLegacy)
	h.OnDeclare("drop-limit", "CHANGED\talpha\n")
	h.HoldPlayLock(t)
	h.Script.Gum = []string{"CHOOSE:Remove alpha's limit now"}
	h.Script.Fzf = []string{"|EXIT"}
	if err := h.p.offerPathLimits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.Fake.Called(h.DeclarePrefix("drop-limit")) || h.plays() != 0 {
		t.Fatal("the limit was dropped, or the play ran, without the lock")
	}
	if !strings.Contains(h.ErrOut.String(), "Team alpha keeps its limit for now; ccy-teams asks again the next time it starts.") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestCancellingTheDescriptionQuestionLeavesNothingWritten(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("undescribed", undescribedAlpha)
	h.Script.Gum = []string{"CHOOSE:Describe alpha now", "ABORT"}
	if err := h.p.offerDescriptions(context.Background()); err == nil {
		t.Fatal("Ctrl-C ends the picker")
	}
	if h.Fake.Called(h.DeclarePrefix("describe")) || h.plays() != 0 {
		t.Fatal("something was written")
	}
}

func TestOfferUpdateListsTeamsInOrderAndDeclinedDoesNothing(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("declared", teamtest.DeclaredAlpha+"TEAM\tbeta\t6168\tpresent\tbeta.agent-bus.internal\t-\t-\n")
	h.On("systemctl list-units", teamtest.UnitAlpha, 0)
	h.OnDeclare("status", "STATUS\talpha\tcurrent\t-\nSTATUS\tbeta\tunbuilt\t-\n")
	h.Script.Gum = []string{"NO"}
	if err := h.p.offerUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ErrOut.String(), "Not as declared: beta (not built).\n") || h.plays() != 0 {
		t.Fatalf("stderr %q, plays %d", h.ErrOut.String(), h.plays())
	}
	if want := "gum confirm --default=true Update them now (play-agent-bus.yml)?"; h.Script.GumLog[0] != want {
		t.Fatalf("gum %q", h.Script.GumLog)
	}
}

func TestOfferUpdateSaysNothingWhenAllIsCurrent(t *testing.T) {
	h := newRig(t)
	if err := h.p.offerUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.Script.GumLog) != 0 {
		t.Fatalf("asked: %v", h.Script.GumLog)
	}
}

func TestOfferUpdateFailsWhenStatusCannotBeRead(t *testing.T) {
	h := newRig(t)
	h.OnErr(h.DeclarePrefix("overview"), "declare: bad", 78)
	err := h.p.offerUpdate(context.Background())
	if code, ok := flow.ExitCodeOf(err); !ok || code != term.ExitFailure {
		t.Fatalf("%v", err)
	}
}

func TestOfferUpdateCancelledAndFailedApplyAreKept(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("status", "STATUS\talpha\toutdated\tunstamped\n")
	h.Script.Gum = []string{"CANCEL"}
	err := h.p.offerUpdate(context.Background())
	if code, ok := flow.ExitCodeOf(err); !ok || code != term.ExitCancelled {
		t.Fatalf("%v", err)
	}
	// a play that fails is said and the list goes on
	h = newRig(t)
	h.OnDeclare("status", "STATUS\talpha\toutdated\tunstamped\n")
	h.OnErr(h.Host.Play, "boom", 1)
	h.Script.Gum = []string{"YES"}
	if err = h.p.offerUpdate(context.Background()); err != nil {
		t.Fatalf("a failed apply does not end the picker: %v", err)
	}
	if !strings.Contains(h.ErrOut.String(), "(ccy-teams apply --all failed, exit 1: see above.)\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestOfferJoinHereAsksForTeamsThatCoverTheCheckout(t *testing.T) {
	h := newRig(t)
	h.inProject()
	h.On("git remote get-url origin", "git@example.com:example-org/proj.git\n", 0)
	h.OnDeclare("github-remote", "example-org/proj\n")
	h.OnDeclare("teams-for", "TEAM\talpha\n")
	h.Script.Gum = []string{"YES"}
	s, err := h.b.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := &run{p: h.p, snap: s}
	// the checkout holds no live session of alpha: ccy-proj is at 0 clients but it is live
	// and names seat 1, so the offer is not made
	if err = r.offerJoinHere(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.Script.GumLog) != 0 {
		t.Fatalf("asked although a live session holds a seat here: %v", h.Script.GumLog)
	}

	delete(s.Live, "ccy-proj")
	err = r.offerJoinHere(context.Background())
	if code, ok := flow.ExitCodeOf(err); !ok || code != term.ExitOK {
		t.Fatalf("starting the session ends the picker: %v", err)
	}
	// S-B15: one question for the checkout, and ccy --teams (bare) shows the teams
	want := "gum confirm --default=true Start a ccy session in " + h.Checkout() + "?"
	if len(h.Script.GumLog) != 1 || h.Script.GumLog[0] != want {
		t.Fatalf("gum %q", h.Script.GumLog)
	}
	if len(h.Execs) != 1 || h.Execs[0].Dir != h.Checkout() || !slices.Equal(h.Execs[0].Argv, []string{"/x/launcher", "--teams"}) {
		t.Fatalf("execs %+v", h.Execs)
	}
}

func TestOfferJoinHereWithNoTeamForTheRepositoryOffersToCreateOne(t *testing.T) {
	h := newRig(t)
	h.inProject()
	h.On("git remote get-url origin", "git@example.com:example-org/proj.git\n", 0)
	h.OnDeclare("github-remote", "example-org/proj\n")
	h.OnDeclare("teams-for", "")
	h.Script.Gum = []string{"NO", "NO"}
	r := &run{p: h.p, snap: board.NewSnapshot()}
	if err := r.offerJoinHere(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"No team on this machine works on example-org/proj (this checkout). The teams listed next are for other repositories.\n",
		"Nothing created. ccy-teams create starts one whenever you like.\n",
	} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
	// S-B15: creating a team is heavy and outward, so a stray Enter does not do it
	if want := "gum confirm --default=false Create a team for example-org/proj now?"; h.Script.GumLog[0] != want {
		t.Fatalf("gum %q", h.Script.GumLog)
	}
	// S-B15: when no team covers the checkout, a session can still be started and choose its teams
	if want := "gum confirm --default=true Start a ccy session in " + h.Checkout() + " (choose its teams)?"; len(h.Script.GumLog) != 2 || h.Script.GumLog[1] != want {
		t.Fatalf("gum %q", h.Script.GumLog)
	}
	if len(h.Execs) != 0 {
		t.Fatalf("declined, yet started: %+v", h.Execs)
	}

	h = newRig(t)
	h.inProject()
	h.On("git remote get-url origin", "git@example.com:example-org/proj.git\n", 0)
	h.OnDeclare("github-remote", "example-org/proj\n")
	h.OnDeclare("teams-for", "")
	// yes to the offer; the wizard runs and stops at gh, which is not logged in
	h.OnErr("gh auth status", "not logged in", 1)
	h.Script.Gum = []string{"YES"}
	r = &run{p: h.p, snap: board.NewSnapshot()}
	err := r.offerJoinHere(context.Background())
	if code, ok := flow.ExitCodeOf(err); !ok || code != term.ExitFailure {
		t.Fatalf("create ends the picker with its status: %v", err)
	}
	if !strings.Contains(h.ErrOut.String(), "gh is not logged in to GitHub") {
		t.Fatalf("the wizard did not run: %q", h.ErrOut.String())
	}
}

func TestOfferJoinHereSkipsWhatItCannotDecide(t *testing.T) {
	// not in a work tree
	h := newRig(t)
	r := &run{p: h.p, snap: board.NewSnapshot()}
	if err := r.offerJoinHere(context.Background()); err != nil || len(h.Script.GumLog) != 0 {
		t.Fatalf("%v %v", err, h.Script.GumLog)
	}
	// no origin, so no repository to look teams up for
	h = newRig(t)
	h.inProject()
	h.On("git remote get-url origin", "", 2)
	r = &run{p: h.p, snap: board.NewSnapshot()}
	if err := r.offerJoinHere(context.Background()); err != nil || len(h.Script.GumLog) != 0 {
		t.Fatalf("%v %v", err, h.Script.GumLog)
	}
	// declare cannot answer: said, and skipped
	h = newRig(t)
	h.inProject()
	h.On("git remote get-url origin", "git@example.com:example-org/proj.git\n", 0)
	h.OnErr(h.DeclarePrefix("github-remote"), "declare: nope", 64)
	r = &run{p: h.p, snap: board.NewSnapshot()}
	if err := r.offerJoinHere(context.Background()); err != nil || !strings.Contains(h.ErrOut.String(), "declare: nope") {
		t.Fatalf("%v %q", err, h.ErrOut.String())
	}
	h = newRig(t)
	h.inProject()
	h.On("git remote get-url origin", "git@example.com:example-org/proj.git\n", 0)
	h.OnDeclare("github-remote", "example-org/proj\n")
	h.OnErr(h.DeclarePrefix("teams-for"), "declare: nope", 64)
	r = &run{p: h.p, snap: board.NewSnapshot()}
	if err := r.offerJoinHere(context.Background()); err != nil || !strings.Contains(h.ErrOut.String(), "declare: nope") {
		t.Fatalf("%v %q", err, h.ErrOut.String())
	}
}

func TestOfferJoinHereWithNoTeamForTheRepositoryCanStartASessionThatChoosesItsTeams(t *testing.T) {
	h := newRig(t)
	h.inProject()
	h.On("git remote get-url origin", "git@example.com:example-org/proj.git\n", 0)
	h.OnDeclare("github-remote", "example-org/proj\n")
	h.OnDeclare("teams-for", "")
	h.Script.Gum = []string{"NO", "YES"}
	r := &run{p: h.p, snap: board.NewSnapshot()}
	err := r.offerJoinHere(context.Background())
	if code, ok := flow.ExitCodeOf(err); !ok || code != term.ExitOK {
		t.Fatalf("starting the session ends the picker: %v", err)
	}
	if len(h.Execs) != 1 || h.Execs[0].Dir != h.Checkout() || !slices.Equal(h.Execs[0].Argv, []string{"/x/launcher", "--teams"}) {
		t.Fatalf("execs %+v", h.Execs)
	}
}
