package hot

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// fakeV2 is a running v2 (teamchat) session's seats: its desired generation, the pool seat each
// team offers, and every change asked of it.
type fakeV2 struct {
	t        *testing.T
	desired  state.Desired
	pool     map[string][2]string // team -> seat part, handle
	applied  bool
	joinErr  error
	putErr   error
	relErr   error
	notHeld  bool // the seat store holds no seat of the team for the session
	leaveErr error
	appErr   error
	onCall   func(call string) // runs as each change or check starts: the world moving meanwhile
	calls    []string
	released []string
}

func (f *fakeV2) called(call string) {
	if f.onCall != nil {
		f.onCall(call)
	}
}

func (f *fakeV2) Desired() state.Desired { return f.desired }

func (f *fakeV2) Choose(team, given string) (string, string, error) {
	p, ok := f.pool[team]
	if !ok || (given != "" && given != p[0]) {
		return "", "", errors.New("no such pool seat")
	}
	return p[0], p[1], nil
}

func (f *fakeV2) next(seats []state.DesiredSeat, farewell []state.Farewell) {
	f.desired.Generation++
	f.desired.Seats, f.desired.Farewell = seats, farewell
}

func (f *fakeV2) Join(_ context.Context, team, seatPart string) error {
	f.calls = append(f.calls, "join "+seatPart+"@"+team)
	f.called("join")
	if f.joinErr != nil {
		return f.joinErr
	}
	f.next(append(slices.Clone(f.desired.Seats), state.DesiredSeat{Team: mustTeam(f.t, team), Seat: seatPart}), f.desired.Farewell)
	return nil
}

func (f *fakeV2) Leave(team string) (state.DesiredSeat, error) {
	f.calls = append(f.calls, "leave "+team)
	if f.leaveErr != nil {
		return state.DesiredSeat{}, f.leaveErr
	}
	i := slices.IndexFunc(f.desired.Seats, func(s state.DesiredSeat) bool { return s.Team.String() == team })
	left := f.desired.Seats[i]
	f.next(slices.Delete(slices.Clone(f.desired.Seats), i, i+1),
		append(slices.Clone(f.desired.Farewell), state.Farewell{Team: left.Team, Seat: left.Seat, Reason: state.FarewellReleased}))
	return left, nil
}

func (f *fakeV2) Put(seats []state.DesiredSeat) error {
	f.calls = append(f.calls, "put "+seatList(seats))
	if f.putErr != nil {
		return f.putErr
	}
	f.next(slices.Clone(seats), f.desired.Farewell)
	return nil
}

func (f *fakeV2) Applied(team, seatPart string) (bool, string, error) {
	f.called("applied")
	if f.appErr != nil {
		return false, "", f.appErr
	}
	if f.applied && f.desired.Holds(mustTeam(f.t, team), seatPart) {
		return true, "", nil
	}
	return false, "the session's keeper has written no report of the seat", nil
}

func (f *fakeV2) Release(_ context.Context, team string) (bool, error) {
	if f.relErr != nil {
		return false, f.relErr
	}
	if f.notHeld {
		return false, nil
	}
	f.released = append(f.released, team)
	return true, nil
}

func seatList(seats []state.DesiredSeat) string {
	items := make([]string, 0, len(seats))
	for _, s := range seats {
		items = append(items, s.Seat+"@"+s.Team.String())
	}
	return strings.Join(items, ",")
}

func mustTeam(t *testing.T, team string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(team)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// newV2Rig is a running ccy session launched on teamchat with seat 1 of alpha: no v1
// membership under its name, its desired.json at generation 1, and a free pool seat 2 of beta.
func newV2Rig(t *testing.T) (*rig, *fakeV2) {
	t.Helper()
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "1"})
	if err := os.RemoveAll(r.dir()); err != nil {
		t.Fatal(err)
	}
	v2 := &fakeV2{
		t: t, applied: true, pool: map[string][2]string{"beta": {"2", "proj.2+local.podman"}},
		desired: state.Desired{
			Generation: 1, Seats: []state.DesiredSeat{{Team: mustTeam(t, "alpha"), Seat: "1"}},
			Farewell: []state.Farewell{}, Inbound: state.InboundAccept,
		},
	}
	r.x.Teamchat = func(checkout, name string) (TeamchatSeats, bool, error) {
		if checkout != r.Checkout() || name != session {
			t.Errorf("opened %s in %s", name, checkout)
		}
		return v2, true, nil
	}
	return r, v2
}

