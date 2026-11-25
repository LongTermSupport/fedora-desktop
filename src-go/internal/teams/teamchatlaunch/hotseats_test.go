package teamchatlaunch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

type hotWorld struct {
	launchWorld
	seats   *HotSeats
	holder  *fakeHolder
	session paths.SessionName
	keeper  seat.Keeper
	alpha   paths.TeamID
	beta    paths.TeamID
}

// newHotWorld is a running v2 session s1 holding seat 1 of alpha at generation 1, with pool
// seats 1 and 2 of beta in the checkout.
func newHotWorld(t *testing.T) *hotWorld {
	t.Helper()
	w := &hotWorld{launchWorld: newLaunchWorld(t), holder: &fakeHolder{}, session: mustSession(t, "s1"), keeper: seat.KeeperRunning}
	w.alpha, w.beta = mustTeamID(t, "alpha"), mustTeamID(t, "beta")
	for _, s := range []string{"repo.1+site.podman", "repo.2+site.podman"} {
		if err := os.MkdirAll(filepath.Join(w.launch.Store.PoolTeam(w.launch.Checkout, w.beta), s), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	gen1 := state.Desired{
		Session: w.session, Generation: 1, WrittenMS: 1, Seats: []state.DesiredSeat{{Team: w.alpha, Seat: "1"}},
		Farewell: []state.Farewell{}, Inbound: state.InboundAccept,
	}
	if err := WriteDesired(w.launch.Store, gen1); err != nil {
		t.Fatal(err)
	}
	seats, found, err := OpenHotSeats(HotParts{
		Launch: w.launch, Sessions: restorable(false),
		Holder: func() (Holder, error) { return w.holder, nil },
		Keeper: func(paths.Store, paths.SessionName, paths.TeamID) (seat.Keeper, error) { return w.keeper, nil },
	}, w.session)
	if err != nil || !found {
		t.Fatalf("open: %v %v", found, err)
	}
	w.seats = seats
	return w
}

func mustTeamID(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (w *hotWorld) onDisk(t *testing.T) state.Desired {
	t.Helper()
	data, err := os.ReadFile(w.launch.Store.DesiredFile(w.session))
	if err != nil {
		t.Fatal(err)
	}
	d, err := state.DecodeDesired(data)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// writeReport writes the keeper's report of the seat, as the keeper writes it once the link
// names the seat's handle.
func (w *hotWorld) writeReport(t *testing.T, team paths.TeamID, seatPart, handle string, generation int64) {
	t.Helper()
	run, err := paths.ParseRunID("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	r := state.Report{
		Run: run, StartedMS: 1, Seat: state.SeatRef{Team: team, Seat: seatPart}, Handle: handle,
		Link: state.LinkRunning, Delivery: "socket", Store: state.StoreOK,
		Desired: state.ReportDesired{Generation: generation, State: state.DesiredApplied},
	}
	data, err := r.Encode()
	if err != nil {
		t.Fatal(err)
	}
	path := w.launch.Store.SessionSeat(w.session, team).Report()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// applied is Applied of seat 2 of beta, failing the test on an error.
func (w *hotWorld) applied(t *testing.T) (bool, string) {
	t.Helper()
	ok, why, err := w.seats.Applied("beta", "2")
	if err != nil {
		t.Fatal(err)
	}
	return ok, why
}

// A session with no desired.json is not a v2 session: found is false, nothing is an error.
func TestOpenHotSeatsFindsNoV2SessionWithoutDesired(t *testing.T) {
	w := newLaunchWorld(t)
	_, found, err := OpenHotSeats(HotParts{Launch: w.launch}, mustSession(t, "other"))
	if err != nil || found {
		t.Fatalf("got %v %v", found, err)
	}
}

// The launcher's verbs open a running session's seats in the checkout's seat store: the session
// its launch wrote desired.json for is found, its release goes to the same store, and a session
// with none, or a name v2 never gives a session, is not found.
func TestVerbsHotSeatsOpensTheSessionInTheCheckoutsStore(t *testing.T) {
	w := newVerbs(t)
	session := mustSession(t, "s1")
	gen1 := state.Desired{Session: session, Generation: 1, Seats: []state.DesiredSeat{}, Farewell: []state.Farewell{}, Inbound: state.InboundAccept}
	if err := WriteDesired(w.store, gen1); err != nil {
		t.Fatal(err)
	}
	seats, found, err := w.verbs.HotSeats(w.checkout, "s1")
	if err != nil || !found || seats.Desired().Generation != 1 {
		t.Fatalf("got %v %v", found, err)
	}
	if err := seats.Release(context.Background(), "alpha"); err != nil || !w.holder.closed {
		t.Fatalf("release: %v, closed %v", err, w.holder.closed)
	}
	for _, name := range []string{"s2", "not a session"} {
		if _, found, err := w.verbs.HotSeats(w.checkout, name); err != nil || found {
			t.Fatalf("%q: got %v %v", name, found, err)
		}
	}
}

// R41: a join takes the seat the way a launch takes it (the pool seat, for this session), then
// writes the next generation of desired.json naming it, the session's other seats kept.
func TestJoinTakesTheSeatThenWritesTheNextGeneration(t *testing.T) {
	w := newHotWorld(t)
	seatPart, handle, err := w.seats.Choose("beta", "2")
	if err != nil || seatPart != "2" || handle != "repo.2+site.podman" {
		t.Fatalf("choose: %q %q %v", seatPart, handle, err)
	}
	if err := w.seats.Join(context.Background(), "beta", "2"); err != nil {
		t.Fatal(err)
	}
	took := w.taker.took
	if len(took) != 1 || took[0].Seat.Team != w.beta || took[0].Seat.Name.String() != "repo.2+site.podman" || took[0].Session != w.session {
		t.Fatalf("took %+v", took)
	}
	d := w.onDisk(t)
	if d.Generation != 2 || !d.Holds(w.alpha, "1") || !d.Holds(w.beta, "2") || d.WrittenMS != 99 || d.Inbound != state.InboundAccept {
		t.Fatalf("desired %+v", d)
	}
	if w.seats.Desired().Generation != 2 {
		t.Fatalf("the session's view is generation %d", w.seats.Desired().Generation)
	}
}

// With no seat named, the lowest free pool seat of the team is chosen, as the launch's chooser
// offers it.
func TestChooseWithNoSeatIsTheLowestFreePoolSeat(t *testing.T) {
	w := newHotWorld(t)
	seatPart, handle, err := w.seats.Choose("beta", "")
	if err != nil || seatPart != "1" || handle != "repo.1+site.podman" {
		t.Fatalf("got %q %q %v", seatPart, handle, err)
	}
	if _, _, err := w.seats.Choose("gamma", ""); !errors.Is(err, ErrSeat) {
		t.Fatalf("a team with no pool seat: %v", err)
	}
	if _, _, err := w.seats.Choose("beta", "9"); !errors.Is(err, ErrSeat) {
		t.Fatalf("a seat not in the pool: %v", err)
	}
}

// A take that fails writes nothing: the session keeps generation 1.
func TestJoinWritesNothingWhenTheTakeFails(t *testing.T) {
	w := newHotWorld(t)
	w.taker.err = seat.ErrHeld
	if err := w.seats.Join(context.Background(), "beta", "1"); !errors.Is(err, seat.ErrHeld) {
		t.Fatalf("got %v", err)
	}
	if d := w.onDisk(t); d.Generation != 1 || d.Holds(w.beta, "1") {
		t.Fatalf("desired %+v", d)
	}
}

// The keeper reports a seat held when its seat lock is held and its report names this
// generation, applied; until then the reason is said.
func TestAppliedIsTheLockHeldAndTheReportAtThisGeneration(t *testing.T) {
	w := newHotWorld(t)
	if err := w.seats.Join(context.Background(), "beta", "2"); err != nil {
		t.Fatal(err)
	}
	if ok, why := w.applied(t); ok || why == "" {
		t.Fatalf("no report yet: %v %q", ok, why)
	}
	w.writeReport(t, w.beta, "2", "repo.2+site.podman", 1)
	if ok, why := w.applied(t); ok {
		t.Fatalf("an older generation passed: %q", why)
	}
	w.writeReport(t, w.beta, "2", "repo.2+site.podman", 2)
	w.keeper = seat.KeeperNoLock
	if ok, why := w.applied(t); ok {
		t.Fatalf("no lock held passed: %q", why)
	}
	w.keeper = seat.KeeperRunning
	if ok, why := w.applied(t); !ok {
		t.Fatalf("not applied: %q", why)
	}
}

// A leave writes the next generation without the seat and with a released farewell for it, then
// releases the seat as a leave (no disconnected notice), the keeper having let it go.
func TestLeaveWritesTheFarewellThenReleasesTheSeat(t *testing.T) {
	w := newHotWorld(t)
	name := mustSeat(t, "repo.1+site.podman")
	w.holder.holdings = []seat.Holding{{
		Seat: seat.Seat{Team: w.alpha, Name: name}, Complete: true, Record: seat.HeldRecord{Session: w.session},
	}}
	left, err := w.seats.Leave("alpha")
	if err != nil || left.Seat != "1" {
		t.Fatalf("leave: %+v %v", left, err)
	}
	d := w.onDisk(t)
	if d.Generation != 2 || d.Holds(w.alpha, "1") || len(d.Farewell) != 1 ||
		d.Farewell[0] != (state.Farewell{Team: w.alpha, Seat: "1", Reason: state.FarewellReleased}) {
		t.Fatalf("desired %+v", d)
	}
	if err := w.seats.Release(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	if len(w.holder.released) != 1 || w.holder.released[0] != (seat.Release{Seat: seat.Seat{Team: w.alpha, Name: name}, Session: w.session, Leaving: true}) || !w.holder.closed {
		t.Fatalf("released %+v closed %v", w.holder.released, w.holder.closed)
	}
	if _, err := w.seats.Leave("beta"); !errors.Is(err, ErrNotJoined) {
		t.Fatalf("a team the session is not on: %v", err)
	}
}

// Put writes the next generation with the seats given: a failed join put back.
func TestPutWritesTheSeatsGivenAtTheNextGeneration(t *testing.T) {
	w := newHotWorld(t)
	before := w.seats.Desired().Seats
	if err := w.seats.Join(context.Background(), "beta", "2"); err != nil {
		t.Fatal(err)
	}
	if err := w.seats.Put(before); err != nil {
		t.Fatal(err)
	}
	if d := w.onDisk(t); d.Generation != 3 || d.Holds(w.beta, "2") || !d.Holds(w.alpha, "1") || len(d.Farewell) != 0 {
		t.Fatalf("desired %+v", d)
	}
}
