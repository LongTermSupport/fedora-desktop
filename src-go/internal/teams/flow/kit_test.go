package flow

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// kitRig is a Kit on the fake machine, on a terminal, with scripted fzf and gum.
type kitRig struct {
	*teamtest.World
	*teamtest.Console

	kit *Kit
}

func newKit(t *testing.T) kitRig {
	t.Helper()
	w := teamtest.Populated(t)
	c := teamtest.Interactive(t, w)
	return kitRig{World: w, Console: c, kit: New(w.Host)}
}

func TestLeaveRoundTripsThroughExitCodeOf(t *testing.T) {
	code, ok := ExitCodeOf(Leave(term.ExitCancelled))
	if !ok || code != term.ExitCancelled {
		t.Fatalf("%v %v", code, ok)
	}
	if _, ok := ExitCodeOf(errors.New("plain")); ok {
		t.Fatal("a plain error asks for no exit")
	}
	if got := Leave(term.ExitUsage).Error(); got != "leave the picker with exit 64" {
		t.Fatalf("message %q", got)
	}
}

func TestTellSaysOneLineOnStderr(t *testing.T) {
	r := newKit(t)
	r.kit.Tell("hello")
	if r.ErrOut.String() != "hello\n" || r.Out.String() != "" {
		t.Fatalf("stderr %q stdout %q", r.ErrOut.String(), r.Out.String())
	}
}

func TestCancelledSaysNothingChangedAndLeavesWith130(t *testing.T) {
	r := newKit(t)
	err := r.kit.Cancelled()
	if code, ok := ExitCodeOf(err); !ok || code != term.ExitCancelled || r.ErrOut.String() != "Cancelled; nothing was changed.\n" {
		t.Fatalf("%v %q", err, r.ErrOut.String())
	}
}

