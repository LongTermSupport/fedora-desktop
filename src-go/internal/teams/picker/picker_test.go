package picker

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
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
	if !slices.Contains(args, "--expect=ctrl-n,ctrl-r,ctrl-x,ctrl-e,ctrl-p,ctrl-u,?") ||
		!slices.Contains(args, "--border-label= Agent teams ") || !slices.Contains(args, "--with-nth=1") {
		t.Fatalf("args %q", args)
	}
	if len(h.Script.Rows[0]) != 8+1 || h.Script.Rows[0][8] != "Exit" {
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
	for _, want := range []string{
		"↑↓ choose   Enter attach a seat / password for a person   Ctrl-P password Ctrl-E Element   ? help   Esc or q quit\n",
		"Ctrl-N new session   Ctrl-U update out-of-date teams   Ctrl-R restore on/off   Ctrl-X remove/park/take down\n",
		"Ctrl-N starts a new ccy session in the row's team, in " + h.Checkout() + "\n",
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

func TestPickerHeaderSaysCtrlNIsUnavailableOutsideAProjectAndRestoreOn(t *testing.T) {
	h := newRig(t)
	teamtest.MustWrite(t, h.Env["HOME"]+"/.config/systemd/user/default.target.wants/ccy-sessions-restore.service", "")
	h.Script.Fzf = []string{"|ABORT"}
	h.run()
	header := h.HeaderOf(0)
	if !strings.Contains(header, "Ctrl-N unavailable: "+h.Root+" is not a project folder (no git repository)\n") ||
		!strings.Contains(header, "restore on this machine is on (") {
		t.Fatalf("header %q", header)
	}
}

func TestPickerHeaderHeadingIsBoldOnAColourTerminal(t *testing.T) {
	h := newRig(t)
	delete(h.Env, "NO_COLOR")
	h.Script.Fzf = []string{"|ABORT"}
	h.run()
	if !strings.HasSuffix(h.HeaderOf(0), term.Bold+board.HeadingText()+term.Reset+"\n") {
		t.Fatalf("header %q", h.HeaderOf(0))
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
	h.OnDeclare("declared", "")
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
	h.OnDeclare("declared", "")
	h.On("systemctl list-units", "", 0)
	h.Script.Gum = []string{"NO"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "Nothing created. ccy-teams create starts one whenever you like.\n") || len(h.Script.GumLog) != 1 {
		t.Fatalf("stderr %q gum %v", h.ErrOut.String(), h.Script.GumLog)
	}
	h = newRig(t)
	h.OnDeclare("declared", "")
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
	h.OnDeclare("declared", "")
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

func TestPickerChangedRowRefreshesAfterBackToList(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"|owner", "|ABORT"}
	h.Script.Rewrite = func(out string) string { return strings.Replace(out, "\t5", "\t99", 1) }
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "That row has changed; the list has been refreshed.\n") ||
		!strings.Contains(h.ErrOut.String(), "Press Enter to go back to the list.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if len(h.Script.Rows) != 2 {
		t.Fatalf("the list was drawn %d times", len(h.Script.Rows))
	}
}

func TestPickerCountsOnlyConsecutiveNoOps(t *testing.T) {
	h := newRig(t)
	// outside a project, Ctrl-N is flow.Refused each time
	h.Script.Fzf = []string{"ctrl-n|port 6167", "ctrl-n|port 6167", "ctrl-n|port 6167"}
	if code := h.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "Nothing attached or started after 3 tries.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}

	h = newRig(t)
	// a key that does something in between starts the count again
	h.Script.Fzf = []string{"ctrl-n|port 6167", "ctrl-n|port 6167", "?|port 6167", "ctrl-n|port 6167", "ctrl-n|port 6167", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
}

func TestPickerEnterOnAnythingElseSaysSoAndWaits(t *testing.T) {
	h := newRig(t)
	h.Script.Fzf = []string{"|proj.box+role.lxc", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	want := "Enter on proj.box+role.lxc does nothing: Ctrl-X parks a member. ? shows every key.\nPress Enter to go back to the list.\n"
	if !strings.Contains(h.ErrOut.String(), want) {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestPickerStartupOfferUpdateRunsApplyAll(t *testing.T) {
	h := newRig(t)
	h.outdatedUntilApplied("config")
	h.OnDeclare("limited", "")
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

func TestPickerCtrlUUpdatesEverythingAndWaits(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("limited", "")
	h.outdatedUntilApplied("unstamped")
	h.Script.Gum = []string{"NO"} // the start-up offer to update is declined; Ctrl-U does it
	h.Script.Fzf = []string{"ctrl-u|port 6167", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if h.plays() != 1 || !strings.Contains(h.ErrOut.String(), "Press Enter to go back to the list.") {
		t.Fatalf("plays %d stderr %q", h.plays(), h.ErrOut.String())
	}
}

func TestPickerNamesTheVerbWhenAFlowFailsAndStopsWhenItIsCancelled(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("limited", "")
	h.outdatedUntilApplied("unstamped")
	h.OnErr(h.Host.Play, "boom", 1)
	h.Script.Gum = []string{"NO"}
	h.Script.Fzf = []string{"ctrl-u|port 6167", "|ABORT"}
	if code := h.run(); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "(ccy-teams apply --all failed, exit 1: see above.)\n") ||
		strings.Contains(h.ErrOut.String(), "cmd_apply") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}

	h = newRig(t)
	h.OnDeclare("limited", "")
	// Ctrl-C at the first question of taking a team down
	h.Script.Gum = []string{"YES", "YES", "CANCEL"}
	h.Script.Fzf = []string{"ctrl-x|port 6167"}
	if code := h.run(); code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
}
