package matrix

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
)

// armed is one deadline a try armed: its length, the function that ends the try, and whether
// the try stopped it.
type armed struct {
	d       time.Duration
	fire    func()
	stopped bool
}

// fakeTimers records each deadline armed and fires one only when the test says so.
type fakeTimers struct {
	mu     sync.Mutex
	timers []*armed
	notify chan struct{}
}

func newFakeTimers() *fakeTimers { return &fakeTimers{notify: make(chan struct{}, 8)} }

func (f *fakeTimers) AfterFunc(d time.Duration, fire func()) func() bool {
	a := &armed{d: d, fire: fire}
	f.mu.Lock()
	f.timers = append(f.timers, a)
	f.mu.Unlock()
	f.notify <- struct{}{}
	return func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		was := !a.stopped
		a.stopped = true
		return was
	}
}

func (f *fakeTimers) all() []armed {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]armed, 0, len(f.timers))
	for _, a := range f.timers {
		out = append(out, *a)
	}
	return out
}

func timedMember(t *testing.T, base, token string, timers Timers) *Client {
	t.Helper()
	c, err := NewMember(Config{BaseURL: base, PlainHTTPHosts: []string{loopback}, Token: token, Clock: &fakeClock{}, Timers: timers})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

// TestATryEndsWhenItsTimerFires is the per-try deadline on its own seam (round 3 minor a): a
// held request ends "timed out" exactly when the test fires the timer the try armed, and not
// through the 429 Clock.
func TestATryEndsWhenItsTimerFires(t *testing.T) {
	slow, arrived := blockingServer(t)
	timers := newFakeTimers()
	c := timedMember(t, slow.URL, "tok", timers)
	done := make(chan error, 1)
	go func() {
		_, err := c.Sync(t.Context(), SyncRequest{Timeout: 30 * time.Second})
		done <- err
	}()
	<-timers.notify
	<-arrived
	select {
	case err := <-done:
		t.Fatalf("the try ended before its timer fired: %v", err)
	default:
	}
	got := timers.all()
	if len(got) != 1 || got[0].d != 30*time.Second+syncGrace {
		t.Fatalf("armed %+v, want one timer of the long-poll plus 15 s", got)
	}
	got[0].fire()
	e := wantErr(t, <-done, ErrUnreachable, "timed out")
	if !e.Transient() {
		t.Error("a try's deadline is not transient")
	}
	if !timers.all()[0].stopped {
		t.Error("the ended try did not stop its timer")
	}
}

// TestEach429RetryArmsAFreshTimer is SY7's "per try": every try, a 429 retry included, has a
// deadline of its own, and each is stopped when its try ends.
func TestEach429RetryArmsAFreshTimer(t *testing.T) {
	srv := newServer(t)
	m := user(t, srv, "alice")
	tok, err := srv.MintToken(m.uid)
	if err != nil {
		t.Fatal(err)
	}
	timers := newFakeTimers()
	c := timedMember(t, srv.URL(), tok, timers)
	srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: whoamiPath, Status: http.StatusTooManyRequests, Body: []byte(`{"retry_after_ms":1}`), Times: 2})
	if _, err := c.Whoami(t.Context()); err != nil {
		t.Fatalf("whoami after two 429s: %v", err)
	}
	got := timers.all()
	if len(got) != 3 {
		t.Fatalf("armed %d timers, want one per try (3)", len(got))
	}
	for i, a := range got {
		if a.d != memberLimit || !a.stopped {
			t.Errorf("try %d: timer %v stopped %v", i+1, a.d, a.stopped)
		}
	}
}

func TestSystemTimers(t *testing.T) {
	fired := make(chan struct{})
	stop := SystemTimers().AfterFunc(time.Millisecond, func() { close(fired) })
	<-fired
	if stop() {
		t.Error("a fired timer was stopped")
	}
	if !SystemTimers().AfterFunc(time.Hour, func() {})() {
		t.Error("a pending timer did not stop")
	}
}
