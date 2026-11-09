// Package ends reproduces the race of Plan 00209 n3c: back decided from a read of main and
// posted outside the team's turn, so the run's own end can post between the read and the post.
package ends

import (
	"context"

	"fix/internal/teamchat/notice"
)

type ends struct{ turns map[string]chan struct{} }

// turn runs fn as team's one lifecycle step at a time.
func (e *ends) turn(ctx context.Context, team string, fn func() error) error {
	held := e.turns[team]
	select {
	case held <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-held }()
	return fn()
}

// Back is the originating instance: the read and the post both outside the turn.
func (e *ends) Back(ctx context.Context, team string, n notice.Client) error {
	latest, err := notice.LatestOwn(ctx, n, "main") // want `lifecycle notice read or posted outside the team's turn: notice.LatestOwn`
	if err != nil || latest != "disconnected" {
		return err
	}
	_, err = n.Send(ctx, "main", "tc-back", "back") // want `lifecycle notice read or posted outside the team's turn: Send`
	return err
}

// End posts through a helper; the helper is judged where it is called from.
func (e *ends) End(ctx context.Context, team string, n notice.Client) error {
	return e.post(ctx, n, "disconnected") // want `lifecycle notice read or posted outside the team's turn: post`
}

func (e *ends) post(ctx context.Context, n notice.Client, event string) error {
	_, err := n.Send(ctx, "main", "tc", event)
	return err
}

// Close calls the notice package's read-then-post outside the turn.
func Close(ctx context.Context, n notice.Client) error {
	return notice.Disconnected(ctx, n, "main") // want `lifecycle notice read or posted outside the team's turn: notice.Disconnected`
}
