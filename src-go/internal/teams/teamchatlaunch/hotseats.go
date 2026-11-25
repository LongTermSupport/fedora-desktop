package teamchatlaunch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatchoice"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
)

// ErrNotJoined is a leave of a team the session's desired membership does not name.
var ErrNotJoined = errors.New("the session is not on the team")

// WriteDesired writes d as the session's desired.json, 0600 and atomically, its control
// directory made first: the one writer of the file, for the launch's generation 1 and every
// later one (C 3.7.5).
func WriteDesired(store paths.Store, d state.Desired) error {
	data, err := d.Encode()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(store.Control(d.Session), 0o700); err != nil {
		return fmt.Errorf("make the control directory: %w", err)
	}
	return fsx.WriteAtomic(store.DesiredFile(d.Session), "desired membership", data, 0o600)
}

// HotParts is what a running v2 session's seats are changed through: the launch's seat store
// and take, the store's session records (whose seats are held, for a seat not named), the
// store opened for a release, and the probe of a seat lock's keeper.
type HotParts struct {
	Launch   *Launch
	Sessions seat.Sessions
	Holder   func() (Holder, error)
	Keeper   func(paths.Store, paths.SessionName, paths.TeamID) (seat.Keeper, error)
}

// HotSeats is a running v2 (teamchat) session's seats, changed while it runs (C 3.7.5, register
// R41): a join takes the seat as a launch takes it, then writes the next generation of the
// session's desired.json, which its keeper follows (RC1); a leave writes the next generation
// without the seat and with a released farewell, then releases the seat once the keeper has let
// it go. ccy-teams session add and remove are its callers.
type HotSeats struct {
	parts   HotParts
	session paths.SessionName
	desired state.Desired
}

// OpenHotSeats reads the session's desired.json; found is false when it has none, which is a
// session that was not launched on teamchat.
func OpenHotSeats(parts HotParts, session paths.SessionName) (*HotSeats, bool, error) {
	if err := parts.Launch.checkKind(); err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(parts.Launch.Store.DesiredFile(session))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read the desired membership of session %s: %w", session, err)
	}
	d, err := state.DecodeDesired(data)
	if err != nil {
		return nil, false, fmt.Errorf("the desired membership of session %s: %w", session, err)
	}
	return &HotSeats{parts: parts, session: session, desired: d}, true, nil
}

// HotSeats opens the seats of the running session in checkout, for ccy-teams session add and
// remove; found is false for a session with no v2 desired membership (a name v2 cannot give a
// session included).
func (v *Verbs) HotSeats(checkout, session string) (*HotSeats, bool, error) {
	name, ok := v2Session(session)
	if !ok {
		return nil, false, nil
	}
	store, key, home, err := system.Here(v.Getenv, func() (string, error) { return checkout, nil })
	if err != nil {
		return nil, false, err
	}
	cfg := v.config(home, key)
	parts := HotParts{
		// A podman seat: a cc session on teamchat is not changed while it runs (hot's v2CCNotBuilt).
		Launch:   &Launch{Store: store, Checkout: key, Kind: seatchoice.KindPodman, Taker: v.Taker(cfg), Status: v.Status, NowMS: v.NowMS},
		Sessions: cfg.Sessions,
		Holder:   func() (Holder, error) { return v.Holder(cfg) },
		Keeper:   seat.KeeperOf,
	}
	return OpenHotSeats(parts, name)
}

// v2Session is the session's name, and false for one v2 never gives a session.
func v2Session(session string) (paths.SessionName, bool) {
	name, err := paths.ParseSessionName(session)
	return name, err == nil
}

// Desired is the generation last read or written.
func (h *HotSeats) Desired() state.Desired { return h.desired }

// Choose is the seat a join of team takes: given, which must be a pool seat of the checkout, or
// else the lowest free pool seat, as the launch's chooser offers it. It is the seat part and the
// handle; no pool seat to take is ErrSeat, naming how to add one.
func (h *HotSeats) Choose(team, given string) (string, string, error) {
	id, err := paths.ParseTeamID(team)
	if err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrSeat, err)
	}
	l := h.parts.Launch
	if given != "" {
		pool, err := system.PoolSeats(l.Store, l.Checkout, id)
		if err != nil {
			return "", "", err
		}
		name, err := ChooseSeat(pool, team, given, l.Kind)
		if err != nil {
			return "", "", fmt.Errorf("team %s: %w", team, err)
		}
		return given, name.String(), nil
	}
	pool, err := system.PoolState(l.Store, l.Checkout, id, h.parts.Sessions, l.Kind)
	if err != nil {
		return "", "", err
	}
	free, ok := seatchoice.Default(pool)
	if !ok {
		return "", "", fmt.Errorf("%w: team %s has no free %s seat in this checkout; add one with %s", ErrSeat, team, l.Kind, seatchoice.AddCommand(team, l.Kind))
	}
	return free.Name, free.Handle, nil
}

