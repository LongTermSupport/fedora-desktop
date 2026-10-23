package teamsync

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/bundle"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// waitHealth waits for the engine to be told a health that ok accepts.
func (w *world) waitHealth(ok func(team.Health) bool) {
	w.t.Helper()
	for {
		hs := w.eng.healths()
		if slices.ContainsFunc(hs, ok) {
			return
		}
		select {
		case <-w.t.Context().Done():
			w.t.Fatalf("no such health in %+v", hs)
		case <-w.eng.healthed:
		}
	}
}

func stoppedWith(code string) func(team.Health) bool {
	return func(h team.Health) bool { return h.Stopped == code }
}

// SY8: a token refused on an unchanged pair stops the team with token; a new pair and a wake
// (LN9: the watcher's change) start it again.
func TestARefusedTokenStopsUntilANewPair(t *testing.T) {
	w := newWorld(t)
	good := w.tokens[me]
	w.place("not-the-token", nil)
	r := w.start()
	w.waitHealth(stoppedWith(exits.CodeToken))
	w.place(good, nil)
	w.watch.fire()
	if b := w.batch(); b.Fresh || b.NextBatch == "" {
		t.Errorf("batch = %+v", b)
	}
	r.poll(t)
	if h := w.eng.lastHealth(); h.Stopped != "" {
		t.Errorf("health = %+v", h)
	}
	if n := w.log.count("stopped (token)"); n != 1 {
		t.Errorf("log = %q", w.log.String())
	}
}

// n3c-D12 and LN9's hot reissue: a new token placed before seat.json, the old device logged out:
// the 401 re-reads the pair, finds it changed and carries on without a stop.
func TestTheTokenRace(t *testing.T) {
	w := newWorld(t)
	r := w.start()
	w.batch()
	r.poll(t)
	old := w.client(me)
	token, err := w.srv.MintToken(user(me))
	check(t, err)
	w.replace(w.seat.Token(), []byte(token))
	check(t, old.Logout(t.Context()))
	w.tokens[me] = token
	w.say(alice, human("one", user(me)))
	w.batch()
	r.poll(t)
	id := w.say(alice, human("two", user(me)))
	if b := w.batch(); len(b.Items) != 1 || b.Items[0].Pending.ID != id {
		t.Fatalf("batch = %+v", b)
	}
	for _, h := range w.eng.healths() {
		if h.Stopped != "" {
			t.Errorf("stopped: %+v", h)
		}
	}
}

// A second 401 after a changed pair stops: the retry is once.
func TestASecond401Stops(t *testing.T) {
	w := newWorld(t)
	reads := 0
	tm := w.newTeam()
	tm.readPair = func(root *homefs.Root, s paths.Seat) (bundle.Pair, error) {
		reads++
		p, err := bundle.ReadPair(root, s)
		if reads == 2 {
			p.Bundle.Bundle.IssuedMS++
		}
		return p, err
	}
	w.place("not-the-token", nil)
	w.startTeam(tm)
	w.waitHealth(stoppedWith(exits.CodeToken))
	if reads != 3 {
		t.Errorf("the pair was read %d times, want 3 (session, retry, session)", reads)
	}
}

// SY8: a 403 stops forbidden; a roster the server will not give stops untrusted (SY6).
func TestForbiddenAndUntrustedStop(t *testing.T) {
	w := newWorld(t)
	w.srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: "/sync", Status: http.StatusForbidden, Body: []byte(`{"errcode":"M_FORBIDDEN","error":"x"}`)})
	w.start()
	w.waitHealth(stoppedWith(exits.CodeForbidden))

	w2 := newWorld(t)
	w2.srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: "/state/teamchat.roster", Status: http.StatusNotFound, Body: []byte(`{"errcode":"M_NOT_FOUND","error":"x"}`)})
	w2.start()
	w2.waitHealth(stoppedWith(exits.CodeUntrusted))
	w2.noBatch()
}

// LN9: a torn pair, or none yet, waits for the next change without a stop; a refused pair stops
// bundle.
func TestAPairNotReadyWaits(t *testing.T) {
	for _, want := range []error{bundle.ErrTorn, fs.ErrNotExist} {
		w := newWorld(t)
		tm := w.newTeam()
		torn := true
		tm.readPair = func(root *homefs.Root, s paths.Seat) (bundle.Pair, error) {
			if torn {
				torn = false
				return bundle.Pair{}, want
			}
			return bundle.ReadPair(root, s)
		}
		w.startTeam(tm)
		w.noBatch()
		w.watch.fire()
		w.batch()
		for _, h := range w.eng.healths() {
			if h.Stopped != "" {
				t.Errorf("%v: stopped %+v", want, h)
			}
		}
	}
	w := newWorld(t)
	w.replace(w.seat.Bundle(), []byte(`{"v":1}`))
	w.start()
	w.waitHealth(stoppedWith(exits.CodeBundle))
}

