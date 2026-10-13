package teams

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

const limitedLegacy = "LIMITED\talpha\tdocs/, CLAUDE/\tany repository of example-org\n"

func (h *pickerHarness) marker(team string) string {
	return filepath.Join(h.app.StateDir, "declined", "path-limit-"+team)
}

func TestPathLimitYesDropsTheLimitAndAppliesTheTeam(t *testing.T) {
	h := newPickerHarness(t)
	h.onDeclare("limited", limitedLegacy)
	h.onDeclare("drop-limit", "CHANGED\talpha\n")
	h.script.gum = []string{"YES"}
	if err := h.app.offerPathLimits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !h.fake.Called("env PYTHONPATH=" + h.app.RootDir + " python3 -P -m helpers.agent_bus.declare drop-limit --file " + h.app.HostVars + " --team alpha") {
		t.Fatalf("calls %v", h.fake.Calls())
	}
	if !slices.Equal(h.ops.calls, []string{"applyTeams alpha"}) {
		t.Fatalf("ops %v", h.ops.calls)
	}
	if want := "gum confirm --default=true Remove the limit, so any path in its repositories can be named?"; h.script.gumLog[0] != want {
		t.Fatalf("gum %q", h.script.gumLog)
	}
	for _, want := range []string{
		"One question about a team already on this machine, asked once, whichever checkout you are in.\n",
		"Team alpha (for any repository of example-org) was made when teams were limited to some folders: its agents may only point each other at files under docs/, CLAUDE/.\n",
	} {
		if !strings.Contains(h.errOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.errOut.String(), want)
		}
	}
	if _, err := os.Stat(h.marker("alpha")); err == nil {
		t.Fatal("a limit that was removed leaves no marker")
	}
}

func TestPathLimitNoIsRememberedAndNeverAskedAgain(t *testing.T) {
	h := newPickerHarness(t)
	h.onDeclare("limited", limitedLegacy)
	h.script.gum = []string{"NO"}
	if err := h.app.offerPathLimits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(h.marker("alpha")); err != nil {
		t.Fatalf("marker: %v", err)
	}
	if !strings.Contains(h.errOut.String(), "Kept. ccy-teams will not ask about alpha's limit again.\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
	if err := h.app.offerPathLimits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.script.gumLog) != 1 || len(h.ops.calls) != 0 {
		t.Fatalf("asked again: %v %v", h.script.gumLog, h.ops.calls)
	}
}

func TestPathLimitFailuresAreFatal(t *testing.T) {
	h := newPickerHarness(t)
	h.onErr(h.declarePrefix("limited"), "declare: cannot read", 78)
	err := h.app.offerPathLimits(context.Background())
	if code, ok := exitCodeOf(err); !ok || code != term.ExitFailure {
		t.Fatalf("limited failing: %v", err)
	}

	h = newPickerHarness(t)
	h.onDeclare("limited", limitedLegacy)
	h.onErr(h.declarePrefix("drop-limit"), "declare: refused", 78)
	h.script.gum = []string{"YES"}
	err = h.app.offerPathLimits(context.Background())
	if code, ok := exitCodeOf(err); !ok || code != term.ExitFailure || len(h.ops.calls) != 0 {
		t.Fatalf("drop-limit failing: %v %v", err, h.ops.calls)
	}

	h = newPickerHarness(t)
	h.onDeclare("limited", limitedLegacy)
	h.script.gum = []string{"CANCEL"}
	err = h.app.offerPathLimits(context.Background())
	if code, ok := exitCodeOf(err); !ok || code != term.ExitCancelled {
		t.Fatalf("cancelled: %v", err)
	}
}

func TestPathLimitApplyFailureIsSaidAndGoesOn(t *testing.T) {
	h := newPickerHarness(t)
	h.onDeclare("limited", limitedLegacy)
	h.onDeclare("drop-limit", "CHANGED\talpha\n")
	h.ops.teamsErr = errors.New("play failed")
	h.script.gum = []string{"YES"}
	if err := h.app.offerPathLimits(context.Background()); err != nil {
		t.Fatalf("a failed apply does not end the picker: %v", err)
	}
	if !strings.Contains(h.errOut.String(), "(ccy-teams apply alpha failed: see above.)\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestOfferUpdateListsTeamsInOrderAndDeclinedDoesNothing(t *testing.T) {
	h := newPickerHarness(t)
	h.onDeclare("declared", declaredAlpha+"TEAM\tbeta\t6168\tpresent\tbeta.agent-bus.internal\t-\n")
	h.on("systemctl list-units", unitAlpha, 0)
	h.onDeclare("status", "STATUS\talpha\tcurrent\t-\nSTATUS\tbeta\tunbuilt\t-\n")
	h.script.gum = []string{"NO"}
	if err := h.app.offerUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.errOut.String(), "Not as declared: beta (not built).\n") || len(h.ops.calls) != 0 {
		t.Fatalf("stderr %q ops %v", h.errOut.String(), h.ops.calls)
	}
	if want := "gum confirm --default=true Update them now (play-agent-bus.yml)?"; h.script.gumLog[0] != want {
		t.Fatalf("gum %q", h.script.gumLog)
	}
}

