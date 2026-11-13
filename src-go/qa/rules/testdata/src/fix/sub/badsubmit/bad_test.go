package badsubmit

import (
	"context"

	"fix/sub/internal/tmuxccy"
)

// A test may type and press Enter by hand: test files are not judged.
func driveByHand(ctx context.Context, t *tmuxccy.Tmux) error {
	if err := t.SendKeys(ctx, "alpha", "x"); err != nil {
		return err
	}
	return t.SendEnter(ctx, "alpha")
}
