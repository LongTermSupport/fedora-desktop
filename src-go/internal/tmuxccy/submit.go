package tmuxccy

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
)

// ErrNotSubmitted is a Submit whose text stayed in Claude's input box after every Enter.
var ErrNotSubmitted = errors.New("the typed text stayed in the input box")

// ErrBoxNotDrawn is a Submit that, after an Enter, never saw Claude's input box again: whether the
// line was taken is not known, and no more Enters are pressed into a screen the reader does not
// know (a dialog, an editor, Claude Code exiting).
var ErrBoxNotDrawn = errors.New("the input box was not drawn after the Enter")

// ErrPaneInMode is a Submit into a pane in a tmux mode (copy mode, a chooser): the mode takes
// every key, while the screen beneath it, which the box is read from, still shows the box.
var ErrPaneInMode = errors.New("the pane is in a tmux mode")

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
//
// A pane in a tmux mode sends every key to the mode, while the box is read from the screen
// beneath it, which would call a lost line taken. So a pane in copy or view mode (a person who
// scrolled back) is taken out of it before the text is typed, any other mode is ErrPaneInMode
// with nothing typed, and a pane found in a mode before an Enter (it entered one while the text
// was typed) is ErrPaneInMode with no Enter pressed, the line perhaps lost.
func (t *Tmux) Submit(ctx context.Context, name, text string, pace SubmitPace) error {
	sleep := pace.Sleep
	if sleep == nil {
		sleep = t.pause
	}
	if err := t.leaveMode(ctx, name); err != nil {
		return err
	}
	if err := t.SendKeys(ctx, name, text); err != nil {
		return err
	}
	pane := ""
	for enter := range SubmitEnters {
		sleep(pace.Pause)
		mode, err := t.paneMode(ctx, name)
		if err != nil {
			return fmt.Errorf("%w (the line was typed, not entered)", err)
		}
		if mode != "" {
			return fmt.Errorf("%w (%s) in %s: it entered it while the line was typed, so the line may be lost; "+
				"no Enter was pressed (Enter %d of %d)", ErrPaneInMode, mode, name, enter+1, SubmitEnters)
		}
		if err = t.SendEnter(ctx, name); err != nil {
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

// leavableModes are the modes a Submit takes a pane out of: the scrollback a person reads, which
// nothing is lost by leaving. Any other mode (a tree or buffer chooser) is the person's choice
// still to make.
var leavableModes = []string{"copy-mode", "view-mode"}

// leaveMode makes sure keys typed into the session's pane reach the program in it: a pane in copy
// or view mode is taken out of it (send-keys -X cancel), and checked again; any other mode, or one
// the pane stays in, is ErrPaneInMode.
func (t *Tmux) leaveMode(ctx context.Context, name string) error {
	mode, err := t.paneMode(ctx, name)
	if err != nil || mode == "" {
		return err
	}
	if !slices.Contains(leavableModes, mode) {
		return fmt.Errorf("%w (%s) in %s, which Submit does not leave; nothing was typed", ErrPaneInMode, mode, name)
	}
	if _, err = t.Run(ctx, "send-keys", "-t", paneTarget(name), "-X", "cancel"); err != nil {
		return fmt.Errorf("leaving %s in %s failed: %s; nothing was typed", mode, name, reason(err))
	}
	if mode, err = t.paneMode(ctx, name); err != nil || mode == "" {
		return err
	}
	return fmt.Errorf("%w (%s) in %s, still after it was cancelled; nothing was typed", ErrPaneInMode, mode, name)
}

// paneMode is the tmux mode the session's pane is in, "" for none.
func (t *Tmux) paneMode(ctx context.Context, name string) (string, error) {
	res, err := t.Run(ctx, "display-message", "-p", "-t", paneTarget(name), "#{pane_mode}")
	if err != nil {
		return "", fmt.Errorf("reading the mode of %s's pane failed: %s", name, reason(err))
	}
	return strings.TrimSpace(string(res.Stdout)), nil
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