func TestOfferUpdateSaysNothingWhenAllIsCurrent(t *testing.T) {
	h := newPickerHarness(t)
	if err := h.app.offerUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.script.gumLog) != 0 {
		t.Fatalf("asked: %v", h.script.gumLog)
	}
}

func TestOfferUpdateFailsWhenStatusCannotBeRead(t *testing.T) {
	h := newPickerHarness(t)
	h.onErr(h.declarePrefix("status"), "declare: bad", 78)
	err := h.app.offerUpdate(context.Background())
	if code, ok := exitCodeOf(err); !ok || code != term.ExitFailure {
		t.Fatalf("%v", err)
	}
}

func TestOfferUpdateCancelledAndFailedApplyAreKept(t *testing.T) {
	h := newPickerHarness(t)
	h.onDeclare("status", "STATUS\talpha\toutdated\tunstamped\n")
	h.script.gum = []string{"CANCEL"}
	err := h.app.offerUpdate(context.Background())
	if code, ok := exitCodeOf(err); !ok || code != term.ExitCancelled {
		t.Fatalf("%v", err)
	}
	h = newPickerHarness(t)
	h.onDeclare("status", "STATUS\talpha\toutdated\tunstamped\n")
	h.script.gum = []string{"YES"}
	h.ops.applyRC = 130
	err = h.app.offerUpdate(context.Background())
	if code, ok := exitCodeOf(err); !ok || code != term.ExitCancelled {
		t.Fatalf("%v", err)
	}
}

