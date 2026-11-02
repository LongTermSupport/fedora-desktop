package ends

import (
	"context"

	"fix/internal/teamchat/notice"
)

// The clean forms: every read and post inside the team's turn, directly or through a helper.

func (e *ends) BackInTurn(ctx context.Context, team string, n notice.Client) error {
	return e.turn(ctx, team, func() error {
		latest, err := notice.LatestOwn(ctx, n, "main")
		if err != nil || latest != "disconnected" {
			return err
		}
		return e.post(ctx, n, "back")
	})
}

func (e *ends) EndInTurn(ctx context.Context, team string, n notice.Client) error {
	return e.turn(ctx, team, func() error { return e.outer(ctx, n) })
}

// RoomState reads the roster, not a lifecycle notice, so it needs no turn.
func roster(ctx context.Context, n notice.Client) ([]string, error) {
	return n.RoomState(ctx, "main")
}
