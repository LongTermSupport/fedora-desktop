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
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// restart is `lxc restart` of agent1 in c1 over the world.
func (w *world) restart() error {
	w.t.Helper()
	return Restart(context.Background(), w.deps(), mustBox(w.t, "c1"), agent1(w.t))
}

func TestRestartResetsThenRestartsTheUnit(t *testing.T) {
	w := placed(t)
	if err := w.restart(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.bus.calls, []string{"ResetFailed "+unitOf(t, "t1"), "Restart "+unitOf(t, "t1")}) {
		t.Fatalf("systemd %v", w.bus.calls)
	}
	if len(w.lxc.calls) != 0 || len(w.roster.joins) != 0 {
		t.Fatalf("restart attached %v or joined %v", w.lxc.verbs(), w.roster.joins)
	}
}

func TestRestartOfflineTargetIs69(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(w *world)
		why   string
	}{
		{"stopped", func(w *world) { w.lxc.boxes["c1"].state = lxccmd.Stopped }, whyStopped},
		{"frozen", func(w *world) { w.lxc.boxes["c1"].state = lxccmd.Frozen }, whyStopped},
		{"missing", func(w *world) { delete(w.lxc.boxes, "c1") }, whyMissing},
		{"stopped before the pin", func(w *world) { w.pinErr = guestroot.ErrMoved }, whyStoppedRestart},
		{"systemd gone during the restart", func(w *world) { w.bus.err = guestbus.ErrGone }, whyStoppedRestart},
		{"systemd unreachable", func(w *world) { w.busErr = errFault }, whyBusDown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := placed(t)
			tc.plant(w)
			err := w.restart()
			off, ok := errors.AsType[*OfflineError](err)
			if !ok || off.Container.String() != "c1" || off.Why != tc.why {
				t.Fatalf("err %v, want offline %q", err, tc.why)
			}
		})
	}
}

func TestRestartWithNoUnitIsRefusedByName(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	err := w.restart()
	herr, ok := errors.AsType[*host.Error](err)
	if !ok || !herr.Drift || herr.Verb != restartVerb || herr.Step != "unit" {
		t.Fatalf("err %v", err)
	}
	if len(w.bus.calls) != 0 {
		t.Fatalf("systemd called for a member with no unit: %v", w.bus.calls)
	}
}

func TestRestartFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(w *world)
		step  string
		drift bool
	}{
		{"listing fails", func(w *world) { w.lxc.listErr = errFault }, "state", false},
		{"pin fails", func(w *world) { w.pinErr = errFault }, "pin", false},
		{"systemd refuses", func(w *world) { w.bus.err = guestbus.ErrSystemd }, "restart", false},
		{"the socket is refused", func(w *world) { w.busErr = guestroot.ErrRefused }, "restart", true},
		{"the unit is a link", func(w *world) {
			unitFile := filepath.Join(w.lxc.boxes["c1"].dir, paths.GuestUnitFile(agent1(w.t)))
			if err := os.Remove(unitFile); err != nil {
				w.t.Fatal(err)
			}
			if err := os.Symlink("/etc/passwd", unitFile); err != nil {
				w.t.Fatal(err)
			}
		}, "unit", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := placed(t)
			tc.plant(w)
			err := w.restart()
			herr, ok := errors.AsType[*host.Error](err)
			if !ok || herr.Step != tc.step || herr.Drift != tc.drift || herr.Verb != restartVerb {
				t.Fatalf("err %#v, want step %s drift %v", err, tc.step, tc.drift)
			}
		})
	}
}
