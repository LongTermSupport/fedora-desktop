package teamchatlaunch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

type stillClock struct{}

func (stillClock) Now() time.Time { return time.UnixMilli(1_800_000_000_000) }

func (stillClock) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// unusedCredential is a credential no test here reaches: no seat is minted or retired.
type unusedCredential struct{}

func (unusedCredential) Kind() seat.Kind { return seat.KindPassword }

func (unusedCredential) Mint(context.Context, seat.Bundle, seat.Secret) (seat.Secret, error) {
	return seat.Secret{}, errors.New("minted")
}

func (unusedCredential) Retire(context.Context, seat.Bundle, seat.Secret) error {
	return errors.New("retired")
}

type restorable bool

func (r restorable) Restorable(paths.SessionName) (bool, error) { return bool(r), nil }

type noReach struct{}

func (noReach) Reach(context.Context, seat.Bundle) error { return errors.New("reached") }

// storeWorld is a real seat store under a temporary home, in which session s1 holds seat 1 of
// team alpha (its held/ written as a take leaves it), and the pool has no other seat.
type storeWorld struct {
	taker StoreTaker
	held  seat.Seat
	other seat.Seat
	paths paths.Store
	key   paths.CheckoutKey
}

func newStoreWorld(t *testing.T) storeWorld {
	t.Helper()
	home := t.TempDir()
	key, err := paths.CheckoutKeyOf("/work/repo")
	if err != nil {
		t.Fatal(err)
	}
	store, err := paths.SeatStore(filepath.Join(home, "state"))
	if err != nil {
		t.Fatal(err)
	}
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	w := storeWorld{
		paths: store, key: key,
		held:  seat.Seat{Team: team, Name: mustSeat(t, "repo.1+site.podman")},
		other: seat.Seat{Team: team, Name: mustSeat(t, "repo.2+site.podman")},
	}
	record := store.PoolSeat(key, team, w.held.Name).HeldRecord()
	if err := os.MkdirAll(filepath.Dir(record), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record, []byte(`{"v":1,"session":"s1","claim":"00112233aabbccdd","since_ms":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := seat.Config{
		StateHome: filepath.Join(home, "state"), RuntimeDir: filepath.Join(home, "run"), Checkout: key,
		Clock: stillClock{}, Credential: unusedCredential{}, Sessions: restorable(true), Announcer: Announcer{}, Reach: noReach{},
	}
	w.taker = StoreTaker{Open: func() (*seat.Store, error) { return seat.Open(cfg) }}
	return w
}

func (w storeWorld) heldBy(t *testing.T, s seat.Seat) bool {
	t.Helper()
	_, err := os.Stat(w.paths.PoolSeat(w.key, s.Team, s.Name).HeldRecord())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return err == nil
}

// A relaunch or a restore launches the session again on the seats it still holds: those are
// kept as they are, not taken again (which would refuse them as held by a restorable session).
func TestStoreTakerKeepsTheSeatsTheSessionHolds(t *testing.T) {
	w := newStoreWorld(t)
	s1 := mustSession(t, "s1")
	if err := w.taker.TakeAll(t.Context(), nil); err != nil {
		t.Fatalf("nothing to take: %v", err)
	}
	if err := w.taker.TakeAll(t.Context(), []seat.Take{{Seat: w.held, Session: s1, State: seat.StateNoRoster}}); err != nil {
		t.Fatalf("the session's own seat: %v", err)
	}
	if !w.heldBy(t, w.held) {
		t.Fatal("the session's own seat was released")
	}
	err := w.taker.TakeAll(t.Context(), []seat.Take{{Seat: w.held, Session: mustSession(t, "s2"), State: seat.StateNoRoster}})
	if err == nil || !w.heldBy(t, w.held) {
		t.Fatalf("another session's take of the seat s1 holds: %v", err)
	}
}

// A take that fails releases nothing it did not take: the session's own seat stays held.
func TestStoreTakerRefusesWhatThePoolDoesNotHave(t *testing.T) {
	w := newStoreWorld(t)
	s1 := mustSession(t, "s1")
	err := w.taker.TakeAll(t.Context(), []seat.Take{
		{Seat: w.held, Session: s1, State: seat.StateNoRoster}, {Seat: w.other, Session: s1, State: seat.StateNoRoster},
	})
	if err == nil {
		t.Fatal("a seat the pool does not have was taken")
	}
	if !w.heldBy(t, w.held) || w.heldBy(t, w.other) {
		t.Fatal("the failed take changed the holdings")
	}
	broken := StoreTaker{Open: func() (*seat.Store, error) { return nil, errors.New("no store") }}
	if err := broken.TakeAll(t.Context(), nil); err == nil {
		t.Fatal("an unopened store took")
	}
}
