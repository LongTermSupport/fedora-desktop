package host

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
)

// The tests are Plan 00246 SPEC-cross-network X31 at the host: the leaf made again with no apply,
// by the step `team apply` makes it with, and the terminator restarted so that it reads it.

const day = 24 * time.Hour

// renewer is the world's LeafRenewer, the names it wants those the applied files give.
func renewer(w *world) LeafRenewer {
	return LeafRenewer{Root: w.root, Clock: w.clock, Rand: w.rand, Units: w.sd, Names: AppliedLeafNames(w.root), StateOwnerUID: self(w.t)}
}

// restartsOf are the restarts of the team's terminator since the log was last cleared.
func restartsOf(w *world) int {
	n := 0
	for _, name := range w.log {
		if name == "restart" {
			n++
		}
	}
	return n
}

// leafModes holds the leaf's two files to the modes apply gives them.
func leafModes(t *testing.T, w *world) {
	t.Helper()
	team := ops(t)
	for p, m := range map[string]os.FileMode{paths.LeafKeyFile(team): 0o600, paths.LeafCertFile(team): 0o644} {
		if got := mode(t, w.at(p)); got != m {
			t.Errorf("%s: mode %v, want %v", p, got, m)
		}
	}
}

// running is a terminator active since at.
func running(at time.Time) Terminator { return Terminator{Active: true, Since: at} }

// X31: the names a renewal wants are those the applied files record, the listen set of
// units.json and the url host of team.json, as apply derived them.
func TestTheAppliedNamesAreTheOnesApplyMadeTheLeafFor(t *testing.T) {
	w := newWorld(t)
	doc := teamDoc()
	docTeam(doc)["url"] = "https://chat.example.com:8448"
	if _, err := w.apply(approved(w, doc)); err != nil {
		t.Fatal(err)
	}
	names, err := AppliedLeafNames(w.root)(ops(t))
	if err != nil {
		t.Fatal(err)
	}
	if !sameNames(names, []string{"192.0.2.10"}, "chat.example.com") {
		t.Fatalf("names %+v", names)
	}
	if _, err := AppliedLeafNames(w.root)(mustTeam(t, "other")); err == nil {
		t.Fatal("a team never applied has names")
	}
}

// X31: 31 days left, nothing is made and nothing restarted.
func TestARenewalWithThirtyOneDaysLeftDoesNothing(t *testing.T) {
	w := applied(t)
	was := certificateFiles(t, w)
	end := theLeaf(t, w).NotAfter()
	w.clock.now, w.log = end.Add(-31*day), nil
	got, err := renewer(w).Renew(t.Context(), ops(t), running(t0))
	if err != nil {
		t.Fatal(err)
	}
	if got.Made || got.Restarted || !got.NotAfter.Equal(end) || restartsOf(w) != 0 {
		t.Fatalf("renewal %+v, %d restarts", got, restartsOf(w))
	}
	if !sameFiles(was, certificateFiles(t, w)) {
		t.Fatal("a file changed")
	}
}

// X31: 29 days left, the leaf is made where apply writes it, signed by the team's CA, the
// terminator restarted once, and the next renewal does nothing.
func TestARenewalWithTwentyNineDaysLeftMakesTheLeafAndRestartsOnce(t *testing.T) {
	w := applied(t)
	team := ops(t)
	was := certificateFiles(t, w)
	end := theLeaf(t, w).NotAfter()
	w.clock.now, w.log, w.sd.started = end.Add(-29*day), nil, nil
	got, err := renewer(w).Renew(t.Context(), team, running(t0))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Made || !got.Restarted || got.Why != "leaf made again: fewer than 30 days remain" {
		t.Fatalf("renewal %+v", got)
	}
	leaf := theLeaf(t, w)
	if !got.NotAfter.Equal(leaf.NotAfter()) || !leaf.NotAfter().After(end) {
		t.Fatalf("renewal ends %v, the leaf %v, the old one %v", got.NotAfter, leaf.NotAfter(), end)
	}
	now := certificateFiles(t, w)
	if bytes.Equal(was[paths.LeafCertFile(team)], now[paths.LeafCertFile(team)]) || !sameFiles(was, now, paths.CACertFile(team), paths.CAKeyFile(team)) {
		t.Fatal("the leaf was kept, or the CA changed")
	}
	if !slices.Equal(w.sd.started, []string{paths.TLSUnit(team)}) {
		t.Fatalf("units restarted: %v", w.sd.started)
	}
	leafModes(t, w)
	w.clock.now, w.log = w.clock.now.Add(time.Minute), nil
	again, err := renewer(w).Renew(t.Context(), team, running(w.clock.now.Add(-30*time.Second)))
	if err != nil || again.Made || again.Restarted || restartsOf(w) != 0 {
		t.Fatalf("the next renewal: %+v, %v, %d restarts", again, err, restartsOf(w))
	}
}

