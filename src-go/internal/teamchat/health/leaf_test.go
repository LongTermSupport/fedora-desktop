package health

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// The tests are Plan 00246 SPEC-cross-network X31 in the tick: the leaf made again with no
// apply, by host's step over a real tree of certificates, and the terminator restarted to read it.

const day = 24 * time.Hour

// leaves is the default world's renewal: nothing to do, and the calls counted.
type leaves struct {
	mu    sync.Mutex
	calls int
}

func (l *leaves) Renew(context.Context, paths.TeamID, host.Terminator) (host.LeafRenewal, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	return host.LeafRenewal{}, nil
}

// wallAt is a fixed time as pki reads it.
type wallAt time.Time

func (w wallAt) WallMS() int64 { return time.Time(w).UnixMilli() }

// tlsTree is alpha's certificates under a scratch root: a CA made two years before t0, and a
// leaf for names made at made, so that it ends a year after.
type tlsTree struct {
	root  string
	team  paths.TeamID
	names pki.Names
	end   time.Time
}

func newTLSTree(t *testing.T, made time.Time) *tlsTree {
	t.Helper()
	tr := &tlsTree{root: t.TempDir(), team: id(t, "alpha"), names: pki.Names{IPs: []netip.Addr{netip.MustParseAddr("192.0.2.10")}}}
	ca, err := pki.Maker{Clock: wallAt(t0.AddDate(-2, 0, 0))}.NewCA("teamchat alpha CA")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := pki.Maker{Clock: wallAt(made)}.NewLeaf(ca, tr.names)
	if err != nil {
		t.Fatal(err)
	}
	tr.end = leaf.NotAfter()
	if err := os.MkdirAll(tr.root+paths.TLSDir(tr.team), 0o700); err != nil {
		t.Fatal(err)
	}
	for p, data := range map[string][]byte{
		paths.CACertFile(tr.team): ca.CertPEM(), paths.CAKeyFile(tr.team): ca.KeyPEM(),
		paths.LeafCertFile(tr.team): leaf.CertPEM(), paths.LeafKeyFile(tr.team): leaf.KeyPEM(),
	} {
		if err := os.WriteFile(tr.root+p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return tr
}

// leafCert is the leaf certificate on disk.
func (tr *tlsTree) leafCert(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(tr.root + paths.LeafCertFile(tr.team)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// withLeafLeft is a world whose alpha leaf has left of its year at t0, renewed by host's
// LeafRenewer over the tree.
func withLeafLeft(t *testing.T, left time.Duration) (*world, *tlsTree) {
	t.Helper()
	w := newWorld(t, "alpha")
	tr := newTLSTree(t, t0.Add(left).AddDate(-1, 0, 0))
	w.leaves = host.LeafRenewer{
		Root: tr.root, Clock: w.clock, Units: w.units,
		Names: func(paths.TeamID) (pki.Names, error) { return tr.names, nil },
	}
	return w, tr
}

// leafProblems are the record's problems about the leaf.
func leafProblems(rec status.Record) []status.Problem {
	var out []status.Problem
	for _, p := range rec.Problems {
		if strings.HasPrefix(p.What, "the TLS leaf") {
			out = append(out, p)
		}
	}
	return out
}

// X31: 31 days left, nothing is made and nothing restarted.
func TestThirtyOneDaysLeftMakesNothing(t *testing.T) {
	w, tr := withLeafLeft(t, 31*day)
	was := tr.leafCert(t)
	w.run(t)
	if tr.leafCert(t) != was || w.units.tlsRestarts != 0 {
		t.Fatalf("the leaf changed, or %d restarts", w.units.tlsRestarts)
	}
	if rec := w.record(t, "alpha"); len(rec.Problems) != 0 {
		t.Fatalf("problems %v", whats(rec.Problems))
	}
}

// X31: 29 days left, the leaf is made, the terminator restarted once, the record clean, and the
// next tick does nothing.
func TestTwentyNineDaysLeftMakesTheLeafAndRestartsTheTerminatorOnce(t *testing.T) {
	w, tr := withLeafLeft(t, 29*day)
	was := tr.leafCert(t)
	w.run(t)
	if tr.leafCert(t) == was {
		t.Fatal("the leaf was kept")
	}
	if w.units.tlsRestarts != 1 || w.units.restarts != 0 {
		t.Fatalf("%d terminator restarts and %d homeserver restarts, want 1 and 0", w.units.tlsRestarts, w.units.restarts)
	}
	rec := w.record(t, "alpha")
	if len(rec.Problems) != 0 {
		t.Fatalf("problems %v", whats(rec.Problems))
	}
	if !slices.ContainsFunc(rec.Events, func(e status.Event) bool { return e.What == "the TLS leaf made again: fewer than 30 days remain" }) {
		t.Fatalf("events %+v", rec.Events)
	}
	made := tr.leafCert(t)
	w.clock.advance(time.Minute)
	w.run(t)
	if tr.leafCert(t) != made || w.units.tlsRestarts != 1 {
		t.Fatalf("the next tick made the leaf again or restarted (%d restarts)", w.units.tlsRestarts)
	}
}

// X31: a changed name set makes the leaf again at any age.
func TestAChangedNameSetMakesTheLeafAtAnyAge(t *testing.T) {
	w, tr := withLeafLeft(t, 200*day)
	was := tr.leafCert(t)
	tr.names = pki.Names{IPs: []netip.Addr{netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("192.0.2.11")}}
	w.run(t)
	if tr.leafCert(t) == was || w.units.tlsRestarts != 1 {
		t.Fatalf("the leaf was kept, or %d restarts", w.units.tlsRestarts)
	}
}

// X31: the CA key unreadable at 29 days: a problem naming the leaf's end and the cause, no
// restart, and the same problem at the next tick.
func TestAnUnreadableCAKeyIsAProblemNamingTheDateAtEveryTick(t *testing.T) {
	w, tr := withLeafLeft(t, 29*day)
	if err := os.Remove(tr.root + paths.CAKeyFile(tr.team)); err != nil {
		t.Fatal(err)
	}
	was := tr.leafCert(t)
	date := tr.end.UTC().Format(time.DateOnly)
	var first []status.Problem
	for tick := range 2 {
		w.run(t)
		got := leafProblems(w.record(t, "alpha"))
		if len(got) != 1 || !strings.Contains(got[0].What, "expires on "+date) || !strings.Contains(got[0].What, paths.CAKeyFile(tr.team)) {
			t.Fatalf("tick %d: leaf problems %+v, want one naming %s and the CA key", tick, got, date)
		}
		if tick == 1 && !slices.Equal(got, first) {
			t.Fatalf("the next tick's problem %+v, the first %+v", got, first)
		}
		first = got
		w.clock.advance(time.Minute)
	}
	if tr.leafCert(t) != was || w.units.tlsRestarts != 0 {
		t.Fatalf("the leaf changed, or %d restarts", w.units.tlsRestarts)
	}
}

// X31: a leaf past its end that cannot be made again is a problem of its own.
func TestAnExpiredLeafIsAProblemOfItsOwn(t *testing.T) {
	w, tr := withLeafLeft(t, -day)
	if err := os.Remove(tr.root + paths.CAKeyFile(tr.team)); err != nil {
		t.Fatal(err)
	}
	w.run(t)
	got := leafProblems(w.record(t, "alpha"))
	want := "the TLS leaf expired on " + tr.end.UTC().Format(time.DateOnly) + ": no member can reach the homeserver"
	if len(got) != 1 || !strings.HasPrefix(got[0].What, want) || strings.Contains(got[0].What, "expires on") {
		t.Fatalf("leaf problems %+v, want one starting %q", got, want)
	}
}

// X31, HB3: nothing is judged or made while a restore is recorded.
func TestNoLeafIsJudgedWhileARestoreIsRecorded(t *testing.T) {
	w, tr := withLeafLeft(t, 29*day)
	w.restores.state = interrupted()
	was := tr.leafCert(t)
	w.run(t)
	if tr.leafCert(t) != was || w.units.tlsRestarts != 0 || len(leafProblems(w.record(t, "alpha"))) != 0 {
		t.Fatalf("the leaf changed, %d restarts, problems %v", w.units.tlsRestarts, whats(w.record(t, "alpha").Problems))
	}
}

// X31: a renewal that cannot say when the leaf ends still names its cause.
func TestALeafThatCannotBeJudgedIsAProblem(t *testing.T) {
	w, tr := withLeafLeft(t, 29*day)
	if err := os.Remove(tr.root + paths.CACertFile(tr.team)); err != nil {
		t.Fatal(err)
	}
	w.run(t)
	got := leafProblems(w.record(t, "alpha"))
	if len(got) != 1 || !strings.HasPrefix(got[0].What, "the TLS leaf cannot be judged: ") || !strings.Contains(got[0].What, paths.CACertFile(tr.team)) {
		t.Fatalf("leaf problems %+v", got)
	}
}

// The default world asks for a renewal once per tick, and never while the caller holds the lock.
func TestTheRenewalIsAskedOncePerTickAndNotByRefresh(t *testing.T) {
	w := newWorld(t, "alpha")
	l := &leaves{}
	w.leaves = l
	w.run(t)
	if l.calls != 1 {
		t.Fatalf("%d renewals", l.calls)
	}
	if err := w.tick(t).Refresh(t.Context(), id(t, "alpha"), nil); err != nil {
		t.Fatal(err)
	}
	if l.calls != 1 {
		t.Fatalf("Refresh renewed: %d renewals", l.calls)
	}
}