func TestInterruptedIsOnlyACancelledContext(t *testing.T) {
	r := newKit(t)
	if err := r.kit.Interrupted(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code, ok := ExitCodeOf(r.kit.Interrupted(ctx)); !ok || code != term.ExitCancelled {
		t.Fatalf("%v %v", code, ok)
	}
}

func TestNeedGumAndNeedFzfNameThePlayThatInstallsThem(t *testing.T) {
	r := newKit(t)
	r.kit.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	if code, ok := ExitCodeOf(r.kit.NeedGum()); !ok || code != term.ExitFailure {
		t.Fatalf("gum: %v %v", code, ok)
	}
	if code, ok := ExitCodeOf(r.kit.NeedFzf()); !ok || code != term.ExitFailure {
		t.Fatalf("fzf: %v %v", code, ok)
	}
	for _, want := range []string{"gum is not installed", "fzf is not installed; playbooks/imports/play-claude-yolo.yml installs it."} {
		if !strings.Contains(r.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", r.ErrOut.String(), want)
		}
	}
	r.kit.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	if r.kit.NeedGum() != nil || r.kit.NeedFzf() != nil {
		t.Fatal("tools on PATH are accepted")
	}
}

func TestAskYesIsGumsConfirmAndCtrlCCancels(t *testing.T) {
	r := newKit(t)
	r.Script.Gum = []string{"YES", "NO", "CANCEL"}
	if yes, err := r.kit.AskYes(t.Context(), ui.Here("Teams", "alpha"), "Sure?", true); err != nil || !yes {
		t.Fatalf("yes: %v %v", yes, err)
	}
	if yes, err := r.kit.AskYes(t.Context(), ui.Here("Teams"), "Sure?", true); err != nil || yes {
		t.Fatalf("no: %v %v", yes, err)
	}
	_, err := r.kit.AskYes(t.Context(), ui.Here("Teams"), "Sure?", true)
	if code, ok := ExitCodeOf(err); !ok || code != term.ExitCancelled {
		t.Fatalf("ctrl-c: %v", err)
	}
}

func TestAskYesLeavesWithFailureWhenGumCannotRun(t *testing.T) {
	r := newKit(t)
	r.kit.Gum = &ui.Gum{Runner: &execx.Fake{}, Stderr: r.ErrOut}
	_, err := r.kit.AskYes(t.Context(), ui.Here("Teams"), "Sure?", true)
	if code, ok := ExitCodeOf(err); !ok || code != term.ExitFailure {
		t.Fatalf("%v", err)
	}
}

func TestConfirmIsAYesInThePickerAndNoEscAndQAreNo(t *testing.T) {
	r := newKit(t)
	r.Script.Fzf = []string{"|YES", "|EXIT", "|ABORT"}
	for i, want := range []bool{true, false, false} {
		yes, err := r.kit.Confirm(t.Context(), ui.Here("Teams", "Do it"), "It does it.", "do it", false)
		if err != nil || yes != want {
			t.Fatalf("answer %d: %v %v", i, yes, err)
		}
	}
}

func TestConfirmWithoutFzfIsRefused(t *testing.T) {
	r := newKit(t)
	r.kit.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, err := r.kit.Confirm(t.Context(), ui.Here("t"), "q", "a", false); err == nil {
		t.Fatal("no fzf, no question")
	}
}

func TestChooseLeavesWithEscAndNamesAFailure(t *testing.T) {
	r := newKit(t)
	r.Script.Fzf = []string{"|ABORT"}
	if _, err := r.kit.Choose(t.Context(), ui.Here("t"), ui.PickSpec{Title: "t", Rows: []string{"a"}, Tags: []string{"0"}}); !errors.Is(err, ui.ErrLeave) {
		t.Fatalf("esc: %v", err)
	}
	r.kit.Picker = &ui.Picker{Runner: &execx.Fake{}, Stderr: r.ErrOut}
	_, err := r.kit.Choose(t.Context(), ui.Here("t"), ui.PickSpec{Title: "t", Rows: []string{"a"}, Tags: []string{"0"}})
	if code, ok := ExitCodeOf(err); !ok || code != term.ExitFailure {
		t.Fatalf("fzf failing: %v", err)
	}
}

func TestInPickerTurnsAFlowsExitIntoWhatThePickerDoesNext(t *testing.T) {
	r := newKit(t)
	if err := r.kit.InPicker("ccy-teams apply --all", term.ExitOK); err != nil {
		t.Fatal(err)
	}
	if code, ok := ExitCodeOf(r.kit.InPicker("x", term.ExitCancelled)); !ok || code != term.ExitCancelled {
		t.Fatalf("cancelled: %v %v", code, ok)
	}
	r.ErrOut.Reset()
	if err := r.kit.InPicker("ccy-teams apply --all", term.ExitFailure); err != nil {
		t.Fatal(err)
	}
	if r.ErrOut.String() != "(ccy-teams apply --all failed, exit 1: see above.)\n" {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
}

func TestRefuseAndNoteSayThenWaitForTheListToBeRead(t *testing.T) {
	r := newKit(t)
	res, err := r.kit.Refuse(t.Context(), "No.")
	if err != nil || res != Refused {
		t.Fatalf("refuse: %v %v", res, err)
	}
	res, err = r.kit.Note(t.Context(), "FYI.")
	if err != nil || res != Done {
		t.Fatalf("note: %v %v", res, err)
	}
	if want := "No.\nPress Enter to go back to the list.\nFYI.\nPress Enter to go back to the list.\n"; r.ErrOut.String() != want {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
}

func TestReportRootErrSaysWhySudoOrTheToolFailedExactlyOnce(t *testing.T) {
	r := newKit(t)
	r.kit.ReportRootErr(sudo.ErrNeedsPassword)
	if !strings.Contains(r.ErrOut.String(), "ERROR: sudo asked for a password again, so nothing was changed. Run sudo -v first.") {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
	r.ErrOut.Reset()
	r.kit.ReportRootErr(&agentbus.CommandError{Tool: "agent-bus", Code: 78})
	if r.ErrOut.Len() != 0 {
		t.Fatalf("a tool that said why says nothing more: %q", r.ErrOut.String())
	}
	r.kit.ReportRootErr(errors.New("could not start"))
	if r.ErrOut.String() != "could not start\n" {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
}

func TestSudoReadyIsWhetherSudoIsReady(t *testing.T) {
	r := newKit(t)
	r.On("sudo -n -v", "", 0)
	if !r.kit.SudoReady(t.Context(), "why") {
		t.Fatal("a cached credential is ready")
	}
}

func TestBackToListReadsALineOrPrintsANewlineOnEOF(t *testing.T) {
	r := newKit(t)
	r.TTY = "x\nrest"
	r.kit.BackToList(t.Context())
	if r.ErrOut.String() != "Press Enter to go back to the list.\n" {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
	r = newKit(t)
	r.TTY = ""
	r.kit.BackToList(t.Context())
	if r.ErrOut.String() != "Press Enter to go back to the list.\n\n" {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
}

func TestBackToListWithNoTerminalDoesNotWait(t *testing.T) {
	r := newKit(t)
	r.kit.OpenTTY = func() (io.ReadCloser, error) { return nil, errors.New("no tty") }
	r.kit.BackToList(t.Context())
	if r.ErrOut.String() != "Press Enter to go back to the list.\n\n" {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
}

// blockingReader never has a line to give until it is closed, as a terminal file does.
// readReturned is closed when a Read call has returned.
type blockingReader struct {
	done         chan struct{}
	readReturned chan struct{}
	once         *sync.Once
}

func newBlockingReader() blockingReader {
	return blockingReader{done: make(chan struct{}), readReturned: make(chan struct{}), once: &sync.Once{}}
}

func (b blockingReader) Read([]byte) (int, error) {
	<-b.done
	close(b.readReturned)
	return 0, io.EOF
}

func (b blockingReader) Close() error {
	b.once.Do(func() { close(b.done) })
	return nil
}

func TestBackToListGivesUpWhenInterrupted(t *testing.T) {
	r := newKit(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reader := newBlockingReader()
	r.kit.OpenTTY = func() (io.ReadCloser, error) { return reader, nil }
	r.kit.BackToList(ctx)
	if !strings.HasSuffix(r.ErrOut.String(), "\n\n") {
		t.Fatalf("stderr %q", r.ErrOut.String())
	}
}

// A cancelled wait must not leave its reader goroutine blocked on the terminal, where a later
// wait would race it for the Enter.
func TestReadLineCancelledLeavesNoReaderGoroutine(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := newBlockingReader()
	got, err := readLine(ctx, r)
	if got || err != nil {
		t.Fatalf("got %v, %v", got, err)
	}
	select {
	case <-r.readReturned:
	default:
		t.Fatal("the reader goroutine is still blocked in Read after readLine returned")
	}
}

func TestReadLineTakesOneLineAndNothingPastIt(t *testing.T) {
	src := strings.NewReader("ab\ncd")
	got, err := readLine(t.Context(), io.NopCloser(src))
	if !got || err != nil {
		t.Fatalf("got %v, %v", got, err)
	}
	if src.Len() != 2 {
		t.Fatalf("%d bytes left, want the 2 after the newline", src.Len())
	}
}

func TestReadLineEndOfInputIsNoLine(t *testing.T) {
	if got, err := readLine(t.Context(), io.NopCloser(strings.NewReader("ab"))); got || err != nil {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestAbbrevAbbreviatesTheHome(t *testing.T) {
	r := newKit(t)
	if got := r.kit.Abbrev(r.kit.Home + "/proj"); got != "~/proj" {
		t.Fatalf("got %q", got)
	}
}