// SY8's transient path: 1 s doubling to 30 s, down at 15 s with one line, back with one line.
func TestTransientFailuresBackOff(t *testing.T) {
	w := newWorld(t)
	w.srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: "/sync", Status: http.StatusBadGateway, Body: []byte(`{}`), Times: 6})
	r := w.start()
	got := make([]time.Duration, 0, 6)
	for range 6 {
		d := r.wait(t)
		got = append(got, d)
		w.clk.Advance(d)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second}
	if !slices.Equal(got, want) {
		t.Errorf("waits = %v", got)
	}
	w.batch()
	r.poll(t)
	if w.log.count("down: ") != 1 || w.log.count("back up") != 1 {
		t.Errorf("log = %q", w.log.String())
	}
	w.waitHealth(func(h team.Health) bool { return h.Down })
	if h := w.eng.lastHealth(); h.Down {
		t.Errorf("still down: %+v", h)
	}
}

// A wake ends a backoff early; the jitter is taken off the wait.
func TestAWakeEndsTheBackoff(t *testing.T) {
	w := newWorld(t)
	w.srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: "/sync", Status: http.StatusBadGateway, Body: []byte(`{}`)})
	r := w.start(func(c *Config) { c.Jitter = func(d time.Duration) time.Duration { return d / 4 } })
	if d := r.wait(t); d != 750*time.Millisecond {
		t.Errorf("wait = %v", d)
	}
	r.tm.Wake()
	w.batch()
}

// SY13: a failed store write holds the position, is said once, and retries on the backoff.
func TestAFailedStoreWriteHoldsThePosition(t *testing.T) {
	w := newWorld(t)
	r := w.start()
	since := w.batch().NextBatch
	r.poll(t)
	fails := 2
	w.eng.setPersistErr(func(team.Batch) error {
		if fails > 0 {
			fails--
			return errors.New("disk full")
		}
		return nil
	})
	id := w.say(alice, human("held", user(me)))
	w.clk.Advance(r.wait(t))
	if got := r.poll(t); got != since {
		t.Errorf("after a failed write the team polls from %q, want %q", got, since)
	}
	w.clk.Advance(r.wait(t))
	r.poll(t)
	if b := w.batch(); len(b.Items) != 1 || b.Items[0].Pending.ID != id {
		t.Errorf("batch = %+v", b)
	}
	if n := w.log.count("the store write failed"); n != 1 {
		t.Errorf("said %d times", n)
	}
}

// A removed team ends Run with no error.
func TestARemovedTeamEnds(t *testing.T) {
	w := newWorld(t)
	w.eng.setPersistErr(func(team.Batch) error { return team.ErrRemoved })
	tm := w.newTeam()
	if err := tm.Run(t.Context()); err != nil {
		t.Errorf("Run = %v", err)
	}
}

// Step 4: a store of another run is closed and this run begun before the first long-poll; back
// is posted once with the reloaded run, after the first SessionStart.
func TestClosesThePreviousRunAndPostsBack(t *testing.T) {
	w := newWorld(t)
	w.eng.preload(user(me), runID(t, oldRun), "", 0)
	first := make(chan struct{})
	w.ends.closeErr = errors.New("transient")
	r := w.start(func(c *Config) { c.FirstSessionStart = first })
	w.clk.Advance(r.wait(t))
	r.poll(t)
	closes, backs := w.ends.runs()
	begun := w.eng.begunRuns()
	if len(closes) != 1 || begun != 1 || len(backs) != 0 || closes[0].ID != runID(t, oldRun) {
		t.Fatalf("closes %+v, begun %d, backs %+v", closes, begun, backs)
	}
	close(first)
	select {
	case <-w.ends.backed:
	case <-t.Context().Done():
		t.Fatal("no back")
	}
	if _, backs := w.ends.runs(); len(backs) != 1 || backs[0].ID != runID(t, linkRun) {
		t.Errorf("backs %+v", backs)
	}
}

// --plain posts back at once; a back that fails is one line.
func TestPlainPostsBackAtOnce(t *testing.T) {
	w := newWorld(t)
	w.ends.backErr = errors.New("unreachable")
	w.start(func(c *Config) { c.Plain, c.FirstSessionStart = true, nil })
	select {
	case <-w.ends.backed:
	case <-t.Context().Done():
		t.Fatal("no back")
	}
}

func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("an empty config was taken")
	}
	w := newWorld(t)
	tm, err := New(Config{Team: w.team, Seat: w.seat, Root: w.root, Engine: w.eng, Ends: w.ends, Clock: w.clk})
	check(t, err)
	if j := tm.cfg.Jitter(8 * time.Second); j < 0 || j > 2*time.Second {
		t.Errorf("jitter %v", j)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := tm.Run(ctx); err != nil {
		t.Errorf("Run on an ended context = %v", err)
	}
}
