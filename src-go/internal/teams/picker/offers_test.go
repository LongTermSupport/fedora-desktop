package picker

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const limitedLegacy = "LIMITED\talpha\tdocs/, CLAUDE/\tany repository of example-org\n"

func (h rig) marker(team string) string {
	return filepath.Join(h.p.StateDir, "declined", "path-limit-"+team)
}

func TestPathLimitYesDropsTheLimitAndAppliesTheTeam(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("limited", limitedLegacy)
	h.OnDeclare("drop-limit", "CHANGED\talpha\n")
	h.Script.Gum = []string{"YES"}
	if err := h.p.offerPathLimits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !h.Fake.Called("env PYTHONPATH=" + h.Host.RootDir + " python3 -P -m helpers.agent_bus.declare drop-limit --file " + h.Host.HostVars + " --team alpha") {
		t.Fatalf("calls %v", h.Fake.Calls())
	}
	if h.plays() != 1 {
		t.Fatalf("the apply play ran %d times, want once", h.plays())
	}
	if want := "gum confirm --default=true Remove the limit, so any path in its repositories can be named?"; h.Script.GumLog[0] != want {
		t.Fatalf("gum %q", h.Script.GumLog)
	}
	for _, want := range []string{
		"One question about a team already on this machine, asked once, whichever checkout you are in.\n",
		"Team alpha (for any repository of example-org) was made when teams were limited to some folders: its agents may only point each other at files under docs/, CLAUDE/.\n",
	} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
	if _, err := os.Stat(h.marker("alpha")); err == nil {
		t.Fatal("a limit that was removed leaves no marker")
	}
}

// declinedOnce is a declined offer: remembered in marker, said, and not made again.
func declinedOnce(t *testing.T, h rig, offer func() error, marker, said string) {
	t.Helper()
	if err := offer(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker: %v", err)
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

func TestPathLimitNoIsRememberedAndNeverAskedAgain(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("limited", limitedLegacy)
	h.Script.Gum = []string{"NO"}
	declinedOnce(t, h, func() error { return h.p.offerPathLimits(context.Background()) }, h.marker("alpha"),
		"Kept. ccy-teams will not ask about alpha's limit again.\n")
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
	h.Script.Gum = []string{"YES"}
	err = h.p.offerPathLimits(context.Background())
	if code, ok := flow.ExitCodeOf(err); !ok || code != term.ExitFailure || h.plays() != 0 {
		t.Fatalf("drop-limit failing: %v, plays %d", err, h.plays())
	}

	h = newRig(t)
	h.OnDeclare("limited", limitedLegacy)
	h.Script.Gum = []string{"CANCEL"}
	err = h.p.offerPathLimits(context.Background())
	if code, ok := flow.ExitCodeOf(err); !ok || code != term.ExitCancelled {
		t.Fatalf("cancelled: %v", err)
	}
}

func TestPathLimitApplyFailureIsSaidAndGoesOn(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("limited", limitedLegacy)
	h.OnDeclare("drop-limit", "CHANGED\talpha\n")
	h.OnErr(h.Host.Play, "play failed", 1)
	h.Script.Gum = []string{"YES"}
	if err := h.p.offerPathLimits(context.Background()); err != nil {
		t.Fatalf("a failed apply does not end the picker: %v", err)
	}
	if !strings.Contains(h.ErrOut.String(), "(ccy-teams apply alpha failed: see above.)\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

const undescribedAlpha = "UNDESCRIBED\talpha\tany repository of example-org\n"

func (h rig) declined(team string) string {
	return filepath.Join(h.p.StateDir, "declined", "description-"+team)
}

func TestDescriptionYesAsksChecksDescribesAndAppliesTheTeam(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("undescribed", undescribedAlpha)
	h.OnDeclare("check", "")
	h.OnDeclare("describe", "CHANGED\tteam alpha description\n")
	h.Script.Gum = []string{"YES", "CHOOSE:Builds the example site"}
	if err := h.p.offerDescriptions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := "gum confirm --default=true Team alpha (for any repository of example-org) has no description. Write one now?"; h.Script.GumLog[0] != want {
		t.Fatalf("gum %q", h.Script.GumLog)
	}
	if !strings.HasPrefix(h.Script.GumLog[1], "gum input --header What is this team for?") {
		t.Fatalf("gum %q", h.Script.GumLog)
	}
	if !h.Fake.Called(h.DeclarePrefix("describe") + " --file " + h.Host.HostVars + " --team alpha --description Builds the example site") {
		t.Fatalf("calls %v", h.Fake.Calls())
	}
	if h.plays() != 1 {
		t.Fatalf("the apply play ran %d times, want once", h.plays())
	}
	if _, err := os.Stat(h.declined("alpha")); err == nil {
		t.Fatal("a description that was written leaves no marker")
	}
}

func TestDescriptionNoIsRememberedAndNeverAskedAgain(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("undescribed", undescribedAlpha)
	h.Script.Gum = []string{"NO"}
	declinedOnce(t, h, func() error { return h.p.offerDescriptions(context.Background()) }, h.declined("alpha"),
		"Kept. ccy-teams will not ask about alpha's description again.\n")
}

func TestARefusedDescriptionIsAskedAgainWithWhatWasTyped(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("undescribed", undescribedAlpha)
	h.OnDeclare("check", "")
	h.OnErr(h.DeclarePrefix("check")+" --file "+h.Host.HostVars+" --description bad", "declare: --description: nope", 64)
	h.OnDeclare("describe", "CHANGED\tteam alpha description\n")
	h.Script.Gum = []string{"YES", "CHOOSE:bad", "CHOOSE:Good one"}
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

func TestADescriptionThatCannotBeAppliedIsSaidAndGoesOn(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("undescribed", undescribedAlpha)
	h.OnDeclare("check", "")
	h.OnDeclare("describe", "CHANGED\tteam alpha description\n")
	h.OnErr(h.Host.Play, "play failed", 1)
	h.Script.Gum = []string{"YES", "CHOOSE:Builds the example site"}
	if err := h.p.offerDescriptions(context.Background()); err != nil {
		t.Fatalf("a failed apply does not end the picker: %v", err)
	}
	if !strings.Contains(h.ErrOut.String(), "(ccy-teams apply alpha failed: see above.)\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestCancellingTheDescriptionQuestionLeavesNothingWritten(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("undescribed", undescribedAlpha)
	h.Script.Gum = []string{"YES", "ABORT"}
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
	want := "gum confirm --default=true Start a ccy session in " + h.Checkout() + " as a member of alpha?"
	if len(h.Script.GumLog) != 1 || h.Script.GumLog[0] != want {
		t.Fatalf("gum %q", h.Script.GumLog)
	}
	if len(h.Execs) != 1 || h.Execs[0].Dir != h.Checkout() || !slices.Equal(h.Execs[0].Argv, []string{"/x/launcher", "--teams", "alpha"}) {
		t.Fatalf("execs %+v", h.Execs)
	}
}

func TestOfferJoinHereWithNoTeamForTheRepositoryOffersToCreateOne(t *testing.T) {
	h := newRig(t)
	h.inProject()
	h.On("git remote get-url origin", "git@example.com:example-org/proj.git\n", 0)
	h.OnDeclare("github-remote", "example-org/proj\n")
	h.OnDeclare("teams-for", "")
	h.Script.Gum = []string{"NO"}
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
	if want := "gum confirm --default=true Create a team for example-org/proj now?"; h.Script.GumLog[0] != want {
		t.Fatalf("gum %q", h.Script.GumLog)
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
