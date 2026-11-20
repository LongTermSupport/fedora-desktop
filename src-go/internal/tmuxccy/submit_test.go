package tmuxccy_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

const boxRule = "────────────────────────────────────────"

// boxScreen is Claude's input box holding text ("" for an empty box).
func boxScreen(text string) string {
	return strings.Join([]string{" ▐▛███▜▌   Claude Code", "", boxRule, "❯ " + text, boxRule, ""}, "\n")
}

// submitPane is a pane whose box takes the typed text and keeps it for the first lost Enters,
// as a TUI not yet ready does (fedora-desktop#264); the Enter after those submits it.
type submitPane struct {
	lost    int
	typed   string
	enters  int
	looks   int
	presses []string
	// mode is the tmux mode the pane is in ("" for none), as an attached person who scrolled
	// back leaves it (copy-mode); keys sent in a mode go to the mode, not to Claude. stuck keeps
	// it there after a cancel; modeOnType enters it as the text is typed.
	mode       string
	stuck      bool
	modeOnType string
}

// inMode answers display-message for the pane's mode.
func (p *submitPane) inMode(f *execx.Fake) {
	f.OnFunc([]string{"tmux", "-L", "ccy", "display-message", "-p", "-t", "=alpha:", "#{pane_mode}"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: []byte(p.mode + "\n")}, nil
	})
}

func (p *submitPane) fake() *execx.Fake {
	f := &execx.Fake{}
	p.inMode(f)
	f.OnFunc([]string{"tmux", "-L", "ccy", "send-keys", "-t", "=alpha:"}, func(c execx.Cmd) (execx.Result, error) {
		key := c.Args[len(c.Args)-1]
		p.presses = append(p.presses, key)
		switch {
		case slices.Contains(c.Args, "-X"):
			if p.mode == "" {
				return execx.Result{ExitCode: 1, Stderr: []byte("not in a mode")}, nil
			}
			if !p.stuck {
				p.mode = ""
			}
			return execx.Result{}, nil
		case p.mode != "":
			return execx.Result{}, nil
		case slices.Contains(c.Args, "-l"):
			p.typed = key
			p.mode = p.modeOnType
			return execx.Result{}, nil
		}
		p.enters++
		if p.enters > p.lost {
			p.typed = ""
		}
		return execx.Result{}, nil
	})
	f.OnFunc([]string{"tmux", "-L", "ccy", "capture-pane", "-p", "-e", "-t", "=alpha:"}, func(execx.Cmd) (execx.Result, error) {
		p.looks++
		return execx.Result{Stdout: []byte(boxScreen(p.typed))}, nil
	})
	return f
}

// pace records the pauses a Submit asks for and takes none.
func pace(slept *[]time.Duration) tmuxccy.SubmitPace {
	return tmuxccy.SubmitPace{Pause: 200 * time.Millisecond, Sleep: func(d time.Duration) { *slept = append(*slept, d) }}
}

func TestSubmitTypesPausesEntersAndSeesTheBoxEmpty(t *testing.T) {
	p := &submitPane{}
	var slept []time.Duration
	if err := newTmux(p.fake()).Submit(context.Background(), "alpha", "/compact now", pace(&slept)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"/compact now", "Enter"}; !slices.Equal(p.presses, want) {
		t.Fatalf("pressed %q, want %q", p.presses, want)
	}
	if p.looks != 1 || len(slept) < 1 || slept[0] != 200*time.Millisecond {
		t.Fatalf("looks %d, slept %v: the Enter waits the pause after the text, then the box is read", p.looks, slept)
	}
}

// A pane left in copy mode (an attached person scrolled back) sends every key to the mode, while
// the box, read from the pane beneath, still looks Ready: the line would be lost and called
// taken. Submit leaves the mode first, then types.
func TestSubmitLeavesCopyModeBeforeTyping(t *testing.T) {
	for _, mode := range []string{"copy-mode", "view-mode"} {
		p := &submitPane{mode: mode}
		var slept []time.Duration
		if err := newTmux(p.fake()).Submit(context.Background(), "alpha", "/compact now", pace(&slept)); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if want := []string{"cancel", "/compact now", "Enter"}; !slices.Equal(p.presses, want) {
			t.Fatalf("%s: pressed %q, want %q", mode, p.presses, want)
		}
	}
}

// A mode Submit does not know how to leave (a tree or buffer chooser), or one that stays after
// the cancel, is refused before anything is typed.
func TestSubmitRefusesAPaneItCannotTakeOutOfItsMode(t *testing.T) {
	for _, p := range []*submitPane{{mode: "tree-mode"}, {mode: "copy-mode", stuck: true}} {
		var slept []time.Duration
		err := newTmux(p.fake()).Submit(context.Background(), "alpha", "/compact now", pace(&slept))
		if !errors.Is(err, tmuxccy.ErrPaneInMode) || !strings.Contains(err.Error(), p.mode) ||
			!strings.Contains(err.Error(), "nothing was typed") {
			t.Fatalf("%s: got %v, want ErrPaneInMode naming the mode", p.mode, err)
		}
		if slices.Contains(p.presses, "/compact now") || slices.Contains(p.presses, "Enter") {
			t.Fatalf("%s: pressed %q", p.mode, p.presses)
		}
	}
}

