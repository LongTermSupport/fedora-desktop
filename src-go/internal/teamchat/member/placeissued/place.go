package placeissued

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

// Deps is what Place needs of the machine: where the seat store and the allocation lock are, which
// checkout's pool the seat joins, and the three seams a test replaces.
type Deps struct {
	// StateHome is $XDG_STATE_HOME, under which the seat store lives.
	StateHome string
	// RuntimeDir is $XDG_RUNTIME_DIR, under which the allocation lock lives.
	RuntimeDir string
	// Checkout is the checkout whose pool the seat is placed in.
	Checkout paths.CheckoutKey
	// Clock is the store's time and the waits of its bounded retries.
	Clock seat.Clock
	// Credential is the pool's credential kind; the store refuses a document of another kind.
	Credential seat.Credential
	// Reach asks whether a new seat's homeserver answers from this machine.
	Reach seat.Reacher
}

// OS is the Deps of the real machine: the real clock, the pool password as the pool's credential
// (the only production credential; DESIGN-v2 7.1 (d1) and (d2) are the owner's to choose), and the
// versions call as the homeserver's reach.
func OS(stateHome, runtimeDir string, checkout paths.CheckoutKey) Deps {
	return Deps{
		StateHome: stateHome, RuntimeDir: runtimeDir, Checkout: checkout,
		Clock:      wallClock{clock.System()},
		Credential: seat.Password{Clock: matrix.SystemClock()},
		Reach:      seat.VersionsReach{Clock: matrix.SystemClock()},
	}
}

// wallClock is the seat store's clock over the real one: its records' time is the wall clock's,
// and its waits are the real clock's.
type wallClock struct{ clock.Clock }

func (c wallClock) Now() time.Time { return time.UnixMilli(c.WallMS()) }

// Placed is the seat Place put in the pool.
type Placed struct {
	Team paths.TeamID
	Seat paths.SeatName
}

// errNotUsed is what the store's two seams that only a take or a release uses answer if they are
// ever asked: place-issued neither takes nor releases a seat.
var errNotUsed = errors.New("placeissued: place-issued never takes or releases a seat")

// notUsed stands in for the store's session records and its notice poster.
type notUsed struct{}

func (notUsed) Restorable(paths.SessionName) (bool, error) { return false, errNotUsed }

func (notUsed) Disconnected(context.Context, seat.Notice) (seat.Outcome, error) {
	return seat.Outcome{}, errNotUsed
}

// Place places the issued seat on r in the pool of d.Checkout: Read judges the document first, and
// a refused document touches nothing. Then the store is opened and its allocation lock taken; a
// crashed placement's leftovers go to quarantine; and the seat is claimed, filled and swapped in
// by the store's own placement, which refuses a team on another homeserver, a credential of
// another kind, and a new seat whose homeserver does not answer. A seat that already has a record
// is replaced and the old record quarantined with its credential unlinked. Every refusal is a
// *FailureError; an error reading stdin is returned as it is.
func Place(ctx context.Context, d Deps, r io.Reader) (Placed, error) {
	is, err := Read(r)
	if err != nil {
		return Placed{}, err
	}
	name, err := paths.ParseSeatName(is.Seat.Handle().String())
	if err != nil {
		return Placed{}, refused(seat.ErrInvalid, msgInvalid, err.Error())
	}
	store, err := seat.Open(seat.Config{
		StateHome: d.StateHome, RuntimeDir: d.RuntimeDir, Checkout: d.Checkout, Clock: d.Clock,
		Credential: d.Credential, Sessions: notUsed{}, Announcer: notUsed{}, Reach: d.Reach,
	})
	if err != nil {
		return Placed{}, failure(err, is.Seat.BaseURL)
	}
	placed := Placed{Team: is.Seat.Team, Seat: name}
	err = errors.Join(placeIn(ctx, store, is, seat.Seat{Team: placed.Team, Name: name}), store.Close())
	if err != nil {
		return Placed{}, failure(err, is.Seat.BaseURL)
	}
	return placed, nil
}

// placeIn is the placement under the allocation lock, which it releases however it ends. A claim
// the store refused before it wrote anything is abandoned; one it failed after writing is left
// for the leftovers sweep, which this run does at once, and the next run does at its start.
func placeIn(ctx context.Context, store *seat.Store, is seat.Issued, s seat.Seat) (err error) {
	alloc, err := store.Lock(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, alloc.Unlock()) }()
	if err = store.QuarantineLeftovers(alloc, s); err != nil {
		return err
	}
	claim, err := store.Claim(alloc, s)
	if err != nil {
		return err
	}
	if err = claim.Fill(ctx, is); err != nil {
		if abandonErr := claim.Abandon(); abandonErr != nil {
			err = errors.Join(err, abandonErr, store.QuarantineLeftovers(alloc, s))
		}
		return err
	}
	return nil
}

// failure is err, a refusal or fault of the seat store, as the FailureError that reports it: a
// document the store judged invalid or for another homeserver is 78 bundle; a homeserver that did
// not answer is 20, naming baseURL; a lock or a seat held is 75; anything else is a store that is
// not as the owner must have it, 78 store. The cause line is the error's own text, which holds no
// credential: the store's errors name paths, URLs and rules.
func failure(err error, baseURL string) error {
	if f, ok := errors.AsType[*FailureError](err); ok {
		return f
	}
	switch {
	case errors.Is(err, seat.ErrInvalid):
		return refused(err, msgInvalid, err.Error())
	case errors.Is(err, seat.ErrHomeserverClash):
		return refused(err, msgClash, err.Error())
	case errors.Is(err, seat.ErrUnreachable):
		return &FailureError{Object: exits.New(exits.CodeUnreachable, nil, msgUnreachable, exits.Name(baseURL)), Cause: err.Error(), err: err}
	case errors.Is(err, seat.ErrBusy):
		return &FailureError{Object: exits.New(exits.CodeSeatHeld, nil, msgBusy), Cause: err.Error(), err: err}
	case errors.Is(err, seat.ErrHeld):
		return &FailureError{Object: exits.New(exits.CodeSeatHeld, nil, msgHeld), Cause: err.Error(), err: err}
	}
	return &FailureError{Object: exits.New(exits.CodeStore, nil, msgStore), Cause: fmt.Sprint(err), err: err}
}
