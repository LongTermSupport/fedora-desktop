package teamtest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestPopulatedWorldIsAHostWithTeamAlphaUp(t *testing.T) {
	w := Populated(t)
	teams, states, err := w.Host.Declare.Overview(t.Context(), w.Root, w.Host.AppliedDir, []string{"alpha"})
	if err != nil || len(teams) != 1 || teams[0].Name != "alpha" || len(states) != 1 {
		t.Fatalf("overview %+v %+v %v", teams, states, err)
	}
	units, err := w.Host.Systemd.HomeserverUnits(t.Context())
	if err != nil || len(units) != 1 || units[0].Team != "alpha" {
		t.Fatalf("units %+v %v", units, err)
	}
}

// The populated checkout's seats are real seat folders the in-process reader reads, seat 1
// held by a lock this test process takes, and no process runs to read them.
func TestPopulatedSeatsAreReadFromDiskAndCanBeFreedAndHeld(t *testing.T) {
	w := Populated(t)
	w.Spawned()
	seats, unreadable, err := w.Host.Bus.SeatList(t.Context(), w.Checkout())
	want := []agentbus.Seat{
		{Team: "alpha", Seat: "1", Held: true, Handle: "proj.1+local.podman"},
		{Team: "alpha", Seat: "dev", Held: false, Handle: "proj.dev+local.podman"},
	}
	if err != nil || unreadable || !reflect.DeepEqual(seats, want) {
		t.Fatalf("seats %+v unreadable %v err %v\n%s", seats, unreadable, err, w.ErrOut.String())
	}
	if got := w.Spawned(); len(got) != 0 {
		t.Fatalf("reading seats started %v", got)
	}
	w.HoldSeat(t, w.Checkout(), "alpha", "1", false)
	w.HoldSeat(t, w.Checkout(), "alpha", "dev", true)
	w.HoldSeat(t, w.Checkout(), "alpha", "dev", true)
	seats, _, err = w.Host.Bus.SeatList(t.Context(), w.Checkout())
	if err != nil || seats[0].Held || !seats[1].Held {
		t.Fatalf("after freeing 1 and holding dev %+v %v", seats, err)
	}
}

// A removed seat is gone from the list, and its lock is let go first: a held seat can be removed.
func TestARemovedSeatIsNotListed(t *testing.T) {
	w := Populated(t)
	w.RemoveSeat(t, w.Checkout(), "alpha", "1")
	seats, unreadable, err := w.Host.Bus.SeatList(t.Context(), w.Checkout())
	want := []agentbus.Seat{{Team: "alpha", Seat: "dev", Held: false, Handle: "proj.dev+local.podman"}}
	if err != nil || unreadable || !reflect.DeepEqual(seats, want) {
		t.Fatalf("seats %+v unreadable %v err %v\n%s", seats, unreadable, err, w.ErrOut.String())
	}
	if _, err := os.Stat(SeatDir(w.Checkout(), "alpha", "1")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the seat's folder is still there: %v", err)
	}
}

func TestABrokenSeatIsLeftOutAndNamed(t *testing.T) {
	w := Populated(t)
	w.BreakSeat(t, w.Checkout(), "alpha", "dev")
	seats, unreadable, err := w.Host.Bus.SeatList(t.Context(), w.Checkout())
	if err != nil || !unreadable || len(seats) != 1 || seats[0].Seat != "1" {
		t.Fatalf("seats %+v unreadable %v err %v\n%s", seats, unreadable, err, w.ErrOut.String())
	}
	if !strings.Contains(w.ErrOut.String(), "agent-bus: seat dev@alpha: ") {
		t.Fatalf("stderr %q", w.ErrOut.String())
	}
}

// A seat the reader cannot survive fails the whole list, as v1's seat list crashed on it.
func TestACrashedSeatFailsTheWholeList(t *testing.T) {
	w := Populated(t)
	w.CrashSeat(t, w.Checkout(), "alpha", "dev")
	seats, unreadable, err := w.Host.Bus.SeatList(t.Context(), w.Checkout())
	if err == nil || unreadable || seats != nil {
		t.Fatalf("seats %+v unreadable %v err %v", seats, unreadable, err)
	}
	if !strings.Contains(err.Error(), "agent-bus seat list: ") || !strings.Contains(err.Error(), "urlsplit") {
		t.Fatalf("err %v", err)
	}
}

// The seat list is read in-process, so no process answers it: scripting one would prove nothing.
func TestOnRefusesTheSeatList(t *testing.T) {
	for name, script := range map[string]func(*World){
		"On":    func(w *World) { w.On("agent-bus seat list", "", 0) },
		"OnErr": func(w *World) { w.OnErr("agent-bus  seat list --json", "no checkout", 2) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: the seat list was scripted", name)
				}
			}()
			script(New(t))
		}()
	}
	New(t).On("agent-bus seat choose alpha", "", 0)
}

