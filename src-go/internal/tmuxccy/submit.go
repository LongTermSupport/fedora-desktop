package tmuxccy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
)

// ErrNotSubmitted is a Submit whose text stayed in Claude's input box after every Enter.
var ErrNotSubmitted = errors.New("the typed text stayed in the input box")

// SubmitEnters is the most Enters a Submit presses; submitLooks is how often it reads the box
// after each, submitLookEvery apart. The bound is about five seconds of a TUI that is not taking
// its Enter.
const (
	SubmitEnters    = 5
	submitLooks     = 5
	submitLookEvery = 200 * time.Millisecond
)

// SubmitPace paces a Submit: Pause before each Enter (an Enter arriving with the text is read as
// part of it), Sleep the wait itself, nil for the Tmux's own.
type SubmitPace struct {
	Pause time.Duration
	Sleep func(time.Duration)
}

// Submit types text into Claude's input box in the session's pane and enters it, checking that it
// was taken: after each Enter it reads the box until the text has left it, and presses Enter
// again while it has not. An Enter can arrive before the TUI is ready, or while it is still taking
// the text, and be lost (fedora-desktop#264). Text still in the box after SubmitEnters Enters is
// ErrNotSubmitted; a failed key or read is returned as it is.
func (t *Tmux) Submit(ctx context.Context, name, text string, pace SubmitPace) error {
	sleep := pace.Sleep
	if sleep == nil {
		sleep = t.pause
	}
	if err := t.SendKeys(ctx, name, text); err != nil {
		return err
	}
	for range SubmitEnters {
		sleep(pace.Pause)
		if err := t.SendEnter(ctx, name); err != nil {
			return fmt.Errorf("%w (the line was typed, not entered)", err)
		}
		taken, err := t.boxEmptied(ctx, name, sleep)
		if err != nil || taken {
			return err
		}
	}
	return fmt.Errorf("%w of %s after %d Enters", ErrNotSubmitted, name, SubmitEnters)
}

// boxEmptied is whether the session's box, read a few times, was seen without typed text. A
// session gone after the Enter holds no text: the line took it there (/exit ends the session).
func (t *Tmux) boxEmptied(ctx context.Context, name string, sleep func(time.Duration)) (bool, error) {
	for range submitLooks {
		sleep(submitLookEvery)
		pane, err := t.CapturePane(ctx, name, true)
		if err != nil && mentions(err.Error(), "can't find", "no server running") {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if screen.ClaudeState(pane) != screen.Typed {
			return true, nil
		}
	}
	return false, nil
}