// Join takes seat seatPart of team for the session, the way its launch took its seats (the pool
// seat, its roster state, under the allocation lock), then writes the next generation naming it.
// Nothing is written when the take fails; a generation that cannot be written releases the seat
// taken, so no seat stays held for a session that does not name it.
func (h *HotSeats) Join(ctx context.Context, team, seatPart string) error {
	l := h.parts.Launch
	chosen, takes, err := l.choose(seatPart + "@" + team)
	if err != nil {
		return err
	}
	takes[0].Session = h.session
	if err = l.Taker.TakeAll(ctx, takes); err != nil {
		return err
	}
	c := chosen[0]
	seats := append(slices.Clone(h.desired.Seats), state.DesiredSeat{Team: c.Team, Seat: c.Seat, Duty: c.Duty})
	if err = h.write(seats, h.without(c.Team, c.Seat)); err != nil {
		released, relErr := h.Release(ctx, team)
		if relErr != nil {
			return errors.Join(err, fmt.Errorf("the seat taken is still held: %w", relErr))
		}
		if !released {
			return errors.Join(err, errors.New("the seat taken was not found in the seat store to release"))
		}
		return fmt.Errorf("%w; the seat taken is released", err)
	}
	return nil
}

// Leave writes the next generation without the session's seat of team and with a released
// farewell for it (the member posts its goodbye, B 2.10), and is the seat left.
func (h *HotSeats) Leave(team string) (state.DesiredSeat, error) {
	i := slices.IndexFunc(h.desired.Seats, func(s state.DesiredSeat) bool { return s.Team.String() == team })
	if i < 0 {
		return state.DesiredSeat{}, fmt.Errorf("%w: session %s is not on %s", ErrNotJoined, h.session, team)
	}
	left := h.desired.Seats[i]
	farewell := append(h.without(left.Team, left.Seat), state.Farewell{Team: left.Team, Seat: left.Seat, Reason: state.FarewellReleased})
	return left, h.write(slices.Delete(slices.Clone(h.desired.Seats), i, i+1), farewell)
}

// Put writes the next generation with seats, the farewells of seats it names dropped: a failed
// join put back.
func (h *HotSeats) Put(seats []state.DesiredSeat) error {
	farewell := slices.DeleteFunc(slices.Clone(h.desired.Farewell), func(f state.Farewell) bool {
		return slices.ContainsFunc(seats, func(s state.DesiredSeat) bool { return s.Team == f.Team && s.Seat == f.Seat })
	})
	return h.write(slices.Clone(seats), farewell)
}

// without is the current farewells less the seat's.
func (h *HotSeats) without(team paths.TeamID, seatPart string) []state.Farewell {
	return slices.DeleteFunc(slices.Clone(h.desired.Farewell), func(f state.Farewell) bool { return f.Team == team && f.Seat == seatPart })
}

func (h *HotSeats) write(seats []state.DesiredSeat, farewell []state.Farewell) error {
	next := h.desired
	next.Generation++
	next.WrittenMS = h.parts.Launch.NowMS()
	next.Seats, next.Farewell = seats, farewell
	if err := WriteDesired(h.parts.Launch.Store, next); err != nil {
		return err
	}
	h.desired = next
	return nil
}

// Applied is whether the keeper holds seat seatPart of team at the generation last written: its
// seat lock held (seat.KeeperOf) and its report.json naming the seat with that generation
// applied. Until then why says what is missing.
func (h *HotSeats) Applied(team, seatPart string) (bool, string, error) {
	id, err := paths.ParseTeamID(team)
	if err != nil {
		return false, "", err
	}
	store := h.parts.Launch.Store
	keeper, err := h.parts.Keeper(store, h.session, id)
	if err != nil {
		return false, "", fmt.Errorf("the seat lock of %s: %w", team, err)
	}
	data, err := os.ReadFile(store.SessionSeat(h.session, id).Report())
	if errors.Is(err, os.ErrNotExist) {
		return false, "the session's keeper has written no report of the seat", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("the report of %s: %w", team, err)
	}
	rep, err := state.DecodeReport(data)
	if err != nil {
		return false, "", fmt.Errorf("the report of %s: %w", team, err)
	}
	gen := h.desired.Generation
	switch {
	case rep.Desired.Generation < gen:
		return false, fmt.Sprintf("the session's keeper has reported generation %d, not %d yet", rep.Desired.Generation, gen), nil
	case rep.Desired.State != state.DesiredApplied:
		code := ""
		if rep.Desired.Code != nil {
			code = *rep.Desired.Code
		}
		return false, fmt.Sprintf("the session's keeper refused the desired membership (%s)", code), nil
	case rep.Seat.Seat != seatPart:
		return false, fmt.Sprintf("the session's report names seat %s, not %s", rep.Seat.Seat, seatPart), nil
	case keeper != seat.KeeperRunning:
		return false, "the seat lock is not held", nil
	}
	return true, "", nil
}

// Release releases the session's seat of team as a leave (no disconnected notice: the session
// goes on), waiting, bounded, for the keeper to let its seat lock go (C 3.7.3). released is false
// when the seat store holds no seat of team for the session, so nothing was released.
func (h *HotSeats) Release(ctx context.Context, team string) (released bool, err error) {
	holder, err := h.parts.Holder()
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, holder.Close()) }()
	holdings, err := holder.Holdings()
	if err != nil {
		return false, fmt.Errorf("the seats of session %s: %w", h.session, err)
	}
	for _, held := range seat.HeldBy(holdings, h.session) {
		if held.Seat.Team.String() == team {
			return true, holder.Release(ctx, seat.Release{Seat: held.Seat, Session: h.session, Leaving: true})
		}
	}
	return false, nil
}
