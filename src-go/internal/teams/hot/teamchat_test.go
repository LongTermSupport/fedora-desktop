package hot

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

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
	calls    []string
	released []string
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
	if f.joinErr != nil {
		return f.joinErr
	}
	f.next(append(slices.Clone(f.desired.Seats), state.DesiredSeat{Team: mustTeam(f.t, team), Seat: seatPart}), f.desired.Farewell)
	return nil
}

func (f *fakeV2) Leave(team string) (state.DesiredSeat, error) {
	f.calls = append(f.calls, "leave "+team)
	i := slices.IndexFunc(f.desired.Seats, func(s state.DesiredSeat) bool { return s.Team.String() == team })
	left := f.desired.Seats[i]
	f.next(slices.Delete(slices.Clone(f.desired.Seats), i, i+1),
		append(slices.Clone(f.desired.Farewell), state.Farewell{Team: left.Team, Seat: left.Seat, Reason: state.FarewellReleased}))
	return left, nil
}

func (f *fakeV2) Put(seats []state.DesiredSeat) error {
	f.calls = append(f.calls, "put "+seatList(seats))
	f.next(slices.Clone(seats), f.desired.Farewell)
	return nil
}

func (f *fakeV2) Applied(team, seatPart string) (bool, string, error) {
	if f.applied && f.desired.Holds(mustTeam(f.t, team), seatPart) {
		return true, "", nil
	}
	return false, "the session's keeper has written no report of the seat", nil
}

func (f *fakeV2) Release(_ context.Context, team string) error {
	f.released = append(f.released, team)
	return nil
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

// A session with neither a v1 membership nor a v2 desired.json is refused as before.
func TestASessionWithNoMembershipOfEitherBusIsRefusedAsBefore(t *testing.T) {
	r, v2 := newV2Rig(t)
	r.x.Teamchat = func(string, string) (TeamchatSeats, bool, error) { return nil, false, nil }
	if code := r.run(t, "add", session, "beta", "--yes"); code != exitConfig || len(v2.calls) != 0 {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	wantLines(t, r.ErrOut.String(), "✗ session "+session+": it was launched without the agent team bus kit (--no-bus, or a ccy older than 3.99); relaunch it from ccy's tmux to change it while it runs")
}
