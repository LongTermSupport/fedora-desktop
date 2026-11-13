package tmuxccy_test

import (
	"context"
	"errors"
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
}

func (p *submitPane) fake() *execx.Fake {
	f := &execx.Fake{}
	f.OnFunc([]string{"tmux", "-L", "ccy", "send-keys", "-t", "=alpha:"}, func(c execx.Cmd) (execx.Result, error) {
		key := c.Args[len(c.Args)-1]
		p.presses = append(p.presses, key)
		if slices.Contains(c.Args, "-l") {
			p.typed = key
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
		if err := newTmux(f).Submit(context.Background(), "alpha", "/exit", pace(&slept)); err != nil {
			t.Fatalf("%q: got %v", msg, err)
		}
	}
}

func TestSubmitCarriesAFailedKeyOrRead(t *testing.T) {
	var slept []time.Duration
	f := &execx.Fake{}
	f.On([]string{"tmux"}, fail("no such pane"))
	if err := newTmux(f).Submit(context.Background(), "alpha", "x", pace(&slept)); err == nil || !strings.Contains(err.Error(), "typing into alpha failed") {
		t.Fatalf("got %v", err)
	}
	f = &execx.Fake{}
	f.On([]string{"tmux"}, fail("no such pane"))
	f.On([]string{"tmux", "-L", "ccy", "send-keys", "-t", "=alpha:", "-l", "x"}, ok(""))
	err := newTmux(f).Submit(context.Background(), "alpha", "x", pace(&slept))
	if err == nil || !strings.Contains(err.Error(), "pressing Enter in alpha failed") || !strings.Contains(err.Error(), "the line was typed, not entered") {
		t.Fatalf("got %v", err)
	}
	f = &execx.Fake{}
	f.On([]string{"tmux"}, fail("no such pane"))
	f.On([]string{"tmux", "-L", "ccy", "send-keys"}, ok(""))
	if err = newTmux(f).Submit(context.Background(), "alpha", "x", pace(&slept)); err == nil || !strings.Contains(err.Error(), "the screen of alpha could not be read") {
		t.Fatalf("got %v", err)
	}
}
