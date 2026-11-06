package health

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tcclock "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/tlsterm"
)

// The tests are Plan 00246 SPEC-cross-network X32 in the tick: each listen address probed through
// a real terminator (tlsterm) on loopback, serving a leaf made by pki, relaying to a homeserver
// stand-in that answers the versions path; the probe's bound on a fake clock.

// reaches is a world's reach seam: the CA and addresses it gives, and how often it was asked.
type reaches struct {
	mu    sync.Mutex
	reach host.Reach
	err   error
	calls int
}

func (r *reaches) get(paths.TeamID) (host.Reach, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.reach, r.err
}

func (r *reaches) asked() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// terminators are the certificates of a team and the terminators in front of one upstream.
type terminators struct {
	ca, other pki.CA
	upstream  *httptest.Server
	hits      atomic.Int32
	// hit receives once per request the upstream answers.
	hit chan struct{}
}

func newTerminators(t *testing.T) *terminators {
	t.Helper()
	maker := pki.Maker{Clock: wallAt(t0)}
	ca, err := maker.NewCA("teamchat alpha CA")
	if err != nil {
		t.Fatal(err)
	}
	other, err := maker.NewCA("another CA")
	if err != nil {
		t.Fatal(err)
	}
	ts := &terminators{ca: ca, other: other, hit: make(chan struct{}, 16)}
	ts.upstream = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_matrix/client/versions" || r.Header.Get("Authorization") != "" {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		ts.hits.Add(1)
		rw.WriteHeader(http.StatusOK)
		ts.hit <- struct{}{}
	}))
	t.Cleanup(ts.upstream.Close)
	return ts
}

// running is one terminator: where it listens, and its stop, which may be called early.
type running struct {
	addr netip.AddrPort
	stop func()
}

// start is a terminator on a free port of 127.0.0.1, with a leaf for 127.0.0.1 signed by by.
func (ts *terminators) start(t *testing.T, by pki.CA) running {
	t.Helper()
	leaf, err := pki.Maker{Clock: wallAt(t0)}.NewLeaf(by, pki.Names{IPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := tlsterm.Start(t.Context(), tlsterm.Config{
		Listen:   []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0")},
		Upstream: netip.MustParseAddrPort(ts.upstream.Listener.Addr().String()),
		CertPEM:  leaf.CertPEM(), KeyPEM: leaf.KeyPEM(),
		Clock: tcclock.NewFake(0, t0.UnixMilli()),
	})
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			if err := srv.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(stop)
	return running{addr: srv.Addrs()[0], stop: stop}
}

// reachWorld is alpha's world with its reach through the given addresses, trusting the team CA.
func reachWorld(t *testing.T, ts *terminators, addrs ...netip.AddrPort) *world {
	t.Helper()
	w := newWorld(t, "alpha")
	w.reaches.reach = host.Reach{CAPEM: ts.ca.CertPEM(), Addrs: addrs}
	return w
}

func terminatorProblems(rec status.Record) []status.Problem {
	var out []status.Problem
	for _, p := range rec.Problems {
		if strings.HasPrefix(p.What, "the TLS terminator") {
			out = append(out, p)
		}
	}
	return out
}

func silentAt(t *testing.T, addr netip.AddrPort, cause string) status.Problem {
	t.Helper()
	return status.Problem{What: "the TLS terminator does not answer on " + addr.String() + ": " + cause, Fix: "systemctl status " + paths.TLSUnit(id(t, "alpha"))}
}

// X32: every address answers through its terminator, so the record has no terminator problem,
// and each address was asked once.
func TestEveryAddressThatAnswersIsNoProblem(t *testing.T) {
	ts := newTerminators(t)
	w := reachWorld(t, ts, ts.start(t, ts.ca).addr, ts.start(t, ts.ca).addr)
	if out := w.run(t); out[0].Result != ResultWritten {
		t.Fatalf("outcome %+v", out[0])
	}
	if got := terminatorProblems(w.record(t, "alpha")); len(got) != 0 {
		t.Fatalf("problems %v", got)
	}
	if n := ts.hits.Load(); n != 2 {
		t.Fatalf("the homeserver was reached %d times, want 2", n)
	}
}

// X32: one listener closed while its unit shows active is one problem, naming only it.
func TestOneClosedListenerIsAProblemNamingOnlyIt(t *testing.T) {
	ts := newTerminators(t)
	a, b := ts.start(t, ts.ca), ts.start(t, ts.ca)
	b.stop()
	w := reachWorld(t, ts, a.addr, b.addr)
	w.run(t)
	got := terminatorProblems(w.record(t, "alpha"))
	if want := []status.Problem{silentAt(t, b.addr, matrix.ReachRefused)}; !slices.Equal(got, want) {
		t.Fatalf("problems %v, want %v", got, want)
	}
}

// X32: a terminator serving a leaf of another CA fails the handshake, and the problem names the
// pinned CA's fingerprint.
func TestALeafOfAnotherCANamesThePinnedFingerprint(t *testing.T) {
	ts := newTerminators(t)
	a := ts.start(t, ts.other).addr
	w := reachWorld(t, ts, a)
	w.run(t)
	fp, err := pki.Fingerprint(ts.ca.CertPEM())
	if err != nil {
		t.Fatal(err)
	}
	cause := "TLS verification of " + a.String() + " failed against the pinned CA sha256:" + fp + ": unknown authority"
	got := terminatorProblems(w.record(t, "alpha"))
	if want := []status.Problem{silentAt(t, a, cause)}; !slices.Equal(got, want) {
		t.Fatalf("problems %v, want %v", got, want)
	}
	if ts.hits.Load() != 0 {
		t.Fatal("a failed handshake reached the homeserver")
	}
}

// hangingListener accepts and never speaks; what it accepted is closed at the end.
func hangingListener(t *testing.T) netip.AddrPort {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu    sync.Mutex
		conns []net.Conn
		done  = make(chan struct{})
	)
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Error(err)
		}
		<-done
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			if err := c.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	return netip.MustParseAddrPort(ln.Addr().String())
}

