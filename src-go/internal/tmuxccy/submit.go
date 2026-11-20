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

// ErrBoxNotDrawn is a Submit that, after an Enter, never saw Claude's input box again: whether the
// line was taken is not known, and no more Enters are pressed into a screen the reader does not
// know (a dialog, an editor, Claude Code exiting).
var ErrBoxNotDrawn = errors.New("the input box was not drawn after the Enter")

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
// was taken: after each Enter it reads the box until it is seen empty or busy, and presses Enter
// again while it still holds text. An Enter can arrive before the TUI is ready, or while it is
// still taking the text, and be lost (fedora-desktop#264); Claude Code can hold the line for
// review, which is text still to enter (fedora-desktop#288). Text still in the box after
// SubmitEnters Enters is ErrNotSubmitted, naming what the box shows; a screen with no box is
// never taken as taken, and is ErrBoxNotDrawn; a failed key or read is returned as it is.
func (t *Tmux) Submit(ctx context.Context, name, text string, pace SubmitPace) error {
	sleep := pace.Sleep
	if sleep == nil {
		sleep = t.pause
	}
	if err := t.SendKeys(ctx, name, text); err != nil {
		return err
	}
	pane := ""
	for enter := range SubmitEnters {
		sleep(pace.Pause)
		if err := t.SendEnter(ctx, name); err != nil {
			return fmt.Errorf("%w (the line was typed, not entered)", err)
		}
		seen, taken, err := t.boxTaken(ctx, name, sleep)
		if err != nil || taken {
			return err
		}
		pane = seen
		switch screen.ClaudeState(pane) {
		case screen.NotDrawn:
			return fmt.Errorf("%w of %s (Enter %d of %d); the screen ends %q", ErrBoxNotDrawn, name, enter+1, SubmitEnters, screen.LastLine(pane))
		case screen.Typed, screen.Ready, screen.Busy:
		}
	}
	return &NotSubmittedError{Name: name, Shows: boxShows(pane)}
}

// NotSubmittedError is ErrNotSubmitted for one session, with what its box showed at the end.
type NotSubmittedError struct {
	Name  string
	Shows string
}

func (e *NotSubmittedError) Error() string {
	return fmt.Sprintf("%s of %s after %d Enters; %s", ErrNotSubmitted, e.Name, SubmitEnters, e.Shows)
}

// Is makes errors.Is(err, ErrNotSubmitted) hold.
func (e *NotSubmittedError) Is(target error) bool { return target == ErrNotSubmitted }

// boxTaken reads the session's box a few times after an Enter: taken once it is seen empty or
// busy, or the session is gone (the line took it there: /exit ends the session). A box still
// holding text and a screen with no box are not taken; the last screen read is returned.
func (t *Tmux) boxTaken(ctx context.Context, name string, sleep func(time.Duration)) (string, bool, error) {
	pane := ""
	for range submitLooks {
		sleep(submitLookEvery)
		var err error
		pane, err = t.CapturePane(ctx, name, true)
		if errors.Is(err, ErrSessionGone) {
			return "", true, nil
		}
		if err != nil {
			return "", false, err
		}
		switch screen.ClaudeState(pane) {
		case screen.Ready, screen.Busy:
			return pane, true, nil
		case screen.Typed, screen.NotDrawn:
		}
	}
	return pane, false, nil
}

// boxShows is what a box that kept its text shows: the box line and Claude Code's line above it.
func boxShows(pane string) string {
	shows := fmt.Sprintf("its box shows %q", screen.Plain(screen.InputLine(pane)))
	if above := screen.AboveTheBox(pane); above != "" {
		shows += fmt.Sprintf(", above it: %q", above)
	}
	return shows
}
