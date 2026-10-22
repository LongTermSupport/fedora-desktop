package lxc

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestfacts"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// ProbeBound is each L3 probe's bound by default: its lxc-info calls, its pin and its opens, in
// all (LP1).
const ProbeBound = 5 * time.Second

// Guests is the L3 probe as the health tick's health.Guests (LP1): a handle to the seat of the
// team's record entry that is not left, and that seat's lock in its guest, read by TB1's open
// through the pinned root. A guest that cannot be reached is Stopped and never an error
// (n3b-D1): a handle the record does not name, a container missing, not running or stopping
// during the probe. A handle only left entries hold, a guest user or seat lock not there yet,
// and a lock TB1 refuses (a link, a FIFO) are free with no record. An error is the host's own
// fault, or a probe past ProbeBound.
type Guests struct {
	Root string // the host root ("" on a host) the records are read under
	LXC  interface {
		Containers(ctx context.Context) ([]string, error)
		State(ctx context.Context, c string) (lxccmd.State, error)
	}
	Pin func(ctx context.Context, container string) (Root, error)
	// Bound is each probe's bound; 0 is ProbeBound.
	Bound time.Duration
}

var (
	probeStopped = health.GuestProbe{Stopped: true}
	probeFree    = health.GuestProbe{}
)

// Probe is handle's seat in team, within the bound. A read that hangs past it is left to finish
// on its own, and its answer is dropped.
func (g Guests) Probe(ctx context.Context, team paths.TeamID, handle string) (health.GuestProbe, error) {
	ctx, cancel := context.WithTimeout(ctx, g.bound())
	defer cancel()
	type result struct {
		probe health.GuestProbe
		err   error
	}
	done := make(chan result, 1)
	go func() {
		probe, err := g.probe(ctx, team, handle)
		done <- result{probe, err}
	}()
	select {
	case r := <-done:
		if ctx.Err() == nil {
			return r.probe, r.err
		}
	case <-ctx.Done():
	}
	return health.GuestProbe{}, fmt.Errorf("lxc: the probe of %s in %s: %w", handle, team, context.Cause(ctx))
}

func (g Guests) probe(ctx context.Context, team paths.TeamID, handle string) (_ health.GuestProbe, err error) {
	rec, _, err := lxcstate.ReadRecord(g.Root, team)
	if err != nil {
		return health.GuestProbe{}, fmt.Errorf("lxc: the probe of %s: %w", handle, err)
	}
	e, ok := rec.ByHandle(handle)
	if !ok {
		if slices.ContainsFunc(rec.Members, func(e lxcstate.Entry) bool { return handle != "" && e.Handle == handle }) {
			return probeFree, nil
		}
		return probeStopped, nil
	}
	c := e.Container.String()
	names, err := g.LXC.Containers(ctx)
	if err != nil {
		return health.GuestProbe{}, fmt.Errorf("lxc: the probe of %s: %w", handle, err)
	}
	if !slices.Contains(names, c) || !g.running(ctx, c) {
		return probeStopped, nil
	}
	root, err := g.Pin(ctx, c)
	if gone(err) {
		return probeStopped, nil
	}
	if err != nil {
		return health.GuestProbe{}, fmt.Errorf("lxc: the probe of %s: %w", handle, err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	probe, err := seatLockOf(root, rec.SeatOf(e), e.User)
	if err != nil && (gone(err) || !g.running(ctx, c)) {
		return probeStopped, nil
	}
	if err != nil {
		return health.GuestProbe{}, fmt.Errorf("lxc: the probe of %s in %s: %w", handle, c, err)
	}
	return probe, nil
}

func (g Guests) bound() time.Duration {
	if g.Bound > 0 {
		return g.Bound
	}
	return ProbeBound
}

func (g Guests) running(ctx context.Context, c string) bool {
	st, err := g.LXC.State(ctx, c)
	return err == nil && st == lxccmd.Running
}

// gone is an error of a pinned root whose init has ended or changed: the container stopped.
func gone(err error) bool {
	return errors.Is(err, guestroot.ErrMoved) || errors.Is(err, guestroot.ErrGone)
}

// seatLockOf is the seat's lock under user's home in the guest, held or free with its content.
// The guest's own files are grammar-checked, and one refused, or not there yet, is free.
func seatLockOf(root Root, id lxcstate.SeatID, user paths.GuestUser) (health.GuestProbe, error) {
	var (
		acct   guestfacts.Account
		held   bool
		record []byte
	)
	passwd, err := readGuest(root, guestPasswd)
	if err == nil {
		acct, err = guestfacts.Passwd(passwd, user, nil)
	}
	if err == nil {
		k := id.Key()
		layout, layoutErr := paths.LXCLayout(acct.Home, paths.GuestRuntimeDir(k), k)
		if layoutErr != nil {
			err = fmt.Errorf("%w: %w", guestfacts.ErrRefused, layoutErr)
		} else {
			held, record, err = root.ProbeLock(layout.Seat(id.Team).Lock())
		}
	}
	if lockFree(err) {
		return probeFree, nil
	}
	if err != nil {
		return health.GuestProbe{}, err
	}
	return health.GuestProbe{Held: held, Record: record}, nil
}
