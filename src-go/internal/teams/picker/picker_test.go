package picker

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

type runnerFunc func(context.Context, execx.Cmd) (execx.Result, error)

func (f runnerFunc) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) { return f(ctx, c) }

func TestPickerNeedsATerminal(t *testing.T) {
	h := newRig(t)
	h.p.Kit.Term.IsTerminal = func(v any) bool { return v != h.p.Stdout }
	if code := h.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	want := "ccy-teams needs a terminal: it attaches sessions to the one you are in. ccy-teams --list prints the table instead.\n"
	if h.ErrOut.String() != want {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPickerEscLeavesWithTheHouseStyleArgvAndTaggedRows(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	args := h.Script.FzfArgs[0]
	if slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "--expect") }) ||
		!slices.Contains(args, "--border-label= Agent teams ") || !slices.Contains(args, "--with-nth=1") {
		t.Fatalf("args %q", args)
	}
	if len(h.Script.Rows[0]) != 8+2 || h.Script.Rows[0][8] != sessionsAction.Line()+"\t8" || h.Script.Rows[0][9] != "Exit" {
		t.Fatalf("rows %q", h.Script.Rows[0])
	}
	if !strings.HasPrefix(h.Script.Rows[0][0], "alpha ") || !strings.HasSuffix(h.Script.Rows[0][0], "\t0") {
		t.Fatalf("first row %q", h.Script.Rows[0][0])
	}
}

func TestPickerHeaderNamesTheCheckoutTopNotTheSubdirectory(t *testing.T) {
	h := newRig(t)
	h.inProject()
	h.Env["PWD"] = h.Checkout() + "/sub/dir"
	h.Script.Fzf = []string{"|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	header := h.HeaderOf(0)
	// newRig's alpha has its description offer answered never, so its row can undo that.
	for _, want := range []string{
		"↑↓ choose   Enter lists what can be done to the row, help among it   Running sessions is the last row   Esc or q quit\n",
		"This row, Enter to: open alpha in Element, open every team in Element, start a session in alpha, add an LXC container to alpha, add a running session to alpha, edit alpha's settings, update out-of-date teams, restart alpha's desktop sessions, wind down alpha's sessions, be asked to describe alpha again, help, shut down alpha's sessions, take alpha down\n",
		"A new ccy session (Enter on a row) starts in the row's team, in " + h.Checkout() + "\n",
		"session restore on this machine is off (ccy_restore_sessions)\n",
		"TEAM ",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("header %q lacks %q", header, want)
		}
	}
	if strings.Contains(header, "sub/dir") {
		t.Fatalf("header names the subdirectory: %q", header)
	}
}

func TestPickerHeaderSaysANewSessionIsUnavailableOutsideAProjectAndRestoreOn(t *testing.T) {
	h := newRig(t)
	teamtest.MustWrite(t, h.Env["HOME"]+"/.config/systemd/user/default.target.wants/ccy-sessions-restore.service", "")
	h.Script.Fzf = []string{"|ABORT"}
	h.run()
	header := h.HeaderOf(0)
	if !strings.Contains(header, "A new ccy session is unavailable: "+h.Root+" is not a project folder (no git repository)\n") ||
		!strings.Contains(header, "restore on this machine is on (") {
		t.Fatalf("header %q", header)
	}
}

