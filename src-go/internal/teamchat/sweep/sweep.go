// Package sweep is the owner's seat sweep of the v2 agent-team bus (Plan 00209 SPEC-n2e.md
// section 6, C 3.7.5, DC21, INT7): every pool seat the checkout holds is released when its
// session is over or no longer wants it, and otherwise kept, a disconnected notice posted once
// for a run that ended. Every judgement of the seat lock is the store's (seat.Store.Announce):
// the sweep reads nothing a container can write, takes the allocation lock only where a release
// and an announce take it, and makes no root call.
package sweep

import (
	"context"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

// Store is the part of the seat store (seat.Store) the sweep uses.
type Store interface {
	Holdings() ([]seat.Holding, error)
	Release(ctx context.Context, r seat.Release) error
	Announce(ctx context.Context, s seat.Seat, session paths.SessionName) error
}

// Desired answers whether a session still desires a seat (its desired.json, C 3.7.5), a file
// the owner writes and the container only reads.
type Desired interface {
	Desires(session paths.SessionName, s seat.Seat) (bool, error)
}

// Action is what the sweep did with a holding.
type Action string

const (
	// Released is a holding whose session is over, or whose held/ is incomplete.
	Released Action = "released"
	// Left is a holding whose session goes on without the seat: released, no notice due.
	Left Action = "left"
	// Kept is a holding its session still wants: announced if its run ended, and kept held.
	Kept Action = "kept"
)

// Result is one holding swept: its action, and the error that stopped it.
type Result struct {
	Seat    seat.Seat
	Session paths.SessionName
	Action  Action
	Err     error
}

// Sweep passes over every holding of the store, each on its own (SW2): one's failure is in its
// Result and the others still run. A store that cannot list its holdings, or a cancelled
// context, is the sweep's error.
func Sweep(ctx context.Context, store Store, sessions seat.Sessions, desired Desired) ([]Result, error) {
	holdings, err := store.Holdings()
	if err != nil {
		return nil, fmt.Errorf("sweep: the holdings: %w", err)
	}
	out := make([]Result, 0, len(holdings))
	for _, h := range holdings {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		action, err := one(ctx, store, sessions, desired, h)
		out = append(out, Result{Seat: h.Seat, Session: h.Record.Session, Action: action, Err: err})
	}
	return out, nil
}

// one sweeps a holding. An incomplete held/ names no session; its release quarantines it.
func one(ctx context.Context, store Store, sessions seat.Sessions, desired Desired, h seat.Holding) (Action, error) {
	if !h.Complete {
		return Released, store.Release(ctx, seat.Release{Seat: h.Seat})
	}
	session := h.Record.Session
	restorable, err := sessions.Restorable(session)
	if err != nil {
		return "", fmt.Errorf("sweep: the record of session %s: %w", session, err)
	}
	if !restorable {
		return Released, store.Release(ctx, seat.Release{Seat: h.Seat, Session: session})
	}
	wanted, err := desired.Desires(session, h.Seat)
	if err != nil {
		return "", fmt.Errorf("sweep: what session %s desires: %w", session, err)
	}
	if !wanted {
		return Left, store.Release(ctx, seat.Release{Seat: h.Seat, Session: session, Leaving: true})
	}
	return Kept, store.Announce(ctx, h.Seat, session)
}
