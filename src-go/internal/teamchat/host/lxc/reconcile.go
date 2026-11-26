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

// recordStartedVerb is every `lxc record-started` error's verb.
const recordStartedVerb = "lxc record-started"

// SeatRecorded is one live seat `lxc record-started` found: `ADDED\t<container>/<key>` when the
// record did not hold it, `KEPT\t...` when it did.
type SeatRecorded struct {
	Seat  lxcstate.StartedSeat
	Added bool
}

func (s SeatRecorded) String() string {
	if s.Added {
		return "ADDED\t" + s.Seat.String()
	}
	return "KEPT\t" + s.Seat.String()
}

// RecordStarted is `lxc record-started` (Plan 00266 C.2): every seat of a team's LXC record, not
// left, whose container runs and whose session is live is added to the record of started seats,
// so a seat that ran before `lxc start` kept that record is started again after a boot. The play
// runs it after the present phase, so it is idempotent: a seat already recorded is kept, and
// nothing is taken out or started. A seat whose liveness cannot be read fails it with the record
// left as it was.
func RecordStarted(ctx context.Context, d Deps) ([]SeatRecorded, error) {
	s, err := lxcstate.ReadStarted(d.Root)
	if err != nil {
		return nil, &host.Error{Verb: recordStartedVerb, Step: stepStartedFile, Err: err}
	}
	names, err := d.LXC.Containers(ctx)
	if err != nil {
		return nil, &host.Error{Verb: recordStartedVerb, Step: stepState, Err: err}
	}
	teams, err := recordTeams(d.Root)
	if err != nil {
		return nil, &host.Error{Verb: recordStartedVerb, Step: stepRecord, Err: err}
	}
	var out []SeatRecorded
	next := s
	for _, team := range teams {
		rec, _, err := lxcstate.ReadRecord(d.Root, team)
		if err != nil {
			return nil, &host.Error{Verb: recordStartedVerb, Step: stepRecord, Err: err}
		}
		for _, e := range rec.Members {
			if e.Status == lxcstate.StatusLeft {
				continue
			}
			seat := lxcstate.StartedSeat{Container: e.Container, Key: rec.SeatOf(e).Key()}
			live, err := seatLiveNow(ctx, d, names, seat)
			if err != nil {
				return nil, err
			}
			if live {
				out = append(out, SeatRecorded{Seat: seat, Added: !s.Has(seat)})
				next = next.Add(seat)
			}
		}
	}
	if len(next.Seats) != len(s.Seats) {
		if err := lxcstate.WriteStarted(d.Root, next); err != nil {
			return nil, &host.Error{Verb: recordStartedVerb, Step: stepStartedFile, Err: err}
		}
	}
	return out, nil
}

// seatLiveNow is whether the seat's container runs and its session is live; a container stopped,
// missing or stopping on the way is not live.
func seatLiveNow(ctx context.Context, d Deps, names []string, seat lxcstate.StartedSeat) (bool, error) {
	why, err := boxState(ctx, d.LXC, names, seat.Container)
	if err != nil {
		return false, &host.Error{Verb: recordStartedVerb, Step: stepState, Err: err}
	}
	if why != "" {
		return false, nil
	}
	live, err := sessionLive(ctx, d, seat)
	var off *OfflineError
	if errors.As(err, &off) {
		return false, nil
	}
	return live, err
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