// disarmed are the fake's timers with an event for each one stopped before it fired, so a test
// knows when a probe that answered has given up its bound, which another probe's shares.
type disarmed struct {
	*tcclock.Fake

	stopped chan struct{}
}

func (d disarmed) AfterFunc(dur time.Duration, fire func()) func() bool {
	stop := d.Fake.AfterFunc(dur, fire)
	return func() bool {
		was := stop()
		if was {
			d.stopped <- struct{}{}
		}
		return was
	}
}

// X32: an address that hangs is a problem at the bound on the fake clock, and it delays neither
// the other address's probe nor the tick's other work, which are done before the bound passes.
func TestAnAddressThatHangsIsAProblemAtTheBoundAndDelaysNothingElse(t *testing.T) {
	ts := newTerminators(t)
	slow := hangingListener(t)
	w := reachWorld(t, ts, slow, ts.start(t, ts.ca).addr)
	bounds := disarmed{Fake: w.timers, stopped: make(chan struct{}, 4)}
	w.bounds = bounds
	viewed := make(chan struct{})
	w.viewer.during = func() { close(viewed) }
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	tick := w.tick(t)
	done := make(chan []Outcome, 1)
	go func() {
		out, err := tick.RunAll(ctx)
		if err != nil {
			t.Error(err)
		}
		done <- out
	}()
	// The other address answered and gave up its bound, and the view ran, all before the slow
	// address's bound passed.
	for _, wait := range []chan struct{}{ts.hit, bounds.stopped, viewed} {
		select {
		case <-wait:
		case <-done:
			t.Fatal("the tick ended before the slow address's bound passed: it was not bounded by the fake clock")
		case <-ctx.Done():
			t.Fatal("the slow address held up the tick's other work")
		}
	}
	if err := w.timers.AwaitDue(ctx, matrix.ReachLimit); err != nil {
		t.Fatalf("the slow address's bound is not armed: %v", err)
	}
	w.timers.Advance(matrix.ReachLimit)
	select {
	case out := <-done:
		if len(out) != 1 || out[0].Result != ResultWritten {
			t.Fatalf("outcome %+v", out)
		}
	case <-ctx.Done():
		t.Fatal("the tick did not end at the probe's bound")
	}
	got := terminatorProblems(w.record(t, "alpha"))
	if want := []status.Problem{silentAt(t, slow, matrix.ReachNoAnswer)}; !slices.Equal(got, want) {
		t.Fatalf("problems %v, want %v", got, want)
	}
}

// X32: an inactive unit is X18c's one problem; no probe is made.
func TestAnInactiveTerminatorIsOneProblemAndIsNotProbed(t *testing.T) {
	ts := newTerminators(t)
	a := ts.start(t, ts.ca)
	a.stop()
	w := reachWorld(t, ts, a.addr)
	w.units.tls = UnitState{Active: "inactive", Sub: "dead"}
	w.run(t)
	got := terminatorProblems(w.record(t, "alpha"))
	if len(got) != 1 || got[0].What != "the TLS terminator unit is inactive: no member can reach the homeserver" {
		t.Fatalf("problems %v", got)
	}
	if w.reaches.asked() != 0 {
		t.Fatal("an inactive terminator was probed")
	}
}

