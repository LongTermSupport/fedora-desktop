// Package loneenter presses a lone Enter outside tmuxccy in the shapes of the four bare re-Enters
// and dialog answer the rule must catch (Plan 00257), beside the mode-checked Enter it must not.
package loneenter

import (
	"context"

	"fix/sub/internal/tmuxccy"
)

type job struct {
	tmux              *tmuxccy.Tmux
	pane              string
	reentered         bool
	answered          bool
	compactReentered  bool
	exitFocusedOnLook bool
}

// reenterExit is sessclose's /exit re-Enter: copy mode takes it, and the line is called stuck.
func reenterExit(ctx context.Context, j *job) error {
	if !j.reentered {
		j.reentered = true
		return j.tmux.SendEnter(ctx, j.pane) // want `a lone SendEnter outside tmuxccy`
	}
	return nil
}

// reenterCompact is sessclose's /compact re-Enter.
func reenterCompact(ctx context.Context, j *job) error {
	if !j.compactReentered {
		j.compactReentered = true
		return j.tmux.SendEnter(ctx, j.pane) // want `a lone SendEnter outside tmuxccy`
	}
	return nil
}

// answerBackgroundWork is the dialog answer marked answered when copy mode took the Enter.
func answerBackgroundWork(ctx context.Context, j *job) error {
	if !j.exitFocusedOnLook {
		return nil
	}
	if err := j.tmux.SendEnter(ctx, j.pane); err != nil { // want `a lone SendEnter outside tmuxccy`
		return err
	}
	j.answered = true
	return nil
}

// windDownReenter is the wind-down's re-Enter, taken as a method value.
func windDownReenter(ctx context.Context, j *job) error {
	enter := j.tmux.SendEnter // want `a lone SendEnter outside tmuxccy`
	return enter(ctx, j.pane)
}

// pressed is the fix: the Enter that leaves copy or view mode first and refuses any other.
func pressed(ctx context.Context, j *job) error {
	if err := j.tmux.PressEnter(ctx, j.pane); err != nil {
		return err
	}
	j.answered = true
	return nil
}