func TestPickerHeaderNeverOffersANewSessionInAContainersCheckout(t *testing.T) {
	h := newRig(t)
	top := h.InLxcCheckout()
	h.Script.Fzf = []string{"|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	header := h.HeaderOf(0)
	if strings.Contains(header, "A new ccy session (Enter on a row) starts") {
		t.Fatalf("a host session is offered in a container's files: %q", header)
	}
	if !strings.Contains(header, "A new ccy session is unavailable: "+top+" is LXC container "+teamtest.LxcContainer+"'s checkout") ||
		!strings.Contains(header, "ccy-teams lxc-add") {
		t.Fatalf("header %q", header)
	}
	if len(h.Script.GumLog) != 0 {
		t.Fatalf("a session is offered there on the way in: %q", h.Script.GumLog)
	}
	if !strings.Contains(h.ErrOut.String(), top+" is LXC container "+teamtest.LxcContainer+"'s checkout") {
		t.Fatalf("the way in says nothing of it: %q", h.ErrOut.String())
	}
}

func TestPickerHeaderHeadingIsBoldOnAColourTerminal(t *testing.T) {
	h := newRig(t)
	delete(h.Env, "NO_COLOR")
	h.Script.Fzf = []string{"|ABORT"}
	h.run()
	if !strings.HasSuffix(h.HeaderOf(0), term.Reset+"\n") || !strings.Contains(h.HeaderOf(0), "\n"+term.Bold+"TEAM ") {
		t.Fatalf("header %q", h.HeaderOf(0))
	}
}

// The rows are laid out together, each seat by its whole handle, and on a terminal too narrow
// for every column the row keeps what it is and its header carries the rest.
func TestPickerShowsWholeHandlesAndMovesColumnsToTheHeaderWhenNarrow(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"|ABORT"}
	h.run()
	rows := strings.Join(h.Script.Rows[0], "\n")
	for _, want := range []string{"alpha  proj.1+local.podman ", "alpha  proj.dev+local.podman ", "up to date"} {
		if !strings.Contains(rows, want) {
			t.Fatalf("rows %q lack %q", rows, want)
		}
	}
	if !strings.Contains(h.HeaderOf(0), "APPLIED") || strings.Contains(strings.Join(h.Script.FzfArgs[0], "\n"), "This row: APPLIED") {
		t.Fatal("a table that fits keeps its columns on the rows")
	}

	h = newRig(t)
	h.Host.Columns = func(any) int { return 50 + ui.RowChrome }
	h.Script.Fzf = []string{"|ABORT"}
	h.run()
	lines := strings.Split(strings.TrimSuffix(h.HeaderOf(0), "\n"), "\n")
	if heading := lines[len(lines)-1]; !strings.HasPrefix(heading, "TEAM ") || !strings.HasSuffix(heading, "STATE") {
		t.Fatalf("the narrow heading is TEAM MEMBER KIND STATE: %q", heading)
	}
	for _, row := range h.Script.Rows[0][:8] {
		if strings.Contains(row, "up to date") {
			t.Fatalf("a narrow row keeps the moved columns: %q", row)
		}
	}
	if !strings.Contains(strings.Join(h.Script.Rows[0], "\n"), "alpha  proj.1+local.podman ") {
		t.Fatalf("the handle stays whole: %q", h.Script.Rows[0])
	}
	if !strings.Contains(strings.Join(h.Script.FzfArgs[0], "\n"), "This row: ABOUT (no description)   APPLIED up to date   SESSION -") {
		t.Fatalf("the team row's header carries its moved columns: %q", h.Script.FzfArgs[0])
	}
}

func TestPickerAsksForSudoOnceAndRefusalLeavesWithTheCost(t *testing.T) {
	h := newRig(t)
	h.Fake.On([]string{"sudo", "-n", "/usr/local/bin/agent-bus", "list", "alpha"},
		execx.Result{Stderr: []byte("sudo: a password is required\n"), ExitCode: 1})
	h.OnErr("sudo -n -v", "sudo: a password is required", 1)
	h.OnErr("sudo -v", "sudo: 3 incorrect password attempts", 1)
	if code := h.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{
		"done as root", "play-agent-bus.yml", "sudo was not given your password",
		"ERROR: so the teams' members cannot be shown, and nothing was changed. ccy-teams --list shows the teams without them.\n",
	} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
	if len(h.Script.Rows) != 0 {
		t.Fatal("no picker is shown")
	}
}

func TestPickerStillRefusedAfterSudoLeaves(t *testing.T) {
	h := newRig(t)
	h.Fake.On([]string{"sudo", "-n", "/usr/local/bin/agent-bus", "list", "alpha"},
		execx.Result{Stderr: []byte("sudo: a password is required\n"), ExitCode: 1})
	h.On("sudo -n -v", "", 1)
	h.On("sudo -v", "", 0)
	if code := h.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "ERROR: sudo took your password but still refuses agent-bus list, so the members cannot be shown.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPickerNoTeamsOffersCreate(t *testing.T) {
	h := newRig(t)
	h.WriteHostVars(t, "")
	h.On("systemctl list-units", "", 0)
	// yes to the offer; the wizard runs and stops at gh, which is not logged in: its exit ends the picker
	h.OnErr("gh auth status", "not logged in", 1)
	h.Script.Gum = []string{"YES"}
	if code := h.run(); code != term.ExitFailure {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if len(h.Script.GumLog) != 1 || h.Script.GumLog[0] != "gum confirm --default=false Create one now?" {
		t.Fatalf("gum %v", h.Script.GumLog)
	}
	for _, want := range []string{"No agent teams on this machine yet.", "gh is not logged in to GitHub"} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}
}

