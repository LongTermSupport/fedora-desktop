package lxc

import (
	"context"
	"errors"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// stopVerb is every `lxc stop` error's verb, and reconcileVerb every `lxc reconcile` one's.
const (
	stopVerb        = "lxc stop"
	reconcileVerb   = "lxc reconcile"
	whyStoppedStop  = "offline: container stopped during the stop"
	stepStartedFile = "started"
)

// Stop is `lxc stop --member C/K` (Plan 00266 C.2): the seat is taken out of the record of
// started seats first, so a purposeful stop is kept even when its container is offline, then its
// session unit is stopped. It asks nothing of claude. The seat, the refusals and the offline
// causes are Restart's, in the stop's words.
func Stop(ctx context.Context, d Deps, container paths.ContainerName, key paths.SeatKey) error {
	if err := recordStarted(d, stopVerb, lxcstate.StartedSeat{Container: container, Key: key}, false); err != nil {
		return err
	}
	return runSeatJob(ctx, d, container, key, stopJob)
}

func stopUnit(ctx context.Context, s guestbus.Systemd, name string) error { return s.Stop(ctx, name) }

// recordStarted adds the seat to the record of started seats, or takes it out; a record already
// so is not written.
func recordStarted(d Deps, verb string, seat lxcstate.StartedSeat, started bool) error {
	s, err := lxcstate.ReadStarted(d.Root)
	if err == nil && s.Has(seat) != started {
		next := s.Remove(seat)
		if started {
			next = s.Add(seat)
		}
		err = lxcstate.WriteStarted(d.Root, next)
	}
	if err != nil {
		return &host.Error{Verb: verb, Step: stepStartedFile, Err: err}
	}
	return nil
}

// ReconcileKind is a reconcile line's first field.
type ReconcileKind string

// What `lxc reconcile` found of each seat.
const (
	ReconcileStarted ReconcileKind = "STARTED"
	ReconcileLive    ReconcileKind = "LIVE"
	ReconcileStopped ReconcileKind = "STOPPED"
	ReconcileRefused ReconcileKind = "REFUSED"
)

// ReconcileLine is one seat's line: `<kind>\t<container>/<key>\t<why>`.
type ReconcileLine struct {
	Kind ReconcileKind
	Seat lxcstate.StartedSeat
	Why  string
}

func (l ReconcileLine) String() string { return string(l.Kind) + "\t" + l.Seat.String() + "\t" + l.Why }

// Refused is how many of lines are refused starts: the verb fails when any is.
func Refused(lines []ReconcileLine) int {
	n := 0
	for _, l := range lines {
		if l.Kind == ReconcileRefused {
			n++
		}
	}
	return n
}

// Reconcile is `lxc reconcile [--container C]` (Plan 00266 C.2): each recorded seat (of
// container only, when it is given) whose container runs and whose session is not live (its seat
// lock not held) is started through Start. A live session is left alone; a seat whose container is
// stopped or missing is reported and its container never started. One line a seat, in the
// record's order. An error is the host's own fault: the record unreadable, or no container list.
func Reconcile(ctx context.Context, d Deps, container *paths.ContainerName) ([]ReconcileLine, error) {
	s, err := lxcstate.ReadStarted(d.Root)
	if err != nil {
		return nil, &host.Error{Verb: reconcileVerb, Step: stepStartedFile, Err: err}
	}
	names, err := d.LXC.Containers(ctx)
	if err != nil {
		return nil, &host.Error{Verb: reconcileVerb, Step: stepState, Err: err}
	}
	var lines []ReconcileLine
	for _, seat := range s.Seats {
		if container != nil && seat.Container != *container {
			continue
		}
		kind, why := reconcileSeat(ctx, d, names, seat)
		lines = append(lines, ReconcileLine{Kind: kind, Seat: seat, Why: why})
	}
	return lines, nil
}

// reconcileSeat is one seat's start, or why there is none.
func reconcileSeat(ctx context.Context, d Deps, names []string, seat lxcstate.StartedSeat) (ReconcileKind, string) {
	why, err := boxState(ctx, d.LXC, names, seat.Container)
	if err != nil {
		return ReconcileRefused, err.Error()
	}
	if why != "" {
		return ReconcileStopped, why
	}
	live, err := sessionLive(ctx, d, seat)
	if off, ok := errors.AsType[*OfflineError](err); ok {
		return ReconcileStopped, off.Why
	}
	if err != nil {
		return ReconcileRefused, err.Error()
	}
	if live {
		return ReconcileLive, ""
	}
	err = Start(ctx, d, seat.Container, seat.Key)
	if off, ok := errors.AsType[*OfflineError](err); ok {
		return ReconcileStopped, off.Why
	}
	if err != nil {
		return ReconcileRefused, err.Error()
	}
	return ReconcileStarted, ""
}

// sessionLive is whether the seat's session holds its seat lock, read through the pinned root as
// the L3 probe reads it.
func sessionLive(ctx context.Context, d Deps, seat lxcstate.StartedSeat) (live bool, err error) {
	id, user, err := restartSeat(d, reconcileVerb, seat.Container, seat.Key)
	if err != nil {
		return false, err
	}
	root, err := d.Pin(ctx, seat.Container.String())
	if gone(err) {
		return false, &OfflineError{Container: seat.Container, Why: whyStoppedStart}
	}
	if err != nil {
		return false, &host.Error{Verb: reconcileVerb, Step: "pin", Err: err}
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	probe, err := seatLockOf(root, id, user)
	if err != nil {
		return false, &host.Error{Verb: reconcileVerb, Step: "lock", Err: fmt.Errorf("%s: %w", seat, err)}
	}
	return probe.Held, nil
}
