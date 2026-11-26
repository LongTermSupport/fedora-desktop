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
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/guest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
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
	if !slices.Equal(w.lxc.verbs(), []string{"c1 guest find-claude"}) || len(w.roster.joins) != 0 {
		t.Fatalf("start attached %v or joined %v", w.lxc.verbs(), w.roster.joins)
	}
}

// R77: each start asks the seat user's login shell where claude and node are, writes their
// directories first in the seat's environment file, then starts the unit.
func TestStartWritesThePathItFoundBeforeTheStart(t *testing.T) {
	w := placed(t)
	w.lxc.found[guest.ProgramNode] = "/opt/node/bin/node"
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	env, ok := guestFile(t, w.lxc.boxes["c1"], paths.GuestSessionEnvFile(keyOf(t, "t1")))
	want := "PATH=" + nvmBin + ":/opt/node/bin:/srv/agent1/.local/bin:/usr/local/bin:/usr/bin:/bin\n"
	if !ok || env != want {
		t.Fatalf("env file %q, want %q", env, want)
	}
	if !slices.Equal(w.log, []string{"guest find-claude", "systemd ResetFailed " + unitOf(t, "t1"), "systemd Start " + unitOf(t, "t1")}) {
		t.Fatalf("order %v", w.log)
	}
	if c := w.lxc.calls[0]; c.User != "agent1" || len(c.Args) != 0 || c.Stdin != "" {
		t.Fatalf("find ran as %+v", c)
	}
}

// One directory for both is named once; a claude with no node is its directory alone.
func TestStartPathNamesEachDirectoryOnce(t *testing.T) {
	w := placed(t)
	delete(w.lxc.found, guest.ProgramNode)
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	env, _ := guestFile(t, w.lxc.boxes["c1"], paths.GuestSessionEnvFile(keyOf(t, "t1")))
	if env != "PATH="+nvmBin+":/srv/agent1/.local/bin:/usr/local/bin:/usr/bin:/bin\n" {
		t.Fatalf("env %q", env)
	}
}

// R77: a lookup that finds no claude refuses the start by name, starts nothing and writes no
// environment file; so does a guest without the host's kit.
func TestStartRefusesWhenTheLookupFindsNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(w *world)
		step  string
	}{
		{"no claude", func(w *world) {
			w.lxc.answer = func(guestCall) error {
				return &lxccmd.GuestError{Op: "guest find-claude", Container: "c1", Exit: 78, Code: exits.CodeClaude}
			}
		}, "claude"},
		{"no kit", func(w *world) {
			if err := os.RemoveAll(filepath.Join(w.lxc.boxes["c1"].dir, paths.HostKitRoot, kitDigest)); err != nil {
				w.t.Fatal(err)
			}
		}, "kit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := placed(t)
			tc.plant(w)
			err := w.start()
			herr, ok := errors.AsType[*host.Error](err)
			if !ok || !herr.Drift || herr.Verb != startVerb || herr.Step != tc.step {
				t.Fatalf("err %v", err)
			}
			if len(w.bus.calls) != 0 {
				t.Fatalf("systemd called: %v", w.bus.calls)
			}
			if _, ok := guestFile(t, w.lxc.boxes["c1"], paths.GuestSessionEnvFile(keyOf(t, "t1"))); ok {
				t.Fatal("an environment file was written")
			}
		})
	}
}

// A restart is a start too: it writes the path it found before restarting the unit.
func TestRestartWritesThePathItFound(t *testing.T) {
	w := placed(t)
	if err := w.restart(); err != nil {
		t.Fatal(err)
	}
	if _, ok := guestFile(t, w.lxc.boxes["c1"], paths.GuestSessionEnvFile(keyOf(t, "t1"))); !ok {
		t.Fatal("no environment file")
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
