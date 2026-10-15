package wsi

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// deadlineError is what execx.OS returns for a command its context's deadline ended.
func deadlineError() error { return fmt.Errorf("run gdbus: %w", context.DeadlineExceeded) }

const (
	ctrlV      = "key 29:1 47:1 47:0 29:0"
	ctrlShiftV = "key 29:1 42:1 47:1 47:0 42:0 29:0"
	shiftIns   = "key 42:1 110:1 110:0 42:0"
	enterKey   = "key 28:1 28:0"
	ctrlS      = "key 29:1 31:1 31:0 29:0"
	editor     = "('org.gnome.TextEditor', false, true, true, false)"
	unfocused  = "('org.gnome.TextEditor', false, true, false, false)"
	closedWin  = "('', false, false, false, true)"
)

// paste runs one auto-paste dictation on Wayland with the panel answering replies in turn.
func paste(t *testing.T, replies []string, args ...string) (*kit, int) {
	t.Helper()
	k := newKit(t)
	k.desktop()
	k.pasteKeys = replies
	code := k.exec(append([]string{"-a"}, args...)...)
	return k, int(code)
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

func TestW30ThePanelNamesTheKeyAndTheSave(t *testing.T) {
	k, code := paste(t, []string{editor}, "--paste-with-shift", "1")
	if code != 0 || k.keys() != ctrlV+" | "+enterKey+" | "+ctrlS {
		t.Fatalf("exit %d keys %q: %s", code, k.keys(), k.errb.String())
	}
	if k.asked != 1 || k.copied["wl-copy"] != "Hello world" {
		t.Fatalf("asked %d copied %q", k.asked, k.copied)
	}
	k.wantStderr("[WSI] Pasting into 'org.gnome.TextEditor': Ctrl+V, then Ctrl+S")
	k.wantStderr("[WSI] Save simulated via Ctrl+S")
	if st := k.states(); st[len(st)-1] != "SUCCESS" || !slices.Equal(k.sleeps[len(k.sleeps)-2:], []time.Duration{50 * time.Millisecond, 300 * time.Millisecond}) {
		t.Fatalf("states %q sleeps %v", st, k.sleeps)
	}
	if n := k.notes(); n[len(n)-1] != "'Hello world'|'audio-input-microphone'|3000" {
		t.Fatalf("notes %q", n)
	}

	k, code = paste(t, []string{"('kitty', true, false, true, false)"}, "--paste-with-shift", "0")
	if code != 0 || k.keys() != ctrlShiftV+" | "+enterKey {
		t.Fatalf("a terminal, whatever was chosen at Insert: exit %d keys %q", code, k.keys())
	}
	k, code = paste(t, []string{editor}, "--paste-with-shift", "1", "--no-auto-enter")
	if code != 0 || k.keys() != ctrlV+" | "+ctrlS {
		t.Fatalf("no Enter: keys %q", k.keys())
	}
	k, code = paste(t, []string{"('kitty', true, false)"}, "--paste-with-shift", "0")
	if code != 0 || k.keys() != ctrlShiftV+" | "+enterKey {
		t.Fatalf("a three-field answer: keys %q", k.keys())
	}
}

func TestW30APanelThatCannotAnswerLeavesTheKeyChosenAtInsert(t *testing.T) {
	k, code := paste(t, nil, "--paste-with-shift", "1")
	if code != 0 || k.keys() != ctrlShiftV+" | "+enterKey {
		t.Fatalf("exit %d keys %q", code, k.keys())
	}
	k.wantStderr("WARN: The panel did not say how to paste into the focused window (Error: GDBus.Error:org.freedesktop.DBus.Error.UnknownMethod: No such method); using the key chosen at Insert")

	k = newKit(t)
	k.desktop()
	k.run.On([]string{"gdbus", "call", "--session", "--dest", "org.gnome.Shell"}, execx.Result{})
	k.run.OnError([]string{"gdbus", "call", "--session", "--dest", "org.gnome.Shell"}, deadlineError())
	if code := k.exec("-a", "--paste-with-shift", "0"); code != 0 || k.keys() != ctrlV+" | "+enterKey {
		t.Fatalf("exit %d keys %q", code, k.keys())
	}
	k.wantStderr("(no answer within 2 s)")
}

func TestW30FocusIsWaitedForThenMustSettle(t *testing.T) {
	replies := append([]string{unfocused, unfocused}, editor)
	k, code := paste(t, replies, "--paste-with-shift", "1")
	if code != 0 || k.keys() != ctrlV+" | "+enterKey+" | "+ctrlS {
		t.Fatalf("exit %d keys %q", code, k.keys())
	}
	if k.asked != 3+5+1 {
		t.Fatalf("asked %d", k.asked)
	}
	k.wantStderr("WARN: 'org.gnome.TextEditor' lost focus during the dictation; waiting for the panel to give it focus back")
	k.wantStderr("'org.gnome.TextEditor' has focus back; pasting once it has kept it for 5 more checks, 0.1 s apart")
	k.wantStderr("Waiting 1.0 s before the Enter: focus was given back for this paste")
	k.wantStderr("'org.gnome.TextEditor' still has focus before the Enter")

	k, code = paste(t, []string{unfocused, editor, editor, unfocused, editor}, "--paste-with-shift", "1")
	if code != 0 || k.asked != 4+1+5+1 || k.keys() != ctrlV+" | "+enterKey+" | "+ctrlS {
		t.Fatalf("a loss while settling starts the settle again: asked %d keys %q", k.asked, k.keys())
	}
	k.wantStderr("WARN: 'org.gnome.TextEditor' lost focus again before the paste")
}

func TestW33FocusIsAskedAgainBeforeTheEnterAfterARetake(t *testing.T) {
	retaken := append([]string{unfocused}, repeat(editor, 6)...)
	k, code := paste(t, append(append(retaken, unfocused), editor), "--paste-with-shift", "1")
	if code != 0 || k.asked != 7+1+1+5 || k.keys() != ctrlV+" | "+enterKey+" | "+ctrlS {
		t.Fatalf("exit %d asked %d keys %q", code, k.asked, k.keys())
	}
	k.wantStderr("WARN: 'org.gnome.TextEditor' lost focus again after the paste; giving it focus back before the Enter")
	k.wantStderr("'org.gnome.TextEditor' has kept focus again; sending the Enter")

	k, code = paste(t, append(retaken, closedWin), "--paste-with-shift", "1")
	if code != 1 || k.keys() != ctrlV {
		t.Fatalf("closed after the paste: exit %d keys %q", code, k.keys())
	}
	k.wantStderr("[ERROR] The text was pasted but not sent: the window this dictation started in was closed")
	if n := k.notes(); n[len(n)-1] != "'The text was pasted but not sent: the window this dictation started in was closed. No Enter was pressed.'|'dialog-error'|0" {
		t.Fatalf("notes %q", n)
	}
	if !slices.Contains(k.states(), "ERROR") {
		t.Fatalf("states %q", k.states())
	}

	k, code = paste(t, append(retaken, "('kitty', true, false)"), "--paste-with-shift", "1")
	if code != 0 || k.keys() != ctrlV+" | "+enterKey+" | "+ctrlS {
		t.Fatalf("a panel that does not pin: exit %d keys %q", code, k.keys())
	}
	k.wantStderr("the panel does not pin focus, so it cannot say whether the window still has it; sending the Enter")
}

func TestW31NothingIsPressedForAClosedOrUnfocusedWindow(t *testing.T) {
	k, code := paste(t, []string{closedWin}, "--paste-with-shift", "1")
	if code != 1 || k.keys() != "" || k.copied["wl-copy"] != "Hello world" {
		t.Fatalf("exit %d keys %q copied %q", code, k.keys(), k.copied)
	}
	if n := k.notes(); n[len(n)-1] != "'Nothing was pasted: the window this dictation started in was closed. The text is on the clipboard: Ctrl+V.'|'dialog-error'|0" {
		t.Fatalf("notes %q", n)
	}
	k, code = paste(t, []string{unfocused}, "--paste-with-shift", "1")
	if code != 1 || k.keys() != "" || k.asked != 21 {
		t.Fatalf("exit %d keys %q asked %d", code, k.keys(), k.asked)
	}
	k.wantStderr("Nothing was pasted: the window this dictation started in would not take focus back")
}

func TestW29TheLegacyPasteIsPrimaryAndShiftInsert(t *testing.T) {
	k, code := paste(t, []string{editor})
	if code != 0 || k.keys() != shiftIns+" | "+enterKey || k.asked != 0 || k.copied["wl-copy -p"] != "Hello world" {
		t.Fatalf("exit %d keys %q asked %d copied %q", code, k.keys(), k.asked, k.copied)
	}
	for _, c := range k.run.Calls() {
		if c.Name == "ydotool" && !slices.Equal(c.Env, []string{"YDOTOOL_SOCKET=" + k.socket}) {
			t.Fatalf("ydotool env %q", c.Env)
		}
	}
}

func TestW32AndW34KeyFailures(t *testing.T) {
	k := newKit(t)
	k.desktop()
	k.pasteKeys = []string{editor}
	k.run.On([]string{"ydotool", "key", "29:1", "47:1"}, execx.Result{ExitCode: 5})
	if code := k.exec("-a", "--paste-with-shift", "1"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR] ydotool Ctrl+V simulation failed (exit code: 5)")

	k = newKit(t)
	k.desktop()
	k.pasteKeys = []string{editor}
	k.run.On([]string{"ydotool", "key", "28:1"}, execx.Result{ExitCode: 3})
	k.run.On([]string{"ydotool", "key", "29:1", "31:1"}, execx.Result{ExitCode: 4})
	if code := k.exec("-a", "--paste-with-shift", "1"); code != 0 {
		t.Fatalf("the paste stands: exit %d", code)
	}
	k.wantStderr("[ERROR] ydotool Enter key simulation failed (exit code: 3)")
	k.wantStderr("[ERROR] The text was pasted but the save (Ctrl+S) failed (exit code: 4)")
	if st := k.states(); st[len(st)-1] != "SUCCESS" {
		t.Fatalf("states %q", st)
	}

	k = newKit(t)
	k.desktop()
	k.run.On([]string{"ydotool"}, execx.Result{ExitCode: 9})
	if code := k.exec("-a"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR] ydotool key simulation failed (exit code: 9)")
}

func TestW29TheSocketAndTheSessionAreCheckedBeforeThePaste(t *testing.T) {
	k := newKit(t)
	k.desktop()
	k.onState["TRANSCRIBING"] = func() { k.missing = append(k.missing, "socket") }
	if code := k.exec("-a"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR] ydotool socket disappeared before paste")

	k = newKit(t)
	k.env["XDG_SESSION_TYPE"] = "mir"
	k.env["DISPLAY"] = ":1"
	if code := k.exec("-a"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR] Unknown window manager for clipboard: mir")
	if strings.Contains(k.keys(), "key") {
		t.Fatal("keys were pressed")
	}
}
