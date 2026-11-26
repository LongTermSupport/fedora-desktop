package loadgen

import (
	"context"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/duration"
)

// Run takes s's steps in order, each at its offset from the start on c, and stops at the first
// failure (LG8) or when ctx ends.
func Run(ctx context.Context, g *Generator, s Schedule, c Clock) error {
	start := c.WallMS()
	for {
		st, ok := s.Next()
		if !ok {
			return nil
		}
		if wait := start + st.AtMS - c.WallMS(); wait > 0 {
			d, err := duration.Times(int(wait), time.Millisecond)
			if err != nil {
				return err
			}
			if err := c.Sleep(ctx, d); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		if st.Pattern != "" {
			err = g.Unit(ctx, st.Pattern)
		} else {
			err = g.Kind(ctx, st.Kind)
		}
		if err != nil {
			return err
		}
	}
}
