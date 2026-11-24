package seat

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// retireSpy is the password credential that counts its Retire calls: an abandon makes none.
type retireSpy struct {
	Password

	mu      sync.Mutex
	retired int
}

func (r *retireSpy) Retire(ctx context.Context, b Bundle, held Secret) error {
	r.mu.Lock()
	r.retired++
	r.mu.Unlock()
	return r.Password.Retire(ctx, b, held)
}

func (r *retireSpy) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.retired
}

// spy reopens the world's store with a retireSpy as its credential.
func (w *world) spy() *retireSpy {
	spy := &retireSpy{Password: Password{Clock: w.clock}}
	w.reopen(func(c *Config) { c.Credential = spy })
	return spy
}

func (w *world) abandon(seat Seat, reason, by string) (Abandoned, error) {
	w.t.Helper()
	return w.store.Abandon(w.t.Context(), seat, reason, by)
}

// An abandon releases the held seat on the owner's word with no logout: the session's copy and
// held/ go to quarantine as a release moves them, the token file is unlinked, released.json
// says why, by whom and when, and the token stays live on its server (no call was made).
func TestAbandon(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s1")
	token := w.readFile(w.pool().HeldToken())
	spy := w.spy()
	got, err := w.abandon(w.seat, "team purged", "owner")
	if err != nil || !got.Held || got.Session.String() != "s1" {
		t.Fatalf("abandon: %+v, %v", got, err)
	}
	if exists(t, taken.Dir) || !exists(t, w.releasedSession(taken)+"/seat.json") {
		t.Errorf("the session's seat directory was not moved to quarantine")
	}
	if exists(t, w.pool().Held()) || !exists(t, w.releasedPool(taken)+"/held.json") || exists(t, w.releasedPool(taken)+"/token") {
		t.Errorf("held/ was not moved to quarantine with its token unlinked")
	}
	raw := decodeJSON(t, w.readFile(w.pool().ReleasedRecord()))
	ab, ok := raw["abandoned"].(map[string]any)
	if !ok || ab["reason"] != "team purged" || ab["by"] != "owner" || ab["at_ms"] != float64(w.clock.Now().UnixMilli()) || len(ab) != 3 {
		t.Errorf("released.json %v, want abandoned {reason, by, at_ms}", raw)
	}
	rec, err := decodeReleasedRecord([]byte(w.readFile(w.pool().ReleasedRecord())))
	if err != nil || rec.Claim != taken.Claim || rec.Abandoned == nil {
		t.Errorf("released.json does not decode as this claim's abandonment: %+v, %v", rec, err)
	}
	if spy.calls() != 0 || !w.alive(token) || len(w.notices.calls()) != 0 {
		t.Errorf("the abandon contacted the server: %d retires, %d notices", spy.calls(), len(w.notices.calls()))
	}
}

// A seat that is not held is nothing to do, and no error.
func TestAbandonNotHeld(t *testing.T) {
	w := newWorld(t)
	w.seed()
	got, err := w.abandon(w.seat, "gone", "owner")
	if err != nil || got.Held {
		t.Errorf("abandon of a free seat: %+v, %v", got, err)
	}
	if exists(t, w.pool().ReleasedRecord()) {
		t.Errorf("a free seat got a released.json")
	}
}

// A seat whose member link runs for its team is refused, held/ and the copy kept.
func TestAbandonRunning(t *testing.T) {
	w := newWorld(t)
	w.seed()
	taken := w.take("s1")
	w.keep("s1", lockRecord)
	if _, err := w.abandon(w.seat, "gone", "owner"); !errors.Is(err, ErrRunning) {
		t.Fatalf("abandon of a running seat: %v", err)
	}
	if !exists(t, w.pool().HeldToken()) || !exists(t, taken.Dir) || exists(t, w.pool().ReleasedRecord()) {
		t.Errorf("a refused abandon changed the seat")
	}
}

// A reason or a caller that is empty or not one line is refused before anything moves.
func TestAbandonNeedsReason(t *testing.T) {
	w := newWorld(t)
	w.seed()
	w.take("s1")
	for _, c := range [][2]string{{"", "owner"}, {"two\nlines", "owner"}, {"gone", ""}} {
		if _, err := w.abandon(w.seat, c[0], c[1]); !errors.Is(err, ErrInvalid) {
			t.Errorf("abandon %q by %q: %v, want ErrInvalid", c[0], c[1], err)
		}
	}
	if !exists(t, w.pool().HeldToken()) {
		t.Errorf("a refused abandon moved held/")
	}
}

