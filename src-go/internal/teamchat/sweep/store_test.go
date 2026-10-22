package sweep

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

const (
	testClaim = "00112233aabbccdd"
	testRun   = "0123456789abcdef"
	runRecord = `{"v":1,"run":"` + testRun + `","started_ms":1}`
)

// clock is a clock that moves only when the code sleeps.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return nil
}

// announcer records every notice it is asked for, and answers posted.
type announcer struct {
	mu    sync.Mutex
	asked []seat.Notice
}

func (a *announcer) Disconnected(_ context.Context, n seat.Notice) (seat.Outcome, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked = append(a.asked, n)
	return seat.Outcome{Posted: true}, nil
}

func (a *announcer) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.asked)
}

// world is a real seat store under a temporary home with one holding of session s1, which is
// restorable and desires its seat, and no token, so a release needs no homeserver.
type world struct {
	t        *testing.T
	store    *seat.Store
	paths    paths.Store
	key      paths.CheckoutKey
	seat     seat.Seat
	session  paths.SessionName
	notices  *announcer
	sessions sessions
	desired  desired
}

func newWorld(t *testing.T) *world {
	t.Helper()
	home := t.TempDir()
	key, err := paths.ParseCheckoutKey("c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0")
	if err != nil {
		t.Fatal(err)
	}
	store, err := paths.SeatStore(filepath.Join(home, "state"))
	if err != nil {
		t.Fatal(err)
	}
	clk := &clock{now: time.UnixMilli(1_800_000_000_000)}
	w := &world{
		t: t, paths: store, key: key, notices: &announcer{}, session: mustSession(t, "s1"),
		seat:     seat.Seat{Team: mustTeam(t, "ops"), Name: mustSeatName(t, "repo.1+site.podman")},
		sessions: sessions{live: map[string]bool{"s1": true}},
		desired:  desired{want: map[string]bool{"s1 repo.1+site.podman": true}},
	}
	w.store, err = seat.Open(seat.Config{
		StateHome: filepath.Join(home, "state"), RuntimeDir: filepath.Join(home, "run"), Checkout: key,
		Clock: clk, Credential: seat.Password{Clock: clk}, Sessions: &w.sessions, Announcer: w.notices,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.store.Close(); err != nil {
			t.Error(err)
		}
	})
	w.write(w.pool().HeldRecord(), `{"v":1,"session":"s1","claim":"`+testClaim+`","since_ms":1}`)
	return w
}

func (w *world) pool() paths.PoolSeat { return w.paths.PoolSeat(w.key, w.seat.Team, w.seat.Name) }

func (w *world) sessionSeat() paths.Seat { return w.paths.SessionSeat(w.session, w.seat.Team) }

func (w *world) write(path, data string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

// hold takes the session's seat lock as a starting keeper would, before it writes its record.
func (w *world) hold() {
	w.t.Helper()
	w.write(w.sessionSeat().Lock(), "")
	f, err := os.OpenFile(w.sessionSeat().Lock(), os.O_RDWR, 0)
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() {
		if err := f.Close(); err != nil {
			w.t.Error(err)
		}
	})
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) sweep() []Result {
	w.t.Helper()
	got, err := Sweep(w.t.Context(), w.store, w.sessions, w.desired)
	if err != nil {
		w.t.Fatal(err)
	}
	for _, r := range got {
		if r.Err != nil {
			w.t.Errorf("%s: %v", r.Seat.Name, r.Err)
		}
	}
	return got
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return err == nil
}

// SW2, SW3: a container starting is never announced, whatever its seat directory holds so far:
// none, no lock file, an empty one, or a held one. Its seat stays held. A free lock naming a run
// is announced once.
func TestSweepNeverAnnouncesAStart(t *testing.T) {
	for name, set := range map[string]func(w *world){
		"no seat directory": func(*world) {},
		"no lock file":      func(w *world) { w.write(w.sessionSeat().Bundle(), "{}") },
		"an empty lock":     func(w *world) { w.write(w.sessionSeat().Lock(), "") },
		"a held lock":       func(w *world) { w.hold() },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			set(w)
			for range 2 {
				if got := w.sweep(); len(got) != 1 || got[0].Action != Kept {
					t.Errorf("sweep: %+v", got)
				}
			}
			if w.notices.count() != 0 || exists(t, w.pool().HeldNotice()) || !exists(t, w.pool().HeldRecord()) {
				t.Errorf("announced %d", w.notices.count())
			}
		})
	}
	w := newWorld(t)
	w.write(w.sessionSeat().Lock(), runRecord)
	w.sweep()
	w.sweep()
	if w.notices.count() != 1 || !exists(t, w.pool().HeldNotice()) || !exists(t, w.pool().HeldRecord()) {
		t.Errorf("an ended run: announced %d", w.notices.count())
	}
}