func TestSpawnedCountsWhatRanSinceTheLastCall(t *testing.T) {
	w := Populated(t)
	if _, err := w.Host.Systemd.HomeserverUnits(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := w.Spawned(); got["systemctl"] != 1 || len(got) != 1 {
		t.Fatalf("first %v", got)
	}
	if got := w.Spawned(); len(got) != 0 {
		t.Fatalf("second %v", got)
	}
}

func TestThePlayLockIsTheWorldsOwnAndCanBeHeldAsAnotherRunHoldsIt(t *testing.T) {
	w := New(t)
	if w.PlayLockHeld(t) {
		t.Fatal("a new world's play lock is held")
	}
	release := w.HoldPlayLock(t)
	if !w.PlayLockHeld(t) {
		t.Fatal("held, it reads free")
	}
	release()
	release()
	if w.PlayLockHeld(t) {
		t.Fatal("released, it reads held")
	}
}

// The overview reads the host_vars in-process and runs declare.py once, for status.
func TestTheOverviewReadsTheHostVarsAndRunsOnlyStatus(t *testing.T) {
	w := New(t)
	w.WriteHostVars(t, HostVarsAlpha)
	w.OnDeclare("status", "STATUS\talpha\tcurrent\t-\n")
	teams, states, err := w.Host.Declare.Overview(t.Context(), w.Root, w.Host.AppliedDir, nil)
	want := []declare.Team{{
		Name: "alpha", Port: 6167, Present: true, ServerName: "alpha.agent-bus.internal", URL: "http://192.0.2.10:6167",
	}}
	if err != nil || !reflect.DeepEqual(teams, want) || len(states) != 1 {
		t.Fatalf("overview %+v %+v %v", teams, states, err)
	}
	if got := w.Spawned(); !reflect.DeepEqual(got, map[string]int{"declare.py status": 1}) {
		t.Fatalf("started %v", got)
	}
}

func TestAScriptedEditLeavesTheFileTheReadsThenAnswerFrom(t *testing.T) {
	w := New(t)
	w.WriteHostVars(t, "agent_bus_teams:\n  - team: alpha\n    port: 6167\n")
	w.OnDeclareEdit("remove", "CHANGED\tteam alpha absent\n", "agent_bus_teams:\n  - team: alpha\n    state: absent\n")
	if change, err := w.Host.Declare.Remove(t.Context(), "alpha", false); err != nil || !change.Changed {
		t.Fatalf("%+v %v", change, err)
	}
	if teams, err := w.Host.Declare.Declared(t.Context()); err != nil || len(teams) != 1 || teams[0].Present {
		t.Fatalf("%+v %v", teams, err)
	}
}

func TestHostVarsWithLxcIsReadBackAsItsMembers(t *testing.T) {
	w := New(t)
	members := []declare.LxcMember{
		{Team: "alpha", Container: "container-a", User: "agent", Host: "worker", Role: "coordinator", State: "present", Checkout: "/srv/agent/one", Seat: "dev"},
		{Team: "alpha", Container: "container-b", User: "agent", Host: "worker", Role: "worker", State: "absent", Checkout: "/srv/agent/two"},
	}
	w.WriteHostVars(t, HostVarsWithLxc(members...))
	if got, err := w.Host.Declare.LxcMembers(t.Context()); err != nil || !reflect.DeepEqual(got, members) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestOnDeclareRefusesARead(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a read was scripted")
		}
	}()
	New(t).OnDeclare("limited", "")
}

func TestOnRefusesADeclareRead(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a read was scripted through On")
		}
	}()
	w := New(t)
	w.On(w.DeclarePrefix("limited"), "", 0)
}

// reported records what refuseInProcessReads reports.
type reported struct{ got []string }

func (r *reported) Helper() {}

func (r *reported) Errorf(format string, args ...any) {
	r.got = append(r.got, fmt.Sprintf(format, args...))
}

// A rule set on the Fake directly is past scriptable; the read running as a process is caught.
func TestAnInProcessReadRunAsAProcessFailsTheTest(t *testing.T) {
	w := New(t)
	w.Fake.On([]string{"agent-bus", "seat", "list"}, execx.Result{})
	w.Fake.On(strings.Fields(w.DeclarePrefix("limited")), execx.Result{})
	w.Fake.On(strings.Fields(w.DeclarePrefix("status")), execx.Result{})
	for _, argv := range [][]string{
		{"agent-bus", "seat", "list", "--json"},
		strings.Fields(w.DeclarePrefix("limited")),
		strings.Fields(w.DeclarePrefix("status")),
	} {
		if _, err := w.Fake.Run(t.Context(), execx.Cmd{Name: argv[0], Args: argv[1:]}); err != nil {
			t.Fatal(err)
		}
	}
	var e reported
	refuseInProcessReads(&e, w.Fake)
	if len(e.got) != 2 || !strings.Contains(e.got[0], "agent-bus seat list --json ran as a process") ||
		!strings.Contains(e.got[1], "declare limited is read in-process") {
		t.Fatalf("reported %q", e.got)
	}
	// The cleanup New registered must not fail this test for the reads it ran on purpose.
	w.Fake = &execx.Fake{}
}

func TestInteractiveKeepsAHostVarsAlreadyWritten(t *testing.T) {
	w := Populated(t)
	Interactive(t, w)
	if teams, err := w.Host.Declare.Declared(t.Context()); err != nil || len(teams) != 1 {
		t.Fatalf("%+v %v", teams, err)
	}
	empty := New(t)
	Interactive(t, empty)
	if limits, err := empty.Host.Declare.Limited(t.Context()); err != nil || len(limits) != 0 {
		t.Fatalf("%+v %v", limits, err)
	}
}
