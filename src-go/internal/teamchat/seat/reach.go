package seat

import (
	"context"
	"errors"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
)

// ErrUnreachable is a homeserver this machine cannot reach: a creation that found no answer from
// the bundle's URL, or a take whose login found none (Plan 00244 X10). It names the URL and keeps
// the client's own error beneath it, so a person is told which URL and a launcher can tell this
// from a refusal.
var ErrUnreachable = errors.New("seat: the homeserver is unreachable")

// Reacher asks whether a bundle's homeserver answers from this machine, as a member would reach
// it: by the bundle's URL and the CA pinned to it. It is asked before a seat is created.
type Reacher interface {
	Reach(ctx context.Context, b Bundle) error
}

// VersionsReach is the Reacher that asks the homeserver for its versions, the one call it answers
// without a token, so the question needs no credential and changes nothing there. One try, bounded
// by the member client's own time limit and by ctx.
type VersionsReach struct {
	// Clock is how the member client waits.
	Clock matrix.Clock
}

// Reach asks b's homeserver for its versions.
func (r VersionsReach) Reach(ctx context.Context, b Bundle) error {
	cfg := b.MemberConfig(signedOut)
	cfg.Clock = r.Clock
	c, err := matrix.NewMember(cfg)
	if err != nil {
		return fmt.Errorf("seat: the member client for %s: %w", b.UserID, err)
	}
	defer c.Close()
	_, err = c.Versions(ctx)
	return err
}

// unreachable is err as ErrUnreachable naming b's URL.
func unreachable(b Bundle, err error) error {
	return fmt.Errorf("%w: %s: %w", ErrUnreachable, b.BaseURL, err)
}

// reachNew is the check before a seat is created: its homeserver must answer.
func (s *Store) reachNew(ctx context.Context, b Bundle) error {
	if err := s.reacher.Reach(ctx, b); err != nil {
		return unreachable(b, err)
	}
	return nil
}
