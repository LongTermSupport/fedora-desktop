package seat

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

var testStart = time.UnixMilli(1_800_000_000_000)

func mustSeatName(t *testing.T, s string) paths.SeatName {
	t.Helper()
	n, err := paths.ParseSeatName(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func mustKey(t *testing.T, hex string) paths.CheckoutKey {
	t.Helper()
	k, err := paths.ParseCheckoutKey(strings.Repeat(hex, 16))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// fakeClock is a clock that moves only when the code sleeps.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *fakeClock { return &fakeClock{now: testStart} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return nil
}

// world is one owner's seat store under a temporary home.
type world struct {
	t     *testing.T
	home  string
	key   paths.CheckoutKey
	store *Store
	clock *fakeClock
	seat  Seat
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{
		t: t, home: t.TempDir(), key: mustKey(t, "c0"), clock: newClock(),
		seat: Seat{Team: mustTeam(t), Name: mustSeatName(t, testHandle)},
	}
	w.store = w.open()
	return w
}

func (w *world) config() Config {
	return Config{
		StateHome: filepath.Join(w.home, "state"), RuntimeDir: filepath.Join(w.home, "run"),
		Checkout: w.key, Clock: w.clock,
	}
}

// open is a store over the world's config, closed when the test ends.
func (w *world) open() *Store {
	w.t.Helper()
	s, err := Open(w.config())
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() {
		if err := s.Close(); err != nil {
			w.t.Error(err)
		}
	})
	return s
}

func (w *world) lock() *Alloc {
	w.t.Helper()
	a, err := w.store.Lock(w.t.Context())
	if err != nil {
		w.t.Fatal(err)
	}
	return a
}

func (w *world) unlock(a *Alloc) {
	w.t.Helper()
	if err := a.Unlock(); err != nil {
		w.t.Fatal(err)
	}
}