// A pane that enters copy mode while the line is typed took the keys into the mode: the line is
// lost, so no Enter is pressed (in copy mode it would copy and leave), and it is not called taken.
func TestSubmitDoesNotEnterALineTypedIntoCopyMode(t *testing.T) {
	p := &submitPane{modeOnType: "copy-mode"}
	var slept []time.Duration
	err := newTmux(p.fake()).Submit(context.Background(), "alpha", "/compact now", pace(&slept))
	if !errors.Is(err, tmuxccy.ErrPaneInMode) || !strings.Contains(err.Error(), "may be lost") {
		t.Fatalf("got %v, want ErrPaneInMode saying the line may be lost", err)
	}
	if slices.Contains(p.presses, "Enter") {
		t.Fatalf("pressed %q", p.presses)
	}
}

// fedora-desktop#264: the first Enter is lost; the box still holds the text, so Enter is pressed
// again and the second one submits it.
func TestSubmitPressesEnterAgainWhileTheTextStaysInTheBox(t *testing.T) {
	p := &submitPane{lost: 1}
	var slept []time.Duration
	if err := newTmux(p.fake()).Submit(context.Background(), "alpha", "/compact now", pace(&slept)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"/compact now", "Enter", "Enter"}; !slices.Equal(p.presses, want) {
		t.Fatalf("pressed %q, want %q", p.presses, want)
	}
}

func TestSubmitFailsLoudlyWhenTheTextNeverLeavesTheBox(t *testing.T) {
	p := &submitPane{lost: 1000}
	var slept []time.Duration
	err := newTmux(p.fake()).Submit(context.Background(), "alpha", "/compact now", pace(&slept))
	if !errors.Is(err, tmuxccy.ErrNotSubmitted) || !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("got %v, want ErrNotSubmitted naming the session", err)
	}
	if p.enters != tmuxccy.SubmitEnters {
		t.Fatalf("pressed Enter %d times, want the bound %d", p.enters, tmuxccy.SubmitEnters)
	}
}

// /exit ends the session: a pane gone after the Enter holds no text, so the line was taken.
func TestSubmitTakesASessionGoneAfterTheEnterAsSubmitted(t *testing.T) {
	var slept []time.Duration
	for _, msg := range []string{"can't find pane: alpha", "no server running on /tmp/x"} {
		f := &execx.Fake{}
		f.On([]string{"tmux", "-L", "ccy", "capture-pane"}, fail(msg))
		f.On([]string{"tmux", "-L", "ccy", "send-keys"}, ok(""))
		f.On([]string{"tmux", "-L", "ccy", "display-message"}, ok("\n"))
		if err := newTmux(f).Submit(context.Background(), "alpha", "/exit", pace(&slept)); err != nil {
			t.Fatalf("%q: got %v", msg, err)
		}
	}
}

// heldScreen is Claude Code holding a typed line back for review (fedora-desktop#288): the line,
// cleaned, still in the box, its notice above.
func heldScreen(text string) string {
	return strings.Join([]string{"● done", "", "   Removed 1 invisible character · review and press Enter to send", boxRule, "❯ " + text, boxRule, ""}, "\n")
}

// scriptedPane answers the looks from a script of screens, one per look, the last repeated;
// every Enter is recorded.
type scriptedPane struct {
	screens []string
	looks   int
	enters  int
}

func (p *scriptedPane) fake() *execx.Fake {
	f := &execx.Fake{}
	f.On([]string{"tmux", "-L", "ccy", "display-message"}, ok("\n"))
	f.OnFunc([]string{"tmux", "-L", "ccy", "send-keys", "-t", "=alpha:"}, func(c execx.Cmd) (execx.Result, error) {
		if !slices.Contains(c.Args, "-l") {
			p.enters++
		}
		return execx.Result{}, nil
	})
	f.OnFunc([]string{"tmux", "-L", "ccy", "capture-pane", "-p", "-e", "-t", "=alpha:"}, func(execx.Cmd) (execx.Result, error) {
		s := p.screens[min(p.looks, len(p.screens)-1)]
		p.looks++
		return execx.Result{Stdout: []byte(s)}, nil
	})
	return f
}

