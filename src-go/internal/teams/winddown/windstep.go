package winddown

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// goalClear is the clearing form of Claude Code's /goal: a bare /goal only prints its status.
const goalClear = "/goal clear"

// whyUnsent is a box holding text before anything was typed (D3).
const whyUnsent = "its input box holds unsent text; attach it (ccy-sessions), send or clear it, and run wind-down again"

// result is where a session's wind-down stands.
type result int

const (
	resultPending result = iota
	resultDone
	resultFailed
)

// phase is how far the keystrokes have gone (W5).
type phase int

const (
	// phaseIdle waits for an idle, empty box before /goal clear is typed.
	phaseIdle phase = iota
	// phaseGoal has entered /goal clear and waits for the box to be empty and idle again.
	phaseGoal
	// phaseText has entered the wind-down text and waits for the box to be empty.
	phaseText
)

// job is one session being wound down: what it is, and how far it has gone.
type job struct {
	name, dir, seat, handle, team string
	coordinator                   bool
	others                        []seatRef
	// tmux is the server the session's pane is on, pane its tmux session, prefix the launcher
	// restore.Judge reads it as; where, for an LXC member, says where it runs, and missing is
	// why a session not there before anything was typed failed.
	tmux           *tmuxccy.Tmux
	pane, prefix   string
	where, missing string

	phase phase
	// graced is the one poll a line still in the box is given before Enter is pressed again,
	// reentered whether it has been.
	graced, reentered bool
	// idleBy bounds the wait for an idle session, settleBy the wait for a typed line to go.
	idleBy, settleBy time.Time
	result           result
	note             string
}

func (j *job) fail(note string) { j.result, j.note = resultFailed, note }

// after is a failure note that says how far the job had gone: once /goal clear is in, the goal
// is gone whatever else fails, and the owner needs to know it.
func (j *job) after(note string) string {
	if j.phase == phaseIdle {
		return note + "; nothing was typed"
	}
	return "/goal clear went in, but " + note
}

// windAll reads every pane, then again every poll, until each job is done or failed (W6): the
// jobs are watched together, so a slow session does not hold the others up.
func (w *WindDown) windAll(ctx context.Context, jobs []*job) {
	start := w.Now()
	for _, j := range jobs {
		j.idleBy = start.Add(w.Waits.Idle)
	}
	for {
		lists := map[*tmuxccy.Tmux]listing{}
		left := false
		for _, j := range jobs {
			if j.result != resultPending {
				continue
			}
			list, read := lists[j.tmux]
			if !read {
				list.live, list.err = liveSessions(ctx, j.tmux)
				lists[j.tmux] = list
			}
			if list.err != nil {
				j.fail(j.after(list.err.Error() + "; the sessions could not be watched any further"))
				continue
			}
			w.step(ctx, j, list.live)
			left = left || j.result == resultPending
		}
		if !left {
			return
		}
		w.Sleep(w.Waits.Poll)
	}
}

// listing is one pass's read of a tmux server's sessions.
type listing struct {
	live map[string]bool
	err  error
}

func liveSessions(ctx context.Context, t *tmuxccy.Tmux) (map[string]bool, error) {
	list, err := t.List(ctx)
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for _, sess := range list {
		live[sess.Name] = true
	}
	return live, nil
}

// step is one look at a session and what follows from it.
func (w *WindDown) step(ctx context.Context, j *job, live map[string]bool) {
	if !live[j.pane] {
		w.ended(j)
		return
	}
	pane, err := j.tmux.CapturePane(ctx, j.pane, true)
	if err != nil {
		// A session that ended since the list was read is seen gone on the next pass.
		if !strings.Contains(err.Error(), "can't find") {
			j.fail(j.after(err.Error()))
		}
		return
	}
	verdict := restore.Judge(j.prefix, true, screen.Plain(pane), restore.ContainerUp, restore.GoingOutcome{})
	switch verdict.State {
	case restore.StateDead, restore.StateWaitingAtPrompt:
		j.fail(j.after("claude is not running in it (" + verdict.Detail + ")"))
		return
	case restore.StateOK, restore.StateStarting, restore.StateSettingGoing, restore.StateNotSetGoing:
	}
	if err := w.act(ctx, j, screen.ClaudeState(pane)); err != nil {
		j.fail(j.after(err.Error()))
	}
}

