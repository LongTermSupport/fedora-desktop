package teamchatlaunch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatmodel"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
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
	w.writeReportAs(t, team, seatPart, handle, state.ReportDesired{Generation: generation, State: state.DesiredApplied})
}

// writeReportAs writes the keeper's report of the seat with its verdict on a generation, the
// team's homeserver having answered the link.
func (w *hotWorld) writeReportAs(t *testing.T, team paths.TeamID, seatPart, handle string, verdict state.ReportDesired) {
	t.Helper()
	w.writeReportWith(t, team, seatPart, handle, verdict, func(r *state.Report) { r.LastSyncOKMS = new(int64(1)) })
}

// writeReportWith writes the keeper's report of the seat, change made to it first.
func (w *hotWorld) writeReportWith(t *testing.T, team paths.TeamID, seatPart, handle string, verdict state.ReportDesired, change func(*state.Report)) {
	t.Helper()
	run, err := paths.ParseRunID("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	r := state.Report{
		Run: run, StartedMS: 1, Seat: state.SeatRef{Team: team, Seat: seatPart}, Handle: handle,
		Link: state.LinkRunning, Delivery: "socket", Store: state.StoreOK,
		Desired: verdict,
	}
	change(&r)
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

// A running session's seats are not opened through a Launch without its launcher's kind: its
// joins would refuse every seat as another launcher's (register R55).
func TestOpenHotSeatsRefusesALaunchWithNoKind(t *testing.T) {
	w := newLaunchWorld(t)
	w.launch.Kind = ""
	if _, _, err := OpenHotSeats(HotParts{Launch: w.launch}, mustSession(t, "other")); !errors.Is(err, ErrKind) {
		t.Fatalf("got %v", err)
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
	if released, err := seats.Release(context.Background(), "alpha"); err != nil || released || !w.holder.closed {
		t.Fatalf("release: %v %v, closed %v", released, err, w.holder.closed)
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

// Register R39: a join adds the seat to a claude already running, so no launch gives it the
// seat's model; the join says so on stderr, before the take, and says nothing when the seat has
// no model. A note that cannot be written takes nothing.
func TestJoinSaysTheSeatsModelIsNotUsed(t *testing.T) {
	w := newHotWorld(t)
	var stderr strings.Builder
	w.seats.parts.Stderr = &stderr
	if err := w.seats.Join(context.Background(), "beta", "1"); err != nil || stderr.Len() != 0 {
		t.Fatalf("no model: %v, stderr %q", err, stderr.String())
	}

	w = newHotWorld(t)
	w.seats.parts.Stderr = &stderr
	var s seatmodel.Settings
	s.SetSeat(w.beta, mustSeatName(t, "repo.2+site.podman"), "sonnet")
	if err := seatmodel.Write(w.launch.Store, s); err != nil {
		t.Fatal(err)
	}
	if err := w.seats.Join(context.Background(), "beta", "2"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"sonnet", "repo.2+site.podman", "not used for this join", "relaunch"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr %q lacks %q", stderr.String(), want)
		}
	}

	w = newHotWorld(t)
	w.seats.parts.Stderr = brokenWriter{}
	s = seatmodel.Settings{}
	s.SetDefault(w.beta, "opus")
	if err := seatmodel.Write(w.launch.Store, s); err != nil {
		t.Fatal(err)
	}
	if err := w.seats.Join(context.Background(), "beta", "1"); err == nil || w.taker.took != nil {
		t.Fatalf("got %v, took %+v", err, w.taker.took)
	}
}

func mustSeatName(t *testing.T, s string) paths.SeatName {
	t.Helper()
	n, err := paths.ParseSeatName(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
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

// removedBeta makes beta a team removed from this machine: no status record, and the host_vars
// declare it absent (register R59).
func (w *hotWorld) removedBeta() {
	w.launch.Life = system.Life{
		Status: w.launch.Status,
		Declared: func(team paths.TeamID) (system.Declared, error) {
			if team.String() == "beta" {
				return system.DeclaredAbsent, nil
			}
			return system.NotDeclared, nil
		},
		HostVars: "host_vars/localhost.yml",
	}.Check
}

// Register R59: a removed team's pool seats are never called free, and a named one is refused,
// naming the removal and the way on; nothing is taken or written.
func TestChooseNeverOffersARemovedTeamsSeat(t *testing.T) {
	w := newHotWorld(t)
	w.removedBeta()
	for _, given := range []string{"", "1"} {
		_, _, err := w.seats.Choose("beta", given)
		if !errors.Is(err, system.ErrTeamGone) || !strings.Contains(err.Error(), "was removed from this machine") ||
			!strings.Contains(err.Error(), "ccy-teams seats abandon beta") {
			t.Errorf("Choose(beta, %q): %v", given, err)
		}
	}
	if err := w.seats.Join(context.Background(), "beta", "1"); !errors.Is(err, system.ErrTeamGone) {
		t.Fatalf("a join of a removed team's seat: %v", err)
	}
	if len(w.taker.took) != 0 {
		t.Fatalf("a removed team's seat was taken: %+v", w.taker.took)
	}
	if d := w.onDisk(t); d.Generation != 1 {
		t.Fatalf("a refused join wrote a generation: %+v", d)
	}
	if code := codeOf(fmt.Errorf("x: %w", system.ErrTeamGone)); code != term.ExitConfig {
		t.Fatalf("a removed team's refusal is 78, got %d", code)
	}
}

// Register R59: a team this machine does not declare and has no record of is served on another
// machine (a placed member): its seat is still offered, and a Launch with no lifecycle refuses.
func TestChooseOffersATeamServedElsewhere(t *testing.T) {
	w := newHotWorld(t)
	w.launch.Life = system.Life{
		Status:   w.launch.Status,
		Declared: func(paths.TeamID) (system.Declared, error) { return system.NotDeclared, nil },
	}.Check
	if seatPart, _, err := w.seats.Choose("beta", ""); err != nil || seatPart != "1" {
		t.Fatalf("a team served elsewhere: %q %v", seatPart, err)
	}
	w.launch.Life = nil
	if _, _, err := w.seats.Choose("beta", ""); err == nil {
		t.Fatal("Choose with no lifecycle wired answered")
	}
	if err := w.seats.Join(context.Background(), "beta", "1"); err == nil {
		t.Fatal("Join with no lifecycle wired took a seat")
	}
}

// Register R55: a ccy session's join is offered its own kind's seats only: a host seat of the
// team is neither the default nor taken by name, and with no podman seat the way on is a podman
// seat's add.
func TestChooseOffersAPodmanSessionOnlyPodmanSeats(t *testing.T) {
	w := newHotWorld(t)
	gamma := mustTeamID(t, "gamma")
	if err := os.MkdirAll(filepath.Join(w.launch.Store.PoolTeam(w.launch.Checkout, gamma), "repo.1+site.host"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.seats.Choose("gamma", ""); !errors.Is(err, ErrSeat) || !strings.HasSuffix(err.Error(), "add one with ccy-teams seats add gamma") {
		t.Fatalf("a team with only a host seat: %v", err)
	}
	if _, _, err := w.seats.Choose("gamma", "1"); !errors.Is(err, ErrSeat) || !strings.Contains(err.Error(), "is a host seat") {
		t.Fatalf("a host seat by name: %v", err)
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

// R60: a seat held and its generation applied is not yet joined: the keeper's report must show
// the link had an answer from the team's homeserver. Until then the reason says it has not; a
// team the link has down is named as one the session cannot reach, with the link's cause.
func TestAppliedWaitsForAnAnswerFromTheTeamsHomeserver(t *testing.T) {
	w := newHotWorld(t)
	if err := w.seats.Join(context.Background(), "beta", "2"); err != nil {
		t.Fatal(err)
	}
	gen := state.ReportDesired{Generation: 2, State: state.DesiredApplied}
	const cause = "the homeserver did not answer (dial tcp: i/o timeout)"
	for _, c := range []struct {
		name   string
		change func(*state.Report)
		ok     bool
		why    string
	}{
		{"never answered", func(*state.Report) {}, false, "the session's link has had no answer from team beta's homeserver yet"},
		{
			"down, never answered", func(r *state.Report) { r.Down = new(cause) }, false,
			"the session cannot reach team beta's homeserver: its link has the team down (" + cause + ")",
		},
		{
			"down after answering", func(r *state.Report) { r.LastSyncOKMS, r.Down = new(int64(5)), new(cause) }, false,
			"the session cannot reach team beta's homeserver: its link has the team down (" + cause + ")",
		},
		{"answered", func(r *state.Report) { r.LastSyncOKMS = new(int64(5)) }, true, ""},
	} {
		w.writeReportWith(t, w.beta, "2", "repo.2+site.podman", gen, c.change)
		if ok, why := w.applied(t); ok != c.ok || why != c.why {
			t.Errorf("%s: applied %v %q, want %v %q", c.name, ok, why, c.ok, c.why)
		}
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
	if released, err := w.seats.Release(context.Background(), "alpha"); err != nil || !released {
		t.Fatalf("release: %v %v", released, err)
	}
	if len(w.holder.released) != 1 || w.holder.released[0] != (seat.Release{Seat: seat.Seat{Team: w.alpha, Name: name}, Session: w.session, Leaving: true}) || !w.holder.closed {
		t.Fatalf("released %+v closed %v", w.holder.released, w.holder.closed)
	}
	if _, err := w.seats.Leave("beta"); !errors.Is(err, ErrNotJoined) {
		t.Fatalf("a team the session is not on: %v", err)
	}
}

// A join whose take succeeded but whose generation cannot be written releases the seat it took,
// so a failed add leaves no seat held for a session that does not name it (batch 4 review 3).
func TestJoinReleasesTheTakeWhenTheGenerationCannotBeWritten(t *testing.T) {
	w := newHotWorld(t)
	name := mustSeat(t, "repo.2+site.podman")
	w.holder.holdings = []seat.Holding{{
		Seat: seat.Seat{Team: w.beta, Name: name}, Complete: true, Record: seat.HeldRecord{Session: w.session},
	}}
	desired := w.launch.Store.DesiredFile(w.session)
	if err := os.Remove(desired); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(desired, "blocks-the-rename"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := w.seats.Join(context.Background(), "beta", "2"); err == nil {
		t.Fatal("a generation that could not be written was not an error")
	}
	if len(w.taker.took) != 1 {
		t.Fatalf("took %+v", w.taker.took)
	}
	if len(w.holder.released) != 1 || w.holder.released[0] != (seat.Release{Seat: seat.Seat{Team: w.beta, Name: name}, Session: w.session, Leaving: true}) {
		t.Fatalf("released %+v", w.holder.released)
	}
	if w.seats.Desired().Generation != 1 {
		t.Fatalf("the session's view is generation %d", w.seats.Desired().Generation)
	}
}

// A release of a team the seat store holds no seat of for the session releases nothing and says
// so (released false), so its caller never reports a seat freed that it did not free (review 4).
func TestReleaseOfASeatTheSessionDoesNotHoldReleasesNothing(t *testing.T) {
	w := newHotWorld(t)
	w.holder.holdings = []seat.Holding{{
		Seat: seat.Seat{Team: w.alpha, Name: mustSeat(t, "repo.1+site.podman")}, Complete: true,
		Record: seat.HeldRecord{Session: mustSession(t, "other")},
	}}
	released, err := w.seats.Release(context.Background(), "alpha")
	if err != nil || released || len(w.holder.released) != 0 || !w.holder.closed {
		t.Fatalf("release: %v %v, released %+v closed %v", released, err, w.holder.released, w.holder.closed)
	}
}

// A report of an older generation is named as older, whatever its verdict: a refusal of an
// earlier generation is not this one's (review nit 2).
func TestAppliedNamesAnOlderReportsGenerationBeforeItsRefusal(t *testing.T) {
	w := newHotWorld(t)
	if err := w.seats.Join(context.Background(), "beta", "2"); err != nil {
		t.Fatal(err)
	}
	code := state.CodeSeat
	w.writeReportAs(t, w.beta, "2", "repo.2+site.podman", state.ReportDesired{Generation: 1, State: state.DesiredRefused, Code: &code})
	if ok, why := w.applied(t); ok || why != "the session's keeper has reported generation 1, not 2 yet" {
		t.Fatalf("got %v %q", ok, why)
	}
	w.writeReportAs(t, w.beta, "2", "repo.2+site.podman", state.ReportDesired{Generation: 2, State: state.DesiredRefused, Code: &code})
	if ok, why := w.applied(t); ok || why != "the session's keeper refused the desired membership (seat)" {
		t.Fatalf("got %v %q", ok, why)
	}
}

// The release a failed generation write makes says when it could not release the seat taken, or
// found none to release.
func TestJoinSaysWhenTheTakeCouldNotBeReleased(t *testing.T) {
	for _, tc := range []struct {
		name    string
		listErr error
		want    string
	}{
		{"store unreadable", errors.New("the store is unreadable"), "the seat taken is still held"},
		{"nothing held", nil, "the seat taken was not found in the seat store to release"},
	} {
		w := newHotWorld(t)
		w.holder.listErr = tc.listErr
		desired := w.launch.Store.DesiredFile(w.session)
		if err := os.Remove(desired); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(desired, "blocks-the-rename"), 0o700); err != nil {
			t.Fatal(err)
		}
		err := w.seats.Join(context.Background(), "beta", "2")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got %v", tc.name, err)
		}
	}
}

// A seat part that names no seat is refused before any take.
func TestJoinOfASeatNotInThePoolTakesNothing(t *testing.T) {
	w := newHotWorld(t)
	if err := w.seats.Join(context.Background(), "beta", "9"); err == nil || len(w.taker.took) != 0 {
		t.Fatalf("got %v, took %+v", err, w.taker.took)
	}
}

// A team name v2 cannot give is ErrSeat for Choose and an error for Applied.
func TestATeamNameV2CannotGiveIsRefused(t *testing.T) {
	w := newHotWorld(t)
	if _, _, err := w.seats.Choose("Not A Team", ""); !errors.Is(err, ErrSeat) {
		t.Fatalf("choose: %v", err)
	}
	if ok, why, err := w.seats.Applied("Not A Team", "1"); ok || why != "" || err == nil {
		t.Fatalf("applied: %v %q %v", ok, why, err)
	}
}

// Applied fails on what it cannot read (the keeper's lock, the report) and names a report of
// another seat as not this one.
func TestAppliedFailsOnWhatItCannotRead(t *testing.T) {
	w := newHotWorld(t)
	if err := w.seats.Join(context.Background(), "beta", "2"); err != nil {
		t.Fatal(err)
	}
	w.writeReport(t, w.beta, "1", "repo.1+site.podman", 2)
	if ok, why := w.applied(t); ok || why != "the session's report names seat 1, not 2" {
		t.Fatalf("another seat: %v %q", ok, why)
	}
	report := w.launch.Store.SessionSeat(w.session, w.beta).Report()
	if err := os.WriteFile(report, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.seats.Applied("beta", "2"); err == nil || !strings.Contains(err.Error(), "the report of beta") {
		t.Fatalf("garbage report: %v", err)
	}
	if err := os.Remove(report); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(report, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.seats.Applied("beta", "2"); err == nil || !strings.Contains(err.Error(), "the report of beta") {
		t.Fatalf("unreadable report: %v", err)
	}
	w.seats.parts.Keeper = func(paths.Store, paths.SessionName, paths.TeamID) (seat.Keeper, error) {
		return seat.KeeperRunning, errors.New("permission denied")
	}
	if _, _, err := w.seats.Applied("beta", "2"); err == nil || !strings.Contains(err.Error(), "the seat lock of beta: permission denied") {
		t.Fatalf("keeper: %v", err)
	}
}

// Release fails when the seat store cannot be opened or listed, releasing nothing.
func TestReleaseFailsWhenTheStoreCannotBeRead(t *testing.T) {
	w := newHotWorld(t)
	w.holder.listErr = errors.New("the store is unreadable")
	if released, err := w.seats.Release(context.Background(), "alpha"); err == nil || released || !w.holder.closed {
		t.Fatalf("list: %v %v closed %v", released, err, w.holder.closed)
	}
	w.seats.parts.Holder = func() (Holder, error) { return nil, errors.New("no seat store") }
	if released, err := w.seats.Release(context.Background(), "alpha"); err == nil || released {
		t.Fatalf("open: %v %v", released, err)
	}
}

// A desired.json that cannot be read or decoded is an error, not "no v2 session".
func TestOpenHotSeatsFailsOnAnUnreadableDesired(t *testing.T) {
	w := newLaunchWorld(t)
	session := mustSession(t, "s1")
	desired := w.launch.Store.DesiredFile(session)
	if err := os.MkdirAll(filepath.Dir(desired), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(desired, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := OpenHotSeats(HotParts{Launch: w.launch}, session); err == nil || found {
		t.Fatalf("garbage: %v %v", found, err)
	}
	if err := os.Remove(desired); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(desired, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, found, err := OpenHotSeats(HotParts{Launch: w.launch}, session); err == nil || found {
		t.Fatalf("unreadable: %v %v", found, err)
	}
}

// WriteDesired fails when the session's control directory cannot be made.
func TestWriteDesiredFailsWhenTheControlDirectoryCannotBeMade(t *testing.T) {
	w := newLaunchWorld(t)
	session := mustSession(t, "s1")
	control := w.launch.Store.Control(session)
	if err := os.MkdirAll(filepath.Dir(control), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(control, []byte("a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := state.Desired{Session: session, Generation: 1, Seats: []state.DesiredSeat{}, Farewell: []state.Farewell{}, Inbound: state.InboundAccept}
	if err := WriteDesired(w.launch.Store, d); err == nil || !strings.Contains(err.Error(), "make the control directory") {
		t.Fatalf("got %v", err)
	}
}

// The launcher's HotSeats fails when the seat store cannot be found (no HOME).
func TestVerbsHotSeatsFailsWithNoStateHome(t *testing.T) {
	w := newVerbs(t)
	w.verbs.Getenv = func(string) string { return "" }
	if _, found, err := w.verbs.HotSeats(w.checkout, "s1"); err == nil || found {
		t.Fatalf("got %v %v", found, err)
	}
}

// A join of a seat the session said goodbye to drops that farewell, and a put-back drops the
// farewells of the seats it names: neither leaves a farewell for a seat the session holds.
func TestJoinAndPutDropTheFarewellsOfTheSeatsTheyName(t *testing.T) {
	w := newHotWorld(t)
	ctx := context.Background()
	if err := w.seats.Join(ctx, "beta", "2"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.seats.Leave("beta"); err != nil {
		t.Fatal(err)
	}
	if err := w.seats.Join(ctx, "beta", "2"); err != nil {
		t.Fatal(err)
	}
	if d := w.onDisk(t); !d.Holds(w.beta, "2") || len(d.Farewell) != 0 {
		t.Fatalf("rejoin: %+v", d)
	}
	before := w.seats.Desired().Seats
	if _, err := w.seats.Leave("alpha"); err != nil {
		t.Fatal(err)
	}
	if err := w.seats.Put(before); err != nil {
		t.Fatal(err)
	}
	if d := w.onDisk(t); !d.Holds(w.alpha, "1") || len(d.Farewell) != 0 {
		t.Fatalf("put back: %+v", d)
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