func TestBackToListReadsALineOrPrintsANewlineOnEOF(t *testing.T) {
	h := newPickerHarness(t)
	h.tty = "x\nrest"
	h.app.backToList(context.Background())
	if h.errOut.String() != "Press Enter to go back to the list.\n" {
		t.Fatalf("stderr %q", h.errOut.String())
	}
	h = newPickerHarness(t)
	h.tty = ""
	h.app.backToList(context.Background())
	if h.errOut.String() != "Press Enter to go back to the list.\n\n" {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestBackToListWithNoTerminalDoesNotWait(t *testing.T) {
	h := newPickerHarness(t)
	h.app.OpenTTY = func() (io.ReadCloser, error) { return nil, errors.New("no tty") }
	h.app.backToList(context.Background())
	if h.errOut.String() != "Press Enter to go back to the list.\n\n" {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

// blockingReader never has a line to give until it is closed.
type blockingReader struct{ done chan struct{} }

func (b blockingReader) Read([]byte) (int, error) {
	<-b.done
	return 0, io.EOF
}

func (b blockingReader) Close() error { return nil }

func TestBackToListGivesUpWhenInterrupted(t *testing.T) {
	h := newPickerHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	h.app.OpenTTY = func() (io.ReadCloser, error) { return blockingReader{done: done}, nil }
	h.app.backToList(ctx)
	if !strings.HasSuffix(h.errOut.String(), "\n\n") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestOfferJoinHereAsksForTeamsThatCoverTheCheckout(t *testing.T) {
	h := newPickerHarness(t)
	h.inProject()
	h.on("git remote get-url origin", "git@example.com:example-org/proj.git\n", 0)
	h.onDeclare("github-remote", "example-org/proj\n")
	h.onDeclare("teams-for", "TEAM\talpha\n")
	h.script.gum = []string{"YES"}
	s, err := h.app.collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := &pickerRun{a: h.app, snap: s}
	// the checkout holds no live session of alpha: ccy-proj is at 0 clients but it is live
	// and names seat 1, so the offer is not made
	if err = r.offerJoinHere(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.script.gumLog) != 0 {
		t.Fatalf("asked although a live session holds a seat here: %v", h.script.gumLog)
	}

	delete(s.Live, "ccy-proj")
	err = r.offerJoinHere(context.Background())
	if code, ok := exitCodeOf(err); !ok || code != term.ExitOK {
		t.Fatalf("starting the session ends the picker: %v", err)
	}
	want := "gum confirm --default=true Start a ccy session in " + h.checkout() + " as a member of alpha?"
	if len(h.script.gumLog) != 1 || h.script.gumLog[0] != want {
		t.Fatalf("gum %q", h.script.gumLog)
	}
	if len(h.execs) != 1 || h.execs[0].dir != h.checkout() || !slices.Equal(h.execs[0].argv, []string{"/x/launcher", "--teams", "alpha"}) {
		t.Fatalf("execs %+v", h.execs)
	}
}

func TestOfferJoinHereWithNoTeamForTheRepositoryOffersToCreateOne(t *testing.T) {
	h := newPickerHarness(t)
	h.inProject()
	h.on("git remote get-url origin", "git@example.com:example-org/proj.git\n", 0)
	h.onDeclare("github-remote", "example-org/proj\n")
	h.onDeclare("teams-for", "")
	h.script.gum = []string{"NO"}
	r := &pickerRun{a: h.app, snap: newSnapshot()}
	if err := r.offerJoinHere(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"No team on this machine works on example-org/proj (this checkout). The teams listed next are for other repositories.\n",
		"Nothing created. ccy-teams create starts one whenever you like.\n",
	} {
		if !strings.Contains(h.errOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.errOut.String(), want)
		}
	}
	if want := "gum confirm --default=true Create a team for example-org/proj now?"; h.script.gumLog[0] != want {
		t.Fatalf("gum %q", h.script.gumLog)
	}

	h = newPickerHarness(t)
	h.inProject()
	h.on("git remote get-url origin", "git@example.com:example-org/proj.git\n", 0)
	h.onDeclare("github-remote", "example-org/proj\n")
	h.onDeclare("teams-for", "")
	h.script.gum = []string{"YES"}
	h.ops.createRC = 3
	r = &pickerRun{a: h.app, snap: newSnapshot()}
	err := r.offerJoinHere(context.Background())
	if code, ok := exitCodeOf(err); !ok || code != 3 {
		t.Fatalf("create ends the picker with its status: %v", err)
	}
}

func TestOfferJoinHereSkipsWhatItCannotDecide(t *testing.T) {
	// not in a work tree
	h := newPickerHarness(t)
	r := &pickerRun{a: h.app, snap: newSnapshot()}
	if err := r.offerJoinHere(context.Background()); err != nil || len(h.script.gumLog) != 0 {
		t.Fatalf("%v %v", err, h.script.gumLog)
	}
	// no origin, so no repository to look teams up for
	h = newPickerHarness(t)
	h.inProject()
	h.on("git remote get-url origin", "", 2)
	r = &pickerRun{a: h.app, snap: newSnapshot()}
	if err := r.offerJoinHere(context.Background()); err != nil || len(h.script.gumLog) != 0 {
		t.Fatalf("%v %v", err, h.script.gumLog)
	}
	// declare cannot answer: said, and skipped
	h = newPickerHarness(t)
	h.inProject()
	h.on("git remote get-url origin", "git@example.com:example-org/proj.git\n", 0)
	h.onErr(h.declarePrefix("github-remote"), "declare: nope", 64)
	r = &pickerRun{a: h.app, snap: newSnapshot()}
	if err := r.offerJoinHere(context.Background()); err != nil || !strings.Contains(h.errOut.String(), "declare: nope") {
		t.Fatalf("%v %q", err, h.errOut.String())
	}
	h = newPickerHarness(t)
	h.inProject()
	h.on("git remote get-url origin", "git@example.com:example-org/proj.git\n", 0)
	h.onDeclare("github-remote", "example-org/proj\n")
	h.onErr(h.declarePrefix("teams-for"), "declare: nope", 64)
	r = &pickerRun{a: h.app, snap: newSnapshot()}
	if err := r.offerJoinHere(context.Background()); err != nil || !strings.Contains(h.errOut.String(), "declare: nope") {
		t.Fatalf("%v %q", err, h.errOut.String())
	}
}

func TestAskYesMapsCancelAndFailure(t *testing.T) {
	h := newPickerHarness(t)
	h.script.gum = []string{"NO"}
	if yes, err := h.app.askYes(context.Background(), "q?", false); yes || err != nil {
		t.Fatalf("%v %v", yes, err)
	}
	h.app.Gum = &ui.Gum{Runner: runnerFunc(func(context.Context, execx.Cmd) (execx.Result, error) {
		return execx.Result{}, errors.New("gum cannot start")
	}), Stderr: h.errOut}
	_, err := h.app.askYes(context.Background(), "q?", false)
	if code, ok := exitCodeOf(err); !ok || code != term.ExitFailure {
		t.Fatalf("%v", err)
	}
}

func TestNeedGumNamesTheInstallingPlay(t *testing.T) {
	h := newPickerHarness(t)
	if err := h.app.needGum(); err != nil {
		t.Fatal(err)
	}
}