// X31: a changed name set makes the leaf again at any age.
func TestARenewalForOtherNamesMakesTheLeafAtAnyAge(t *testing.T) {
	w := applied(t)
	r := renewer(w)
	r.Names = func(paths.TeamID) (pki.Names, error) { return pki.Names{IPs: addrs("192.0.2.10", "192.0.2.11")}, nil }
	w.clock.now = w.clock.now.Add(day)
	got, err := r.Renew(t.Context(), ops(t), running(t0))
	if err != nil || !got.Made || got.Why != "leaf made again: its names changed" {
		t.Fatalf("renewal %+v, %v", got, err)
	}
	if names := theLeaf(t, w).Names(); !sameNames(names, []string{"192.0.2.10", "192.0.2.11"}, "") {
		t.Fatalf("names %+v", names)
	}
}

// X31: a CA key the renewal cannot read is an error naming the file, with the old leaf's end, no
// restart and no file changed, every time it is tried.
func TestARenewalThatCannotReadTheCAKeyChangesNothing(t *testing.T) {
	w := applied(t)
	team := ops(t)
	end := theLeaf(t, w).NotAfter()
	deleteAt(t, w, paths.CAKeyFile(team))
	leafFiles := func() string { return w.read(paths.LeafCertFile(team)) + w.read(paths.LeafKeyFile(team)) }
	was := leafFiles()
	w.clock.now = end.Add(-31 * day)
	if got, err := renewer(w).Renew(t.Context(), team, running(t0)); err != nil || got.Made {
		t.Fatalf("a leaf that is kept needs no CA key: %+v, %v", got, err)
	}
	w.clock.now, w.log = end.Add(-29*day), nil
	for range 2 {
		got, err := renewer(w).Renew(t.Context(), team, running(t0))
		if err == nil || !strings.Contains(err.Error(), paths.CAKeyFile(team)) {
			t.Fatalf("error %v, want one naming the CA key", err)
		}
		if got.Made || got.Restarted || !got.NotAfter.Equal(end) || restartsOf(w) != 0 {
			t.Fatalf("renewal %+v, %d restarts", got, restartsOf(w))
		}
	}
	if leafFiles() != was {
		t.Fatal("the leaf changed")
	}
}

// X31: a terminator that started before the leaf on disk was made serves the leaf it read at its
// start, so it is restarted even when nothing is made: a restart that failed after the leaf was
// written is tried again.
func TestARestartThatFailedIsTriedAgain(t *testing.T) {
	w := applied(t)
	team := ops(t)
	end := theLeaf(t, w).NotAfter()
	w.clock.now, w.log = end.Add(-29*day), nil
	w.faults["restart"] = errFault
	got, err := renewer(w).Renew(t.Context(), team, running(t0))
	if err == nil || !got.Made || got.Restarted {
		t.Fatalf("renewal %+v, %v; want the leaf made and the restart failed", got, err)
	}
	delete(w.faults, "restart")
	w.clock.now, w.log = w.clock.now.Add(time.Minute), nil
	again, err := renewer(w).Renew(t.Context(), team, running(t0))
	if err != nil || again.Made || !again.Restarted || restartsOf(w) != 1 {
		t.Fatalf("the next renewal: %+v, %v, %d restarts", again, err, restartsOf(w))
	}
}

// X31: a terminator that is not active is not started or restarted by a renewal: it reads the
// new leaf at its next start, and the tick reports it as X18c's problem.
func TestARenewalRestartsNoInactiveTerminator(t *testing.T) {
	w := applied(t)
	w.clock.now, w.log = theLeaf(t, w).NotAfter().Add(-29*day), nil
	got, err := renewer(w).Renew(t.Context(), ops(t), Terminator{})
	if err != nil || !got.Made || got.Restarted || restartsOf(w) != 0 || w.called("start") {
		t.Fatalf("renewal %+v, %v, log %v", got, err, w.log)
	}
}

// X31: a leaf past its end is made again, as one near it is.
func TestARenewalMakesAnExpiredLeafAgain(t *testing.T) {
	w := applied(t)
	end := theLeaf(t, w).NotAfter()
	w.clock.now = end.Add(day)
	got, err := renewer(w).Renew(t.Context(), ops(t), running(t0))
	if err != nil || !got.Made || !got.NotAfter.After(w.clock.now) {
		t.Fatalf("renewal %+v, %v", got, err)
	}
}
