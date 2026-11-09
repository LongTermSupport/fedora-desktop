package sweep

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

func mustTeam(t *testing.T, s string) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return team
}

func mustSeatName(t *testing.T, s string) paths.SeatName {
	t.Helper()
	n, err := paths.ParseSeatName(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func mustSession(t *testing.T, s string) paths.SessionName {
	t.Helper()
	n, err := paths.ParseSessionName(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// call is one call the fake store was asked.
type call struct {
	verb    string
	seat    string
	session string
	leaving bool
}

// fakeStore answers Holdings with its list and records every release and announce, failing
// those of a seat named in fail.
type fakeStore struct {
	mu       sync.Mutex
	holdings []seat.Holding
	listErr  error
	fail     map[string]error
	calls    []call
}

func (f *fakeStore) Holdings() ([]seat.Holding, error) { return f.holdings, f.listErr }

func (f *fakeStore) Release(_ context.Context, r seat.Release) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{verb: "release", seat: r.Seat.Name.String(), session: r.Session.String(), leaving: r.Leaving})
	return f.fail[r.Seat.Name.String()]
}

func (f *fakeStore) Announce(_ context.Context, s seat.Seat, session paths.SessionName) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{verb: "announce", seat: s.Name.String(), session: session.String()})
	return f.fail[s.Name.String()]
}

// sessions is the session records: a name set true is restorable; one in fail is an error.
type sessions struct {
	live map[string]bool
	fail map[string]error
}

func (s sessions) Restorable(n paths.SessionName) (bool, error) {
	return s.live[n.String()], s.fail[n.String()]
}

// desired answers whether a session desires a seat: by "<session> <seat>"; one in fail errs.
type desired struct {
	want map[string]bool
	fail map[string]error
}

func (d desired) Desires(n paths.SessionName, s seat.Seat) (bool, error) {
	key := n.String() + " " + s.Name.String()
	return d.want[key], d.fail[key]
}

func holding(t *testing.T, name, session string) seat.Holding {
	t.Helper()
	return seat.Holding{
		Seat:     seat.Seat{Team: mustTeam(t, "ops"), Name: mustSeatName(t, name)},
		Record:   seat.HeldRecord{V: 1, Session: mustSession(t, session)},
		Complete: true,
	}
}

// SW2: an over session is released, one that no longer desires its seat is released as a
// leave, a restorable one that desires it is announced, and an incomplete held/ is released.
func TestSweepDecidesEachHolding(t *testing.T) {
	incomplete := holding(t, "d.seat", "s4")
	incomplete.Complete, incomplete.Record = false, seat.HeldRecord{}
	store := &fakeStore{holdings: []seat.Holding{
		holding(t, "a.seat", "s1"), holding(t, "b.seat", "s2"), holding(t, "c.seat", "s3"), incomplete,
	}}
	got, err := Sweep(t.Context(), store, sessions{live: map[string]bool{"s2": true, "s3": true}},
		desired{want: map[string]bool{"s3 c.seat": true}})
	if err != nil {
		t.Fatal(err)
	}
	want := []call{
		{verb: "release", seat: "a.seat", session: "s1"},
		{verb: "release", seat: "b.seat", session: "s2", leaving: true},
		{verb: "announce", seat: "c.seat", session: "s3"},
		{verb: "release", seat: "d.seat"},
	}
	if !slices.Equal(store.calls, want) {
		t.Errorf("calls %+v, want %+v", store.calls, want)
	}
	actions := make([]Action, 0, len(got))
	for _, r := range got {
		if r.Err != nil {
			t.Errorf("%s: %v", r.Seat.Name, r.Err)
		}
		actions = append(actions, r.Action)
	}
	if !slices.Equal(actions, []Action{Released, Left, Kept, Released}) {
		t.Errorf("actions %v", actions)
	}
}

// SW2: each holding's failure is its own; the others are still swept.
func TestSweepFailuresAreEachHoldings(t *testing.T) {
	boom := errors.New("boom")
	store := &fakeStore{
		holdings: []seat.Holding{
			holding(t, "a.seat", "s1"), holding(t, "b.seat", "s2"), holding(t, "c.seat", "s3"), holding(t, "e.seat", "s5"),
		},
		fail: map[string]error{"c.seat": boom},
	}
	got, err := Sweep(t.Context(), store,
		sessions{live: map[string]bool{"s2": true, "s3": true, "s5": true}, fail: map[string]error{"s1": boom}},
		desired{want: map[string]bool{"s3 c.seat": true, "s5 e.seat": true}, fail: map[string]error{"s2 b.seat": boom}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("results %+v", got)
	}
	for i, wantErr := range []bool{true, true, true, false} {
		if (got[i].Err != nil) != wantErr || wantErr && !errors.Is(got[i].Err, boom) {
			t.Errorf("%s: %v", got[i].Seat.Name, got[i].Err)
		}
	}
	if want := []call{{verb: "announce", seat: "c.seat", session: "s3"}, {verb: "announce", seat: "e.seat", session: "s5"}}; !slices.Equal(store.calls, want) {
		t.Errorf("calls %+v", store.calls)
	}
}

// A store that cannot list its holdings is the sweep's error; a cancelled sweep stops.
func TestSweepErrors(t *testing.T) {
	boom := errors.New("boom")
	if _, err := Sweep(t.Context(), &fakeStore{listErr: boom}, sessions{}, desired{}); !errors.Is(err, boom) {
		t.Errorf("holdings: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	store := &fakeStore{holdings: []seat.Holding{holding(t, "a.seat", "s1")}}
	if _, err := Sweep(ctx, store, sessions{}, desired{}); !errors.Is(err, context.Canceled) || len(store.calls) != 0 {
		t.Errorf("cancelled: %v, %+v", err, store.calls)
	}
}