// deadTeam is a second team, "dead", with its own homeserver and the test handle's account,
// its pool seat placed in the world's checkout.
func (w *world) deadTeam() Seat {
	w.t.Helper()
	team, err := paths.ParseTeamID("dead")
	if err != nil {
		w.t.Fatal(err)
	}
	server := paths.ServerName(team)
	hs := fakehs.New(fakehs.Options{ServerName: server, Now: func() time.Time { return testStart }})
	if err := hs.Start(); err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() {
		if err := hs.Stop(); err != nil {
			w.t.Error(err)
		}
	})
	hs.AddUser(testHandle, testPassword, false)
	b := bundleAt(w.t, hs.URL())
	b.Team, b.ServerName, b.UserID, b.AdminID = team, server, "@"+testHandle+":"+server, "@admin:"+server
	data, err := b.Encode()
	if err != nil {
		w.t.Fatal(err)
	}
	seat := Seat{Team: team, Name: w.seat.Name}
	writeFile(w.t, w.poolOf(seat).Password(), testPassword)
	writeFile(w.t, w.poolOf(seat).Bundle(), string(data))
	return seat
}

// The running check is per team: one session holds a live seat of team ops (its link running)
// and a dead seat of team dead. Abandoning dead succeeds while ops's link runs, and ops's seat,
// its copy and its link's lock are untouched.
func TestAbandonPerTeam(t *testing.T) {
	w := newWorld(t)
	w.seed()
	dead := w.deadTeam()
	if _, err := w.takeSeat(dead, "s1"); err != nil {
		t.Fatalf("take of the dead seat: %v", err)
	}
	live := w.take("s1")
	w.keep("s1", lockRecord)
	liveHeld := w.readFile(w.pool().HeldRecord())
	got, err := w.abandon(dead, "team purged", "owner")
	if err != nil || !got.Held {
		t.Fatalf("abandon of the dead seat while the live team's link runs: %+v, %v", got, err)
	}
	if exists(t, w.poolOf(dead).Held()) || exists(t, w.store.store.SessionSeat(mustSession(t, "s1"), dead.Team).Dir()) {
		t.Errorf("the dead seat was not released")
	}
	if w.readFile(w.pool().HeldRecord()) != liveHeld || !exists(t, live.Dir) || exists(t, w.pool().ReleasedRecord()) {
		t.Errorf("the live team's seat was touched")
	}
	if p, err := w.store.probe(w.sessionSeat("s1")); err != nil || p.state != lockHeld {
		t.Errorf("the live team's link lock: %+v, %v", p, err)
	}
}

// released.json's abandoned is optional, and when there it has exactly reason, by and at_ms,
// each well formed.
func TestReleasedAbandoned(t *testing.T) {
	const head = `{"v":1,"claim":"00112233aabbccdd","released_ms":1`
	for doc, ok := range map[string]bool{
		head + `}`: true,
		head + `,"abandoned":{"reason":"gone","by":"owner","at_ms":1}}`:       true,
		head + `,"abandoned":{"reason":"","by":"owner","at_ms":1}}`:           false,
		head + `,"abandoned":{"reason":"a\nb","by":"owner","at_ms":1}}`:       false,
		head + `,"abandoned":{"reason":"gone","by":"","at_ms":1}}`:            false,
		head + `,"abandoned":{"reason":"gone","by":"owner","at_ms":-1}}`:      false,
		head + `,"abandoned":{"reason":"gone","by":"owner"}}`:                 false,
		head + `,"abandoned":{"reason":"gone","by":"owner","at_ms":1,"x":1}}`: false,
		head + `,"abandoned":{"REASON":"gone","by":"owner","at_ms":1}}`:       false,
		head + `,"abandoned":"gone"}`:                                         false,
		head + `,"abandoned":null}`:                                           false,
	} {
		if _, err := decodeReleasedRecord([]byte(doc)); (err == nil) != ok {
			t.Errorf("%s: %v, want ok %v", doc, err, ok)
		}
	}
}