// X32: a terminator whose state cannot be read is not probed either.
func TestAnUnreadableTerminatorIsNotProbed(t *testing.T) {
	ts := newTerminators(t)
	w := reachWorld(t, ts, ts.start(t, ts.ca).addr)
	w.units.tlsErr = errors.New("dbus down")
	w.run(t)
	if w.reaches.asked() != 0 || ts.hits.Load() != 0 {
		t.Fatal("a terminator of unknown state was probed")
	}
}

// X32, HB3: while a restore is recorded nothing is judged and nothing probed.
func TestNoAddressIsProbedWhileARestoreIsRecorded(t *testing.T) {
	ts := newTerminators(t)
	a := ts.start(t, ts.ca)
	a.stop()
	w := reachWorld(t, ts, a.addr)
	w.restores.state = interrupted()
	w.run(t)
	if got := terminatorProblems(w.record(t, "alpha")); len(got) != 0 {
		t.Fatalf("problems %v", got)
	}
	if w.reaches.asked() != 0 {
		t.Fatal("a probe was made while a restore is recorded")
	}
}

// HB3: a restore recorded after the tick began, seen when the renewal reads the record again under
// the lock, stops the probes too.
func TestNoAddressIsProbedOnceARestoreIsRecordedMeanwhile(t *testing.T) {
	ts := newTerminators(t)
	a := ts.start(t, ts.ca)
	a.stop()
	w := reachWorld(t, ts, a.addr)
	w.restores.state, w.restores.from = interrupted(), 2
	w.run(t)
	if w.reaches.asked() != 0 {
		t.Fatal("a probe was made once a restore was recorded")
	}
	if got := terminatorProblems(w.record(t, "alpha")); len(got) != 0 {
		t.Fatalf("problems %v", got)
	}
}

// A reach that cannot be read is one problem of its own, with apply as its fix.
func TestAReachThatCannotBeReadIsAProblem(t *testing.T) {
	ts := newTerminators(t)
	w := reachWorld(t, ts)
	w.reaches.err = errors.New("units.json: no such file")
	w.run(t)
	got := terminatorProblems(w.record(t, "alpha"))
	want := status.Problem{What: "the TLS terminator cannot be probed: its listen addresses or the team's CA certificate cannot be read", Fix: "teamchat-admin health alpha, then apply the team again (" + paths.PlayFile + ")"}
	if !slices.Equal(got, []status.Problem{want}) {
		t.Fatalf("problems %v, want %v", got, want)
	}
}

// restartingLeaves is a renewal that made the leaf and restarted the terminator.
type restartingLeaves struct{}

func (restartingLeaves) Renew(context.Context, paths.TeamID, host.Terminator) (host.LeafRenewal, error) {
	return host.LeafRenewal{Made: true, Why: "leaf made again: test", Restarted: true}, nil
}

// A terminator the renewal restarted this tick is not probed: it is starting with its new leaf,
// and the next tick judges it.
func TestATerminatorRestartedThisTickIsNotProbed(t *testing.T) {
	ts := newTerminators(t)
	a := ts.start(t, ts.ca)
	a.stop()
	w := reachWorld(t, ts, a.addr)
	w.leaves = restartingLeaves{}
	w.run(t)
	if got := terminatorProblems(w.record(t, "alpha")); len(got) != 0 {
		t.Fatalf("problems %v", got)
	}
	if w.reaches.asked() != 0 {
		t.Fatal("a terminator restarted this tick was probed")
	}
}

// Refresh, a caller holding the lock, probes as the tick does: the probe changes nothing.
func TestRefreshProbesTheTerminator(t *testing.T) {
	ts := newTerminators(t)
	a := ts.start(t, ts.ca)
	a.stop()
	w := reachWorld(t, ts, a.addr)
	w.run(t)
	if err := w.tick(t).Refresh(t.Context(), id(t, "alpha"), nil); err != nil {
		t.Fatal(err)
	}
	got := terminatorProblems(w.record(t, "alpha"))
	if want := []status.Problem{silentAt(t, a.addr, matrix.ReachRefused)}; !slices.Equal(got, want) {
		t.Fatalf("problems %v, want %v", got, want)
	}
	if w.reaches.asked() != 2 {
		t.Fatalf("the reach was asked %d times, want 2", w.reaches.asked())
	}
}
