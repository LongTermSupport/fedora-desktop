package seat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

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

// sessions is a fake of the session records: a name set true is restorable.
type sessions struct {
	mu   sync.Mutex
	live map[string]bool
	err  error
}

func (s *sessions) Restorable(n paths.SessionName) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.live[n.String()], s.err
}

func (s *sessions) set(n string, restorable bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live == nil {
		s.live = map[string]bool{}
	}
	s.live[n] = restorable
}

func (s *sessions) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// world is one owner's seat store under a temporary home, over a homeserver.
type world struct {
	*homeserver

	home     string
	key      paths.CheckoutKey
	store    *Store
	sessions *sessions
	seat     Seat
}

func newWorld(t *testing.T) *world {
	t.Helper()
	clock := newClock()
	w := &world{
		homeserver: newHomeserver(t, clock), home: t.TempDir(), key: mustKey(t, "c0"),
		sessions: &sessions{}, seat: Seat{Team: mustTeam(t), Name: mustSeatName(t, testHandle)},
	}
	w.store = w.open()
	return w
}

func (w *world) config() Config {
	return Config{
		StateHome: filepath.Join(w.home, "state"), RuntimeDir: filepath.Join(w.home, "run"),
		Checkout: w.key, Clock: w.clock, Credential: Password{Clock: w.clock}, Sessions: w.sessions,
	}
}

// pool is the world's pool seat.
func (w *world) pool() paths.PoolSeat {
	return w.store.store.PoolSeat(w.key, w.seat.Team, w.seat.Name)
}

// sessionSeat is the world's seat in a session.
func (w *world) sessionSeat(session string) paths.Seat {
	return w.store.store.SessionSeat(mustSession(w.t, session), w.seat.Team)
}

// seed writes the pool record as a placement would: the password, then seat.json.
func (w *world) seed() {
	w.t.Helper()
	data, err := w.bundle().Encode()
	if err != nil {
		w.t.Fatal(err)
	}
	writeFile(w.t, w.pool().Password(), testPassword)
	writeFile(w.t, w.pool().Bundle(), string(data))
}

func (w *world) take(session string) Taken {
	w.t.Helper()
	got, err := w.tryTake(session, StateActive)
	if err != nil {
		w.t.Fatalf("take for %s: %v", session, err)
	}
	return got
}

func (w *world) tryTake(session string, state HandleState) (Taken, error) {
	w.t.Helper()
	a := w.lock()
	defer w.unlock(a)
	return w.store.Take(w.t.Context(), a, Take{Seat: w.seat, Session: mustSession(w.t, session), State: state})
}

// keep holds a session's seat lock as its keeper would, with content as the lock's record.
func (w *world) keep(session, content string) func() {
	w.t.Helper()
	path := w.sessionSeat(session).Lock()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		w.t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		w.t.Fatal(err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		w.t.Fatal(err)
	}
	var once sync.Once
	end := func() {
		once.Do(func() {
			if err := f.Close(); err != nil {
				w.t.Error(err)
			}
		})
	}
	w.t.Cleanup(end)
	return end
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