// capture is a tmux capture of Claude Code kept in internal/screen's testdata.
func capture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(filepath.Join("..", "screen", "testdata", name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// fedora-desktop#288: a line Claude Code held for review is text still to enter, so the next
// Enter is pressed, and it sends the line. The screens are captures of Claude Code 2.1.325.
func TestSubmitEntersALineHeldForReviewAgain(t *testing.T) {
	var slept []time.Duration
	held := capture(t, "held-for-review.txt")
	sent := capture(t, "held-for-review-then-sent.txt")
	p := &scriptedPane{screens: []string{held, held, held, held, held, sent}}
	if err := newTmux(p.fake()).Submit(context.Background(), "alpha", "/compact now", pace(&slept)); err != nil {
		t.Fatal(err)
	}
	if p.enters != 2 {
		t.Fatalf("pressed Enter %d times, want 2: the held line is entered again", p.enters)
	}
}

// Giving up names what the box showed and what Claude Code said above it.
func TestSubmitNamesTheHeldStateWhenItGivesUp(t *testing.T) {
	var slept []time.Duration
	p := &scriptedPane{screens: []string{heldScreen("/compact now")}}
	err := newTmux(p.fake()).Submit(context.Background(), "alpha", "/compact now", pace(&slept))
	if !errors.Is(err, tmuxccy.ErrNotSubmitted) {
		t.Fatalf("got %v, want ErrNotSubmitted", err)
	}
	for _, want := range []string{
		"the typed text stayed in the input box of alpha after 5 Enters",
		`its box shows "❯ /compact now"`,
		`above it: "Removed 1 invisible character · review and press Enter to send"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not say %q", err, want)
		}
	}
}

// A screen with no input box after the Enter is not proof the line was taken: Submit keeps
// looking, and a box drawn again decides.
func TestSubmitDoesNotTakeAScreenWithNoBoxAsTaken(t *testing.T) {
	var slept []time.Duration
	redraw := "Resuming…\n"
	typed := boxScreen("/compact now")
	p := &scriptedPane{screens: []string{redraw, redraw, typed, typed, typed, boxScreen("")}}
	if err := newTmux(p.fake()).Submit(context.Background(), "alpha", "/compact now", pace(&slept)); err != nil {
		t.Fatal(err)
	}
	if p.enters != 2 {
		t.Fatalf("pressed Enter %d times, want 2: the box came back with the line in it", p.enters)
	}
}

// A screen that keeps no box gets no more Enters, which could answer whatever it shows: Submit
// says the box was not drawn, with the screen's last line.
func TestSubmitStopsAtAScreenThatDrawsNoBox(t *testing.T) {
	var slept []time.Duration
	p := &scriptedPane{screens: []string{"Do you want to proceed?\n❯ 1. Yes\n  2. No\n"}}
	err := newTmux(p.fake()).Submit(context.Background(), "alpha", "/compact now", pace(&slept))
	if !errors.Is(err, tmuxccy.ErrBoxNotDrawn) || errors.Is(err, tmuxccy.ErrNotSubmitted) {
		t.Fatalf("got %v, want ErrBoxNotDrawn alone", err)
	}
	if !strings.Contains(err.Error(), "alpha") || !strings.Contains(err.Error(), `"2. No"`) {
		t.Fatalf("%q does not name the session and the screen", err)
	}
	if p.enters != 1 {
		t.Fatalf("pressed Enter %d times, want 1", p.enters)
	}
}

func TestSubmitCarriesAFailedKeyOrRead(t *testing.T) {
	var slept []time.Duration
	f := &execx.Fake{}
	f.On([]string{"tmux"}, fail("no such pane"))
	if err := newTmux(f).Submit(context.Background(), "alpha", "x", pace(&slept)); err == nil || !strings.Contains(err.Error(), "reading the mode of alpha's pane failed") {
		t.Fatalf("got %v", err)
	}
	f = &execx.Fake{}
	f.On([]string{"tmux"}, fail("no such pane"))
	f.On([]string{"tmux", "-L", "ccy", "display-message"}, ok("\n"))
	if err := newTmux(f).Submit(context.Background(), "alpha", "x", pace(&slept)); err == nil || !strings.Contains(err.Error(), "typing into alpha failed") {
		t.Fatalf("got %v", err)
	}
	f = &execx.Fake{}
	f.On([]string{"tmux"}, fail("no such pane"))
	f.On([]string{"tmux", "-L", "ccy", "display-message"}, ok("\n"))
	f.On([]string{"tmux", "-L", "ccy", "send-keys", "-t", "=alpha:", "-l", "x"}, ok(""))
	err := newTmux(f).Submit(context.Background(), "alpha", "x", pace(&slept))
	if err == nil || !strings.Contains(err.Error(), "pressing Enter in alpha failed") || !strings.Contains(err.Error(), "the line was typed, not entered") {
		t.Fatalf("got %v", err)
	}
	f = &execx.Fake{}
	f.On([]string{"tmux"}, fail("no such pane"))
	f.On([]string{"tmux", "-L", "ccy", "display-message"}, ok("\n"))
	f.On([]string{"tmux", "-L", "ccy", "send-keys"}, ok(""))
	if err = newTmux(f).Submit(context.Background(), "alpha", "x", pace(&slept)); err == nil || !strings.Contains(err.Error(), "the screen of alpha could not be read") {
		t.Fatalf("got %v", err)
	}
}