func TestPickerNoTeamsDeclinedAndCancelled(t *testing.T) {
	h := newRig(t)
	h.WriteHostVars(t, "")
	h.On("systemctl list-units", "", 0)
	h.Script.Gum = []string{"NO"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "Nothing created. ccy-teams create starts one whenever you like.\n") || len(h.Script.GumLog) != 1 {
		t.Fatalf("stderr %q gum %v", h.ErrOut.String(), h.Script.GumLog)
	}
	h = newRig(t)
	h.WriteHostVars(t, "")
	h.On("systemctl list-units", "", 0)
	h.Script.Gum = []string{"CANCEL"}
	if code := h.run(); code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "Cancelled; nothing was changed.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPickerMissingGumIsRefusedWhereItIsNeeded(t *testing.T) {
	h := newRig(t)
	h.WriteHostVars(t, "")
	h.On("systemctl list-units", "", 0)
	h.p.Kit.LookPath = func(name string) (string, error) { return "", errors.New("not found: " + name) }
	if code := h.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "ERROR: gum is not installed, and ccy-teams needs it here. playbooks/imports/play-claude-yolo.yml installs it; run that play and try again.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPickerWithoutFzfSaysWhatInstallsIt(t *testing.T) {
	h := newRig(t)
	h.p.Kit.LookPath = func(name string) (string, error) {
		if name == "fzf" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	if code := h.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "ERROR: fzf is not installed; playbooks/imports/play-claude-yolo.yml installs it.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPickerCancelledByInterruptIsExit130(t *testing.T) {
	h := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := h.p.run(ctx); code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "Cancelled; nothing was changed.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPickerInterruptWhileChoosingIsExit130(t *testing.T) {
	h := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	h.p.Kit.Picker = &ui.Picker{Runner: runnerFunc(func(context.Context, execx.Cmd) (execx.Result, error) {
		cancel()
		return execx.Result{ExitCode: 130}, &execx.ExitError{Name: "fzf", Code: 130}
	}), Stderr: h.ErrOut}
	if code := h.p.run(ctx); code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
}

func TestPickerRowOfTellsIdenticalRowsApartByTheirTag(t *testing.T) {
	h := newRig(t)
	s, err := h.b.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.Rows = append(s.Rows, s.Rows[1])
	row, ok := s.RowAt("8")
	if !ok || row.Member != s.Rows[1].Member {
		t.Fatalf("row %+v %v", row, ok)
	}
	if _, ok := s.RowAt("99"); ok {
		t.Fatal("an index past the rows is a changed list")
	}
	if _, ok := s.RowAt("x"); ok {
		t.Fatal("a tag that is not a number is a changed list")
	}
}

// Each row carries its place in the list through fzf (SPEC-ccy-teams 9.20), so a row fzf hands
// back that the list never showed is a failure, said as one, never a silent redraw.
func TestPickerARowTheListNeverShowedIsAFailure(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"|owner", "|ABORT"}
	h.Script.Rewrite = func(out string) string { return strings.Replace(out, "\t5", "\t99", 1) }
	if code := h.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "a row that is not in the list") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPickerCountsOnlyConsecutiveNoOps(t *testing.T) {
	h := newRig(t)
	// outside a project, starting a session is flow.Refused each time
	start := []string{"|port 6167", "|Start a ccy session"}
	h.Script.Fzf = slices.Concat(start, start, start)
	if code := h.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "Nothing attached or started after 3 tries.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}

	h = newRig(t)
	// a line that does something in between starts the count again
	help := []string{"|port 6167", "|What the columns"}
	h.Script.Fzf = slices.Concat(start, start, help, start, start, []string{"|ABORT"})
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
}

func TestPickerEnterOnAnyRowOpensItsMenuAndTheBackRowReturnsToTheList(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"|proj.box+role.lxc", "|EXIT", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if len(h.Script.Rows) != 3 || !strings.Contains(h.HeaderOf(1), "What to do with proj.box+role.lxc in alpha:") {
		t.Fatalf("drawn %d times, menu header %q", len(h.Script.Rows), h.HeaderOf(1))
	}
}

func TestPickerStartupOfferUpdateRunsApplyAll(t *testing.T) {
	h := newRig(t)
	h.outdatedUntilApplied("config")
	h.Script.Gum = []string{"YES"}
	h.Script.Fzf = []string{"|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	if h.plays() != 1 {
		t.Fatalf("the apply play ran %d times, want once", h.plays())
	}
	if !strings.Contains(h.ErrOut.String(), "Not as declared: alpha (out of date: config).\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPickerUpdateEveryTeamUpdatesEverythingAndWaits(t *testing.T) {
	h := newRig(t)
	h.outdatedUntilApplied("unstamped")
	h.Script.Gum = []string{"NO"} // the start-up offer to update is declined; the menu line does it
	h.Script.Fzf = []string{"|port 6167", "|Update every team", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if h.plays() != 1 || !strings.Contains(h.ErrOut.String(), "Press Enter to go back to the list.") {
		t.Fatalf("plays %d stderr %q", h.plays(), h.ErrOut.String())
	}
}

func TestPickerNamesTheVerbWhenAFlowFailsAndStopsWhenItIsCancelled(t *testing.T) {
	h := newRig(t)
	h.outdatedUntilApplied("unstamped")
	h.OnErr(h.Host.Play, "boom", 1)
	h.Script.Gum = []string{"NO"}
	h.Script.Fzf = []string{"|port 6167", "|Update every team", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "(ccy-teams apply --all failed, exit 1: see above.)\n") ||
		strings.Contains(h.ErrOut.String(), "cmd_apply") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}

	h = newRig(t)
	// Ctrl-C at the first question of taking a team down
	h.Script.Gum = []string{"YES", "YES", "CANCEL"}
	h.Script.Fzf = []string{"|port 6167", "|Take alpha down"}
	if code := h.run(); code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
}
