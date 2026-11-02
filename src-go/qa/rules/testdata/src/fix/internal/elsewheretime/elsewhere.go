package elsewheretime

import (
	"context"
	"time"
)

// Outside teamchat, whose tests are not bound to a fake clock, nothing is judged.

func Bounded(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}
