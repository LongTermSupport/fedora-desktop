package lxc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
)

// start is `lxc start` of agent1 in c1 over the world.
func (w *world) start() error {
	w.t.Helper()
	return Start(context.Background(), w.deps(), mustBox(w.t, "c1"), keyOf(w.t, "t1"))
}

// SU4: the unit is reset, then started, never restarted: systemd's StartUnit leaves a unit that
// already runs as it is, so a running session is never ended by a start.
func TestStartResetsThenStartsTheUnitNeverRestarts(t *testing.T) {
	w := placed(t)
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.bus.calls, []string{"ResetFailed " + unitOf(t, "t1"), "Start " + unitOf(t, "t1")}) {
		t.Fatalf("systemd %v", w.bus.calls)
	}
	if len(w.lxc.calls) != 0 || len(w.roster.joins) != 0 {
		t.Fatalf("start attached %v or joined %v", w.lxc.verbs(), w.roster.joins)
	}
}

// SU4: an offline target is 69 with the start's own words.
func TestStartOfflineTargetIs69(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(w *world)
		why   string
	}{
		{"stopped", func(w *world) { w.lxc.boxes["c1"].state = lxccmd.Stopped }, whyStopped},
		{"missing", func(w *world) { delete(w.lxc.boxes, "c1") }, whyMissing},
		{"systemd gone during the start", func(w *world) { w.bus.err = guestbus.ErrGone }, whyStoppedStart},
		{"systemd unreachable", func(w *world) { w.busErr = errFault }, whyBusDown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := placed(t)
			tc.plant(w)
			err := w.start()
			off, ok := errors.AsType[*OfflineError](err)
			if !ok || off.Container.String() != "c1" || off.Why != tc.why {
				t.Fatalf("err %v, want offline %q", err, tc.why)
			}
		})
	}
}

// SU4: a seat with no unit, or no record entry, is refused by name under the start's verb; a
// refused systemd call is the start's failure.
func TestStartRefusalsAndFailuresNameTheStart(t *testing.T) {
	w := placed(t)
	if err := os.Remove(filepath.Join(w.lxc.boxes["c1"].dir, unitFileOf(t, "t1"))); err != nil {
		t.Fatal(err)
	}
	herr, ok := errors.AsType[*host.Error](w.start())
	if !ok || !herr.Drift || herr.Verb != startVerb || herr.Step != "unit" || len(w.bus.calls) != 0 {
		t.Fatalf("no unit: %v, systemd %v", herr, w.bus.calls)
	}
	w = newWorld(t)
	w.addBox("c1")
	herr, ok = errors.AsType[*host.Error](w.start())
	if !ok || !herr.Drift || herr.Verb != startVerb || herr.Step != stepRecord {
		t.Fatalf("no record: %v", herr)
	}
	w = placed(t)
	w.bus.err = guestbus.ErrSystemd
	herr, ok = errors.AsType[*host.Error](w.start())
	if !ok || herr.Verb != startVerb || herr.Step != "start" {
		t.Fatalf("refused: %v", herr)
	}
}
