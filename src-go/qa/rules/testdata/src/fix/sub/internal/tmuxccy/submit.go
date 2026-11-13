// Package tmuxccy stands for the tmux owner: its Submit is the one function that may type and
// press Enter, and its raw text commands are judged.
package tmuxccy

import "context"

// Tmux stands for the owner's runner.
type Tmux struct{}

// Run stands for the runner call.
func (t *Tmux) Run(args ...string) {}

// SendKeys types text.
func (t *Tmux) SendKeys(ctx context.Context, name, text string) error { return nil }

// SendEnter presses Enter.
func (t *Tmux) SendEnter(ctx context.Context, name string) error { return nil }

// CapturePane reads the pane.
func (t *Tmux) CapturePane(ctx context.Context, name string, escapes bool) (string, error) {
	return "", nil
}

// Submit is the verified submitter: it may type and press Enter.
func (t *Tmux) Submit(ctx context.Context, name, text string) error {
	if err := t.SendKeys(ctx, name, text); err != nil {
		return err
	}
	return t.SendEnter(ctx, name)
}

// typeAndEnter is a second submitter in the owner, which nothing checks.
func (t *Tmux) typeAndEnter(ctx context.Context, name, text string) error {
	if err := t.SendKeys(ctx, name, text); err != nil {
		return err
	}
	return t.SendEnter(ctx, name) // want `submitted with an Enter nothing checks`
}

// rawSubmit builds send-keys -l and Enter by hand.
func (t *Tmux) rawSubmit(name, text string) {
	t.Run("send-keys", "-t", name, "-l", text)
	t.Run("send-keys", "-t", name, "C-m") // want `a tmux text command and a submit key`
}

// rawOneCall types and submits in one argv.
func (t *Tmux) rawOneCall(name, text string) {
	t.Run("send-keys", "-t", name, "-l", text, "Enter") // want `a tmux text command and a submit key`
}

// pasted pastes a buffer and submits it.
func (t *Tmux) pasted(name string) {
	t.Run("paste-buffer", "-t", name)
	t.Run("send-keys", "-t", name, "KPEnter") // want `a tmux text command and a submit key`
}

// loneEnter answers a dialog: no text typed here, not judged.
func (t *Tmux) loneEnter(name string) {
	t.Run("send-keys", "-t", name, "Enter")
}

// loneEscape presses Escape: not a submit.
func (t *Tmux) loneEscape(name string) {
	t.Run("send-keys", "-t", name, "Escape")
}
