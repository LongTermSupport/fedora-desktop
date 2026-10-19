package open

import (
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// pngWorld is a graphical session with a PNG, gio answering out, two entries and chafa.
func pngWorld(t *testing.T, gio string) (*world, string) {
	t.Helper()
	w := newWorld(t)
	p := w.file("a.png", "x")
	w.types("image/png")
	w.gui("image/png", gio)
	w.desktop("org.gnome.Loupe.desktop", "[Desktop Entry]\nName=Image Viewer\n")
	w.desktop("org.gimp.GIMP.desktop", "[Desktop Entry]\nName=GIMP\n")
	w.install("chafa")
	w.runner.On([]string{"gio", "launch"}, execx.Result{})
	w.runner.On([]string{"chafa"}, execx.Result{})
	return w, p
}

// E15: the registered default opens without asking.
func TestRegisteredDefaultOpensUnasked(t *testing.T) {
	w, p := pngWorld(t, gioWithDefault)
	if code := w.run(p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	if len(w.picker.specs) != 0 {
		t.Fatal("asked")
	}
	entry := w.dataDirs + "/applications/org.gnome.Loupe.desktop"
	if !w.called("gio launch " + entry + " " + p) {
		t.Fatalf("ran %q", w.ran())
	}
}

// E15, B1: registered apps but no default means asking.
func TestNoDefaultAsksEvenWithRegisteredApps(t *testing.T) {
	w, p := pngWorld(t, gioNoDefault)
	w.picker.chooseRow("1")
	if code := w.run(p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	if len(w.picker.specs) != 1 {
		t.Fatalf("asked %d times", len(w.picker.specs))
	}
	if !w.called("chafa -- " + p) {
		t.Fatalf("the second row is chafa; ran %q", w.ran())
	}
}

// E15: no desktop apps and a known type: the first terminal handler, unasked.
func TestKnownTypeInATerminalOpensUnasked(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.png", "x")
	w.types("image/png")
	w.install("chafa", "viu")
	w.runner.On([]string{"chafa"}, execx.Result{})
	if code := w.run(p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	if len(w.picker.specs) != 0 || !w.called("chafa -- "+p) {
		t.Fatalf("asked %d, ran %q", len(w.picker.specs), w.ran())
	}
}

// E15: an unknown type asks; --ask always asks.
func TestUnknownTypeAndAskAsk(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.bin", "x")
	w.types("application/x-weird")
	w.picker.chooseRow("0")
	w.runner.On([]string{"file", "--"}, execx.Result{})
	if code := w.run(p); code != term.ExitOK || len(w.picker.specs) != 1 {
		t.Fatalf("unknown: exit %d, asked %d: %s", code, len(w.picker.specs), w.stderr.String())
	}
	w, p = pngWorld(t, gioWithDefault)
	w.picker.chooseRow("0")
	if code := w.run("-a", p); code != term.ExitOK || len(w.picker.specs) != 1 {
		t.Fatalf("--ask: exit %d, asked %d", code, len(w.picker.specs))
	}
}

// E17, B18, B19: the house picker, the heading, a hidden tag per row and a Cancel row.
func TestPickerScreen(t *testing.T) {
	w, p := pngWorld(t, gioNoDefault)
	w.desktop("org.gimp.GIMP.desktop", "[Desktop Entry]\nName=chafa (terminal image)\n")
	w.picker.chooseRow("1")
	if code := w.run(p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	s, h := w.picker.specs[0], w.picker.headings[0]
	if s.Title != "open" || s.ExitLabel != "Cancel — open nothing" || !strings.Contains(s.Header, "Enter open   Esc or q cancel") {
		t.Fatalf("spec %+v", s)
	}
	if strings.Join(s.Rows, "|") != "chafa (terminal image) [GUI]|chafa (terminal image)|file (identify only)" {
		t.Fatalf("rows %q", s.Rows)
	}
	if strings.Join(s.Tags, "|") != "0|1|2" {
		t.Fatalf("tags %q", s.Tags)
	}
	if strings.Join(h.Crumbs, "|") != "open|a.png" || h.Context != "image/png" {
		t.Fatalf("heading %+v", h)
	}
	if !w.called("chafa -- " + p) {
		t.Fatalf("tag 1 is the terminal chafa; ran %q", w.ran())
	}
}

// E17, rule 15: with several targets the heading says which.
func TestHeadingNamesWhichTarget(t *testing.T) {
	w := newWorld(t)
	a, b := w.file("a.bin", "x"), w.file("b.bin", "y")
	w.types("application/x-weird")
	w.runner.On([]string{"file", "--"}, execx.Result{})
	w.picker.chooseRow("0")
	w.picker.chooseRow("0")
	if code := w.run(a, b); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	if got := w.picker.headings[1]; got.Context != "target 2 of 2 · application/x-weird" || got.Crumbs[1] != "b.bin" {
		t.Fatalf("heading %+v", got)
	}
}

// E17, E3: leaving the picker cancels, exit 130, and later targets are not opened.
func TestLeavingThePickerCancels(t *testing.T) {
	w := newWorld(t)
	a, b := w.file("a.bin", "x"), w.file("b.bin", "y")
	w.types("application/x-weird")
	w.picker.fail(ui.ErrLeave)
	if code := w.run(a, b); code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
	w.wantStderr("open: cancelled — nothing opened.\n")
	if len(w.picker.specs) != 1 || w.called("file --brief --mime-type -- "+b) {
		t.Fatalf("went on to b: %q", w.ran())
	}
}

// A picker that fails (not a leave, not fzf missing) is exit 1.
func TestPickerFailureFails(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.bin", "x")
	w.types("application/x-weird")
	w.picker.fail(&execx.ExitError{Name: "fzf", Code: 2})
	if code := w.run(p); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.wantStderr("open: fzf exited 2")
}

// E16, B17: no terminal to ask on is exit 1 with the way forward; nothing is read.
func TestNoTerminalToAskOn(t *testing.T) {
	for _, mode := range []string{"stderr", "tty"} {
		w := newWorld(t)
		p := w.file("a.bin", "x")
		w.types("application/x-weird")
		if mode == "stderr" {
			w.terminal = false
		} else {
			w.noTTY = true
		}
		if code := w.run(p); code != term.ExitFailure {
			t.Fatalf("%s: exit %d", mode, code)
		}
		w.wantStderr("open: no default handler for '" + p + "' (application/x-weird), and no terminal to ask on.\n")
		w.wantStderr("open --list")
		w.wantStderr("open --set-default")
		if len(w.picker.specs) != 0 {
			t.Fatalf("%s: picked", mode)
		}
	}
}

// numberedWorld is an unknown type with fzf missing and the terminal answering tty.
func numberedWorld(t *testing.T, tty string) (*world, string) {
	t.Helper()
	w := newWorld(t)
	p := w.file("a.bin", "x")
	w.types("application/x-weird")
	w.install("xxd", "less")
	w.runner.On([]string{"less"}, execx.Result{})
	w.runner.On([]string{"xxd"}, execx.Result{})
	w.runner.On([]string{"file", "--"}, execx.Result{})
	w.picker.fail(ui.ErrFzfMissing)
	w.tty = tty
	return w, p
}

// E18: without fzf, a numbered menu with the heading, read from the terminal.
func TestNumberedMenuChooses(t *testing.T) {
	w, p := numberedWorld(t, "3\n")
	if code := w.run(p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	for _, want := range []string{"open › a.bin", "application/x-weird", "   1) xxd (hex dump)\n", "   2) less (page)\n", "   3) file (identify only)\n", "Choice [1-3, q to cancel]: "} {
		w.wantStderr(want)
	}
	if !w.called("file -- " + p) {
		t.Fatalf("ran %q", w.ran())
	}
}

// E18: a bad answer re-prompts, at most 3 times.
func TestNumberedMenuRepromptsThenGivesUp(t *testing.T) {
	w, p := numberedWorld(t, "9\nabc\n2\n")
	if code := w.run(p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	if n := strings.Count(w.stderr.String(), "  Not one of the options — enter a number between 1 and 3, or q to cancel.\n"); n != 2 {
		t.Fatalf("%d complaints:\n%s", n, w.stderr.String())
	}
	if !w.called("less -- " + p) {
		t.Fatalf("ran %q", w.ran())
	}
	w, p = numberedWorld(t, "0\n\n-1\n2\n")
	if code := w.run(p); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.wantStderr("open: giving up after 3 attempts — nothing opened.\n")
	if w.called("less -- " + p) {
		t.Fatal("ran after giving up")
	}
}

// E18: q, quit and end of input cancel.
func TestNumberedMenuCancels(t *testing.T) {
	for _, tty := range []string{"q\n", "quit\n", "Q\n", "", "1"} {
		w, p := numberedWorld(t, tty)
		if code := w.run(p); code != term.ExitCancelled {
			t.Fatalf("%q: exit %d", tty, code)
		}
		w.wantStderr("open: cancelled — nothing opened.\n")
		if w.called("xxd -- " + p) {
			t.Fatalf("%q: ran", tty)
		}
	}
}

// E19: Ctrl-C at the numbered prompt cancels; a stale interrupt from before the prompt does not.
func TestInterruptAtThePromptCancels(t *testing.T) {
	w, p := numberedWorld(t, "")
	tty := &interruptingTTY{signals: w.signals, release: make(chan struct{})}
	t.Cleanup(func() { close(tty.release) })
	app := w.app()
	app.Env.OpenTTY = func() (io.ReadCloser, error) { return tty, nil }
	w.signals <- os.Interrupt // stale: from before the prompt, so drained
	code := app.Run(t.Context(), []string{p})
	if code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
	w.wantStderr("Choice [1-3, q to cancel]: ")
	w.wantStderr("open: cancelled — nothing opened.\n")
	if !tty.reading.Load() {
		t.Fatal("the prompt was never read: the stale interrupt cancelled it")
	}
}

// interruptingTTY is a terminal on which the person presses Ctrl-C once the prompt is being
// read: its first Read delivers an interrupt and then blocks, as a terminal with no input does.
type interruptingTTY struct {
	signals chan os.Signal
	release chan struct{}
	reading atomic.Bool
}

func (t *interruptingTTY) Read([]byte) (int, error) {
	if !t.reading.Swap(true) {
		t.signals <- os.Interrupt
	}
	<-t.release
	return 0, io.EOF
}

func (*interruptingTTY) Close() error { return nil }
