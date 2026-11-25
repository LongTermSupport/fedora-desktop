package chooser

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// TmuxReattach is the production Teamchat.Reattach over ccy's tmux: the session holding a seat
// is put on this terminal instead of a new session being started (issue 423). Outside tmux (a
// ccy launch chooses before its own tmux) the session is attached; inside one (cc chooses in its
// new tmux session) this client is switched to it, and the new session, given no seat, ends. A
// session not running in ccy's tmux, or open in another terminal, is an error naming the way on.
func TmuxReattach(t *tmuxccy.Tmux, getenv func(string) string) func(ctx context.Context, session string) error {
	return func(ctx context.Context, session string) error {
		running, err := t.List(ctx)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(running, func(s tmuxccy.Session) bool { return s.Name == session }) {
			return fmt.Errorf("it is not running in ccy's tmux, and its record keeps the seat: bring it back with ccy-sessions restore %s", session)
		}
		if getenv("TMUX") != "" {
			if _, err := t.Run(ctx, "switch-client", "-t", "="+session); err != nil {
				return fmt.Errorf("switch this terminal to %s: %w", session, err)
			}
			return nil
		}
		if _, err := t.Attach(ctx, session); err != nil {
			if errors.Is(err, tmuxccy.ErrOpenElsewhere) {
				return fmt.Errorf("%w; ccy-sessions takes it over (choose %s, then Take it over here)", err, session)
			}
			return err
		}
		return nil
	}
}