// ended is a session whose tmux session has gone: done only once the wind-down was entered.
func (w *WindDown) ended(j *job) {
	switch j.phase {
	case phaseText:
		j.result = resultDone
	case phaseGoal:
		j.fail(j.after("the session ended before the wind-down was typed"))
	case phaseIdle:
		j.fail(j.missing)
	}
}

// act is what the box calls for in the job's phase (W5, W7).
func (w *WindDown) act(ctx context.Context, j *job, state screen.State) error {
	now := w.Now()
	switch j.phase {
	case phaseIdle:
		switch state {
		case screen.Ready:
			return w.typeLine(ctx, j, goalClear, phaseGoal)
		case screen.Typed:
			j.fail(whyUnsent)
		case screen.Busy, screen.NotDrawn:
			w.waitIdle(j, state, now)
		}
	case phaseGoal:
		switch state {
		case screen.Ready:
			return w.typeLine(ctx, j, Instruction(j.team, j.coordinator), phaseText)
		case screen.Typed:
			return w.reenter(ctx, j, now, "/goal clear stayed in its input box after a second Enter; the wind-down was not typed")
		case screen.Busy, screen.NotDrawn:
			w.waitIdle(j, state, now)
		}
	case phaseText:
		switch state {
		case screen.Ready, screen.Busy:
			j.result = resultDone
		case screen.Typed:
			return w.reenter(ctx, j, now, j.after("the wind-down stayed in its input box after a second Enter"))
		case screen.NotDrawn:
			if !now.Before(j.settleBy) {
				j.fail(j.after("its input box was not drawn again within " + seconds(w.Waits.Settle) + " of the wind-down; look at it (ccy-sessions)"))
			}
		}
	}
	return nil
}

// waitIdle fails the job once a busy or undrawn session has kept it waiting past the bound.
func (w *WindDown) waitIdle(j *job, state screen.State, now time.Time) {
	if now.Before(j.idleBy) {
		return
	}
	if state == screen.Busy {
		j.fail(j.after("it stayed busy for " + seconds(w.Waits.Idle)))
		return
	}
	j.fail(j.after("its input box was not drawn within " + seconds(w.Waits.Idle)))
}

// reenter gives a line left in the box one poll of grace (a line slow to clear is not entered
// twice), then presses Enter once more, then fails the job once the line is still there past
// the bound.
func (w *WindDown) reenter(ctx context.Context, j *job, now time.Time, note string) error {
	if !j.graced {
		j.graced = true
		return nil
	}
	if !j.reentered {
		j.reentered = true
		return j.tmux.SendEnter(ctx, j.pane)
	}
	if !now.Before(j.settleBy) {
		j.fail(note)
	}
	return nil
}

// typeLine types text, then presses Enter on its own after the submit pause, as ccy-sessions
// types a line: an Enter arriving with the text is read as part of it. The job moves to next,
// with fresh bounds.
func (w *WindDown) typeLine(ctx context.Context, j *job, text string, next phase) error {
	if err := j.tmux.SendKeys(ctx, j.pane, text); err != nil {
		return err
	}
	w.Sleep(w.Waits.Submit)
	if err := j.tmux.SendEnter(ctx, j.pane); err != nil {
		return fmt.Errorf("%w (the line was typed, not entered)", err)
	}
	now := w.Now()
	j.phase, j.graced, j.reentered = next, false, false
	j.settleBy, j.idleBy = now.Add(w.Waits.Settle), now.Add(w.Waits.Idle)
	return nil
}