// R41: `session add` on a running v2 session takes the seat and writes the next generation of
// its desired.json, then waits for its keeper to hold it, as v1's add does for v1's membership.
func TestAddPutsARunningV2SessionOnTheTeam(t *testing.T) {
	r, v2 := newV2Rig(t)
	if code := r.run(t, "add", session, "beta", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(),
		"✓ session "+session+": running, can be hot-controlled",
		"✓ seat 2: free (proj.2+local.podman)",
		"✓ record: --teams 1@alpha,2@beta",
		"✓ membership: generation 2 written",
		"✓ session: seat 2 held, applied (0.0 s)",
		"Done. The session is told it joined beta as proj.2+local.podman. Nothing restarted.")
	if !slices.Equal(v2.calls, []string{"join 2@beta"}) {
		t.Fatalf("calls %v", v2.calls)
	}
	if got := r.recordArgs(t); !slices.Equal(got, []string{"--no-ssh", "--teams", "1@alpha,2@beta"}) {
		t.Fatalf("record %v", got)
	}
	if code := r.run(t, "add", session, "beta/2", "--yes"); code != term.ExitOK || len(v2.calls) != 1 {
		t.Fatalf("rerun exit %d, calls %v:\n%s", code, v2.calls, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "Nothing to do: "+session+" is already in beta as seat 2.")
}

// A seat the keeper does not hold within the wait is put back: the generation without it, the
// seat released, the record as it was.
func TestAV2AddTheKeeperDoesNotApplyIsPutBack(t *testing.T) {
	r, v2 := newV2Rig(t)
	v2.applied = false
	if code := r.run(t, "add", session, "beta", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(),
		"✗ session: seat 2 not held after 10 s: the session's keeper has written no report of the seat",
		"✓ record: restored (--teams 1@alpha)",
		"✓ membership: generation 3 written (restored)")
	if !slices.Equal(v2.calls, []string{"join 2@beta", "put 1@alpha"}) || !slices.Equal(v2.released, []string{"beta"}) {
		t.Fatalf("calls %v released %v", v2.calls, v2.released)
	}
	if got := r.recordArgs(t); !slices.Equal(got, []string{"--no-ssh", "--teams", "1@alpha"}) {
		t.Fatalf("record %v", got)
	}
}

// A take the seat store refuses writes no generation and puts the record back; a held seat is
// exit 75.
func TestAV2AddWhoseTakeIsRefusedChangesNothing(t *testing.T) {
	r, v2 := newV2Rig(t)
	v2.joinErr = seat.ErrHeld
	if code := r.run(t, "add", session, "beta", "--yes"); code != exitBusy {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if v2.desired.Generation != 1 || len(v2.released) != 0 {
		t.Fatalf("desired %+v released %v", v2.desired, v2.released)
	}
	if got := r.recordArgs(t); !slices.Equal(got, []string{"--no-ssh", "--teams", "1@alpha"}) {
		t.Fatalf("record %v", got)
	}
}

// `session remove` on a running v2 session writes the next generation without the seat and
// with its farewell, then releases the seat once the keeper lets it go.
func TestRemoveTakesARunningV2SessionOffTheTeam(t *testing.T) {
	r, v2 := newV2Rig(t)
	if code := r.run(t, "remove", session, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(),
		"✓ record: no --teams",
		"✓ membership: generation 2 written (farewell alpha/1)",
		"✓ session: seat 1 released",
		"Removed. "+session+" is no longer in alpha; seat 1 is free.")
	if !slices.Equal(v2.calls, []string{"leave alpha"}) || !slices.Equal(v2.released, []string{"alpha"}) {
		t.Fatalf("calls %v released %v", v2.calls, v2.released)
	}
	if code := r.run(t, "remove", session, "alpha", "--yes"); code != term.ExitOK || len(v2.calls) != 1 {
		t.Fatalf("rerun exit %d, calls %v", code, v2.calls)
	}
}

// Defence for R41's class (a host verb that writes one bus generation's file for a session of
// the other): every verb on a v2 session either acts on its desired.json or refuses naming
// teamchat; none falls through to v1's "launched without the kit" or writes v1's membership.
func TestEveryVerbOnAV2SessionActsOrRefusesByName(t *testing.T) {
	for _, args := range [][]string{
		{"move", session, "alpha", "beta", "--yes"},
		{"role", session, "alpha", "worker", "--yes"},
		{"repair", session, "--yes"},
		{"add", session, "beta", "--role=worker", "--yes"},
		{"remove", session, "alpha", "--park", "--yes"},
		{"remove", session, "alpha", "--coordinator=demote", "--yes"},
	} {
		r, v2 := newV2Rig(t)
		code := r.run(t, args...)
		out := r.ErrOut.String()
		if code != exitConfig || !strings.Contains(out, "teamchat") || strings.Contains(out, "bus kit") {
			t.Fatalf("%v: exit %d:\n%s", args, code, out)
		}
		if len(v2.calls) != 0 {
			t.Fatalf("%v: calls %v", args, v2.calls)
		}
		if _, err := os.Stat(r.dir()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%v: a v1 membership was written: %v", args, err)
		}
	}
}

// Batch 4 review 2: the desired generation a v2 change writes the next one after is read under
// the session's change lock, so two adds cannot both write generation N+1 over N.
func TestAV2SessionsDesiredIsReadUnderItsChangeLock(t *testing.T) {
	r, v2 := newV2Rig(t)
	open := r.x.Teamchat
	r.x.Teamchat = func(checkout, name string) (TeamchatSeats, bool, error) {
		path, err := changePath(r.runtime, session)
		if err != nil {
			t.Fatal(err)
		}
		l, err := takeLock(path, "the test")
		var busy *busyError
		if !errors.As(err, &busy) {
			t.Errorf("the desired membership was read with the change lock free (%v)", err)
		}
		if relErr := l.release(); relErr != nil {
			t.Error(relErr)
		}
		return open(checkout, name)
	}
	if code := r.run(t, "add", session, "beta", "--yes"); code != term.ExitOK || !slices.Equal(v2.calls, []string{"join 2@beta"}) {
		t.Fatalf("exit %d, calls %v:\n%s", code, v2.calls, r.ErrOut.String())
	}
}

// Review 4: a remove whose seat the seat store holds for no-one of this session says so, and
// never that the seat is free.
func TestAV2RemoveOfASeatTheStoreDoesNotHoldSaysSo(t *testing.T) {
	r, v2 := newV2Rig(t)
	v2.notHeld = true
	if code := r.run(t, "remove", session, "alpha", "--yes"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	out := r.ErrOut.String()
	wantLines(t, out,
		"- session: seat 1: the seat store holds no seat of alpha for "+session+", so none was released",
		"Removed. "+session+" is no longer in alpha.")
	if strings.Contains(out, "is free") || strings.Contains(out, "seat 1 released") {
		t.Fatalf("a seat not released was reported freed:\n%s", out)
	}
}

// Review 7: a remove whose release fails says the seat is not released, with the store's exit.
func TestAV2RemoveWhoseReleaseFailsSaysTheSeatIsNotReleased(t *testing.T) {
	r, v2 := newV2Rig(t)
	v2.relErr = seat.ErrBusy
	if code := r.run(t, "remove", session, "alpha", "--yes"); code != exitBusy {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	out := r.ErrOut.String()
	if !strings.Contains(out, "✗ session: seat 1 not released: ") || strings.Contains(out, "is free") {
		t.Fatalf("output:\n%s", out)
	}
}

// Review 7: a put-back whose generation and release both fail names each and the way on; the
// record is still restored.
func TestAV2PutBackThatFailsSaysWhatWasNotPutBack(t *testing.T) {
	r, v2 := newV2Rig(t)
	v2.applied = false
	v2.putErr = errors.New("the disk is full")
	v2.relErr = errors.New("the seat store is unreadable")
	if code := r.run(t, "add", session, "beta", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(),
		"✓ record: restored (--teams 1@alpha)",
		"✗ membership: the disk is full",
		"✗ seat: the seat store is unreadable",
		"The record, the membership or the seat could not be put back (above): run the same command again, or remove the team from the session.")
	if got := r.recordArgs(t); !slices.Equal(got, []string{"--no-ssh", "--teams", "1@alpha"}) {
		t.Fatalf("record %v", got)
	}
}

// A put-back whose seat the store holds for no-one of this session says there was none to release.
func TestAV2PutBackOfASeatTheStoreDoesNotHoldSaysSo(t *testing.T) {
	r, v2 := newV2Rig(t)
	v2.applied, v2.notHeld = false, true
	if code := r.run(t, "add", session, "beta", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	out := r.ErrOut.String()
	wantLines(t, out, "- seat: the seat store holds no seat of beta for "+session+", so none was released")
	if strings.Contains(out, "could not be put back") {
		t.Fatalf("output:\n%s", out)
	}
}

// Review 5: a running cc session launched on teamchat (no v1 membership, a v2 desired.json) is
// refused naming teamchat, never as v1's "not running" or "launched without the kit".
func TestACCSessionOnTeamchatIsRefusedByName(t *testing.T) {
	r := newCCRig(t, membership.Seat{Team: "alpha", Seat: "1"})
	if err := os.RemoveAll(r.dir()); err != nil {
		t.Fatal(err)
	}
	v2 := &fakeV2{t: t}
	r.x.Teamchat = func(checkout, name string) (TeamchatSeats, bool, error) {
		if checkout != r.Checkout() || name != ccSession {
			t.Errorf("opened %s in %s", name, checkout)
		}
		return v2, true, nil
	}
	for _, verb := range [][]string{{"add", ccSession, "beta", "--yes"}, {"remove", ccSession, "alpha", "--yes"}} {
		code := r.run(t, verb...)
		out := r.ErrOut.String()
		if code != exitConfig || !strings.Contains(out, "teamchat") || strings.Contains(out, "bus kit") || strings.Contains(out, "no running container") {
			t.Fatalf("%v: exit %d:\n%s", verb, code, out)
		}
		if len(v2.calls) != 0 {
			t.Fatalf("calls %v", v2.calls)
		}
	}
	// A cc session with no v2 desired.json either is refused as before.
	r.x.Teamchat = func(string, string) (TeamchatSeats, bool, error) { return nil, false, nil }
	if code := r.run(t, "add", ccSession, "beta", "--yes"); code != exitConfig || !strings.Contains(r.ErrOut.String(), "no running container") {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
}

// Review nit 1: a session with no v1 membership whose v2 desired membership cannot be read is
// refused as a configuration (78), naming both, as a session with neither is.
func TestASessionWhoseV2DesiredCannotBeReadIsRefusedAsConfig(t *testing.T) {
	r, v2 := newV2Rig(t)
	r.x.Teamchat = func(string, string) (TeamchatSeats, bool, error) {
		return nil, false, errors.New("the state home: HOME is not set")
	}
	if code := r.run(t, "add", session, "beta", "--yes"); code != exitConfig || len(v2.calls) != 0 {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ session "+session+": it has no agent-bus membership, and whether it runs on teamchat (v2) could not be read: the state home: HOME is not set; relaunch it from ccy's tmux to change it while it runs")
}

// A session with neither a v1 membership nor a v2 desired.json is refused as before.
func TestASessionWithNoMembershipOfEitherBusIsRefusedAsBefore(t *testing.T) {
	r, v2 := newV2Rig(t)
	r.x.Teamchat = func(string, string) (TeamchatSeats, bool, error) { return nil, false, nil }
	if code := r.run(t, "add", session, "beta", "--yes"); code != exitConfig || len(v2.calls) != 0 {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ session "+session+": it was launched without the agent team bus kit (--no-bus, or a ccy older than 3.99); relaunch it from ccy's tmux to change it while it runs")
}

// An add naming another seat of a team the session is already in is a usage error: one seat per
// team per session; nothing is asked of the seat store.
func TestAV2AddOfATeamItIsInAsAnotherSeatIsAUsageError(t *testing.T) {
	r, v2 := newV2Rig(t)
	if code := r.run(t, "add", session, "alpha/2", "--yes"); code != term.ExitUsage || len(v2.calls) != 0 {
		t.Fatalf("exit %d, calls %v:\n%s", code, v2.calls, r.ErrOut.String())
	}
	if !strings.Contains(r.ErrOut.String(), session+" is already in alpha as seat 1: one seat per team per session; remove it first.") {
		t.Fatalf("output:\n%s", r.ErrOut.String())
	}
}

// A team with no pool seat to choose is refused with the chooser's reason; nothing is written.
func TestAV2AddWithNoSeatToChooseChangesNothing(t *testing.T) {
	r, v2 := newV2Rig(t)
	if code := r.run(t, "add", session, "gamma", "--yes"); code != term.ExitFailure || len(v2.calls) != 0 {
		t.Fatalf("exit %d, calls %v:\n%s", code, v2.calls, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ seat: no such pool seat")
	if got := r.recordArgs(t); !slices.Equal(got, []string{"--no-ssh", "--teams", "1@alpha"}) {
		t.Fatalf("record %v", got)
	}
}

// With no terminal and no --yes, add and remove ask first, so they change nothing.
func TestAV2ChangesWithoutYesAndNoTerminalChangeNothing(t *testing.T) {
	for _, args := range [][]string{{"add", session, "beta"}, {"remove", session, "alpha"}} {
		r, v2 := newV2Rig(t)
		if code := r.run(t, args...); code != term.ExitUsage || len(v2.calls) != 0 {
			t.Fatalf("%v: exit %d, calls %v:\n%s", args, code, v2.calls, r.ErrOut.String())
		}
		if !strings.Contains(r.ErrOut.String(), "confirm it with --yes") {
			t.Fatalf("%v: output:\n%s", args, r.ErrOut.String())
		}
	}
}

// A record that cannot be written stops add and remove before the seat store or desired.json.
func TestAV2ChangesWhoseRecordCannotBeWrittenStopFirst(t *testing.T) {
	for _, args := range [][]string{{"add", session, "beta", "--yes"}, {"remove", session, "alpha", "--yes"}} {
		r, v2 := newV2Rig(t)
		v2.onCall = func(string) { t.Errorf("%v: the seat store was asked after the record failed", args) }
		r.x.Teamchat = func(string, string) (TeamchatSeats, bool, error) {
			r.breakRecords(t)
			return v2, true, nil
		}
		if code := r.run(t, args...); code != term.ExitFailure || len(v2.calls) != 0 || v2.desired.Generation != 1 {
			t.Fatalf("%v: exit %d, calls %v:\n%s", args, code, v2.calls, r.ErrOut.String())
		}
	}
}

// A leave whose generation cannot be written puts the record back and fails; no seat is released.
func TestAV2RemoveWhoseGenerationCannotBeWrittenRestoresTheRecord(t *testing.T) {
	r, v2 := newV2Rig(t)
	v2.leaveErr = errors.New("the disk is full")
	if code := r.run(t, "remove", session, "alpha", "--yes"); code != term.ExitFailure || len(v2.released) != 0 {
		t.Fatalf("exit %d, released %v:\n%s", code, v2.released, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ membership: the disk is full", "✓ record: restored (--teams 1@alpha)")
	if got := r.recordArgs(t); !slices.Equal(got, []string{"--no-ssh", "--teams", "1@alpha"}) {
		t.Fatalf("record %v", got)
	}
}

// A failed join whose record cannot be put back either says how to repair it.
func TestAV2FailedJoinWhoseRecordCannotBeRestoredSaysSo(t *testing.T) {
	r, v2 := newV2Rig(t)
	v2.joinErr = errors.New("the seat store is unreadable")
	v2.onCall = func(string) { r.breakRecords(t) }
	if code := r.run(t, "add", session, "beta", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(),
		"✗ seat 2: the seat store is unreadable",
		"The record could not be restored (above): run the same command again, or remove the team from the session.")
}

// A session that ends while its keeper has not yet held the seat is put back, and a put-back
// that fails then names the next launch and the seat sweep as the way on.
func TestAV2SessionEndedDuringTheAddIsPutBack(t *testing.T) {
	r, v2 := newV2Rig(t)
	v2.applied = false
	v2.putErr = errors.New("the disk is full")
	v2.onCall = func(call string) {
		if call == "join" {
			r.m.container = ""
		}
	}
	if code := r.run(t, "add", session, "beta", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(),
		"✗ session: session ended during the add; seat 2 left free",
		"✗ membership: the disk is full",
		"The record, the membership or the seat could not be put back (above): the session's next launch replays its record, and the seat sweep releases the seat.")
	if !slices.Equal(v2.released, []string{"beta"}) {
		t.Fatalf("released %v", v2.released)
	}
}

// A keeper check that errors on every poll is waited out and named, then the seat is put back.
func TestAV2AddWhoseKeeperCheckFailsIsPutBackNamingIt(t *testing.T) {
	r, v2 := newV2Rig(t)
	v2.appErr = errors.New("the seat lock of beta: permission denied")
	if code := r.run(t, "add", session, "beta", "--yes"); code != term.ExitFailure {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ session: seat 2 not held after 10 s: the seat lock of beta: permission denied")
	if !slices.Equal(v2.calls, []string{"join 2@beta", "put 1@alpha"}) || !slices.Equal(v2.released, []string{"beta"}) {
		t.Fatalf("calls %v released %v", v2.calls, v2.released)
	}
}

// ccOnTeamchat: a not-running ccy session never asks teamchat (its record is not a cc one); a cc
// record whose pane is gone is "not running"; a cc session whose v2 desired cannot be read, or
// whose registry cannot be opened, is refused naming the cause.
func TestTheCCOnTeamchatProbe(t *testing.T) {
	r, v2 := newV2Rig(t)
	r.m.container = ""
	r.x.Teamchat = func(string, string) (TeamchatSeats, bool, error) {
		t.Error("a ccy session's record was taken for a cc one")
		return v2, true, nil
	}
	if code := r.run(t, "add", session, "beta", "--yes"); code != exitConfig || !strings.Contains(r.ErrOut.String(), "no running container") {
		t.Fatalf("ccy, not running: exit %d:\n%s", code, r.ErrOut.String())
	}

	cc := newCCRig(t, membership.Seat{Team: "alpha", Seat: "1"})
	if err := os.RemoveAll(cc.dir()); err != nil {
		t.Fatal(err)
	}
	cc.m.pane = 0
	cc.x.Teamchat = func(string, string) (TeamchatSeats, bool, error) {
		t.Error("a cc session with no pane was asked of teamchat")
		return v2, true, nil
	}
	if code := cc.run(t, "add", ccSession, "beta", "--yes"); code != exitConfig || !strings.Contains(cc.ErrOut.String(), "no running container") {
		t.Fatalf("cc, no pane: exit %d:\n%s", code, cc.ErrOut.String())
	}

	cc = newCCRig(t, membership.Seat{Team: "alpha", Seat: "1"})
	if err := os.RemoveAll(cc.dir()); err != nil {
		t.Fatal(err)
	}
	cc.x.Teamchat = func(string, string) (TeamchatSeats, bool, error) {
		return nil, false, errors.New("the seat store: permission denied")
	}
	if code := cc.run(t, "add", ccSession, "beta", "--yes"); code != exitConfig {
		t.Fatalf("cc, unreadable: exit %d:\n%s", code, cc.ErrOut.String())
	}
	wantLines(t, cc.ErrOut.String(), "✗ session "+ccSession+": whether it runs on teamchat (v2) could not be read: the seat store: permission denied")

	cc.Host.Registry = func() (*registry.Store, error) { return nil, errors.New("the registry is unreadable") }
	if code := cc.run(t, "add", ccSession, "beta", "--yes"); code != exitConfig {
		t.Fatalf("cc, no registry: exit %d:\n%s", code, cc.ErrOut.String())
	}
	wantLines(t, cc.ErrOut.String(), "✗ session "+ccSession+": the registry is unreadable")
}
