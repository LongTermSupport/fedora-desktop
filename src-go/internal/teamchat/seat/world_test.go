package seat

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const testPassword = "pool-password-1"

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

// homeserver is a running fakehs with the seat's account made, its password testPassword.
type homeserver struct {
	t      *testing.T
	hs     *fakehs.Server
	clock  *fakeClock
	userID string
}

func newHomeserver(t *testing.T, clock *fakeClock) *homeserver {
	t.Helper()
	hs := fakehs.New(fakehs.Options{ServerName: paths.ServerName(mustTeam(t)), Now: func() time.Time { return testStart }})
	if err := hs.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := hs.Stop(); err != nil {
			t.Error(err)
		}
	})
	return &homeserver{t: t, hs: hs, clock: clock, userID: hs.AddUser(testHandle, testPassword, false)}
}

// bundle is the seat's pool seat.json on this server.
func (h *homeserver) bundle() Bundle { return bundleAt(h.t, h.hs.URL()) }

// member is a client holding token on the server.
func (h *homeserver) member(token string) *matrix.Client {
	h.t.Helper()
	c, err := matrix.NewMember(matrix.Config{BaseURL: h.hs.URL(), PlainHTTPHosts: []string{loopback}, Token: token, Clock: h.clock})
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(c.Close)
	return c
}

// alive is whether token is a live token of the seat's account.
func (h *homeserver) alive(token string) bool {
	h.t.Helper()
	who, err := h.member(token).Whoami(h.t.Context())
	if e, failed := errors.AsType[*matrix.Error](err); failed && e.Errcode == "M_UNKNOWN_TOKEN" {
		return false
	}
	if err != nil {
		h.t.Fatalf("whoami: %v", err)
	}
	return who.UserID == h.userID
}

// stale is a token an earlier holder of the seat's device kept: a login as the device.
func (h *homeserver) stale() string {
	h.t.Helper()
	sess, err := h.member(signedOut).LoginDevice(h.t.Context(), testHandle, testPassword, paths.SeatDevice)
	if err != nil {
		h.t.Fatal(err)
	}
	return sess.AccessToken.Reveal()
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
