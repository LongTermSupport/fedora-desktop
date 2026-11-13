// Package badsubmit types into a pane and submits it in the ways the rule must catch, beside
// the ways it must not.
package badsubmit

import (
	"context"

	"fix/sub/internal/tmuxccy"
)

// typeInto is the defect of fedora-desktop#264: typed, paused, one Enter, never checked.
func typeInto(ctx context.Context, t *tmuxccy.Tmux, name, text string, pause func()) error {
	if err := t.SendKeys(ctx, name, text); err != nil {
		return err
	}
	pause()
	return t.SendEnter(ctx, name) // want `submitted with an Enter nothing checks`
}

// methodValue hides the Enter behind a method value.
func methodValue(ctx context.Context, t *tmuxccy.Tmux, name string) error {
	enter := t.SendEnter // want `submitted with an Enter nothing checks`
	if err := t.SendKeys(ctx, name, "continue"); err != nil {
		return err
	}
	return enter(ctx, name)
}

// inClosure types and submits inside a function literal of the function.
func inClosure(ctx context.Context, t *tmuxccy.Tmux, name string) func() error {
	return func() error {
		if err := t.SendKeys(ctx, name, "/exit"); err != nil {
			return err
		}
		return t.SendEnter(ctx, name) // want `submitted with an Enter nothing checks`
	}
}

// newline submits inside the typing.
func newline(ctx context.Context, t *tmuxccy.Tmux, name string) error {
	return t.SendKeys(ctx, name, "/compact now\n") // want `a line ending typed with SendKeys`
}

// carriageReturn submits inside a built text.
func carriageReturn(ctx context.Context, t *tmuxccy.Tmux, name, text string) error {
	return t.SendKeys(ctx, name, text+"\r") // want `a line ending typed with SendKeys`
}

// submitted is the fix: Submit checks the box.
func submitted(ctx context.Context, t *tmuxccy.Tmux, name string) error {
	return t.Submit(ctx, name, "/compact now")
}

// reenter presses Enter again on a line a poll saw still in the box: a lone Enter, not judged.
func reenter(ctx context.Context, t *tmuxccy.Tmux, name string) error {
	return t.SendEnter(ctx, name)
}

// typedOnly types text and leaves it for a person: no Enter, not judged.
func typedOnly(ctx context.Context, t *tmuxccy.Tmux, name string) error {
	return t.SendKeys(ctx, name, "draft")
}
