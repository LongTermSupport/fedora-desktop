package lxc

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// started is the record of started seats under the world's host root.
func (w *world) started() lxcstate.Started {
	w.t.Helper()
	s, err := lxcstate.ReadStarted(w.host)
	if err != nil {
		w.t.Fatal(err)
	}
	return s
}

func (w *world) stop() error {
	w.t.Helper()
	return Stop(context.Background(), w.deps(), mustBox(w.t, "c1"), keyOf(w.t, "t1"))
}

func (w *world) reconcile(only ...string) []string {
	w.t.Helper()
	var box *paths.ContainerName
	if len(only) == 1 {
		c := mustBox(w.t, only[0])
		box = &c
	}
	got, err := Reconcile(context.Background(), w.deps(), box)
	if err != nil {
		w.t.Fatal(err)
	}
	out := make([]string, 0, len(got))
	for _, l := range got {
		out = append(out, l.String())
	}
	return out
}

// theSeat is the placed world's one seat, agent1's in c1 of t1.
func theSeat(t *testing.T) lxcstate.StartedSeat {
	t.Helper()
	return lxcstate.StartedSeat{Container: mustBox(t, "c1"), Key: keyOf(t, "t1")}
}

// startedPlaced is a placed world whose seat was started by `lxc start`, with the calls cleared.
func startedPlaced(t *testing.T) *world {
	t.Helper()
	w := placed(t)
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	w.lxc.calls, w.bus.calls, w.log = nil, nil, nil
	return w
}

// C.2: a start records its seat; a refused one records nothing.
func TestStartRecordsTheSeatItStarted(t *testing.T) {
	w := placed(t)
	w.lxc.boxes["c1"].state = lxccmd.Stopped
	if err := w.start(); err == nil || w.started().Has(theSeat(t)) {
		t.Fatalf("offline start: %v, recorded %+v", err, w.started())
	}
	w.lxc.boxes["c1"].state = lxccmd.Running
	if err := w.start(); err != nil {
		t.Fatal(err)
	}
	if got := w.started().Seats; !slices.Equal(got, []lxcstate.StartedSeat{theSeat(t)}) {
		t.Fatalf("started %+v", got)
	}
}

// C.2: `lxc stop` stops the seat's unit, with no lookup of claude, and takes it out of the record.
func TestStopStopsTheUnitAndForgetsTheSeat(t *testing.T) {
	w := startedPlaced(t)
	if err := w.stop(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.bus.calls, []string{"Stop " + unitOf(t, "t1")}) || len(w.lxc.calls) != 0 {
		t.Fatalf("systemd %v, attached %v", w.bus.calls, w.lxc.verbs())
	}
	if w.started().Has(theSeat(t)) {
		t.Fatal("the stopped seat is still recorded")
	}
}

// A stop of a seat whose container is stopped still forgets it, and says the container is
// offline (69), as every verb on one seat does.
func TestStopOfAStoppedContainerForgetsTheSeat(t *testing.T) {
	w := startedPlaced(t)
	w.lxc.boxes["c1"].state = lxccmd.Stopped
	if off, ok := errors.AsType[*OfflineError](w.stop()); !ok || off.Why != whyStopped {
		t.Fatalf("not offline: %v", off)
	}
	if w.started().Has(theSeat(t)) {
		t.Fatal("still recorded")
	}
}

// C.2 case 1: a recorded seat whose container runs and whose session does not is started.
func TestReconcileStartsASeatThatIsDown(t *testing.T) {
	w := startedPlaced(t)
	got := w.reconcile()
	if !slices.Equal(got, []string{"STARTED\tc1/" + keyOf(t, "t1").String() + "\t"}) {
		t.Fatalf("lines %q", got)
	}
	if !slices.Equal(w.bus.calls, []string{"ResetFailed " + unitOf(t, "t1"), "Start " + unitOf(t, "t1")}) {
		t.Fatalf("systemd %v", w.bus.calls)
	}
}

// C.2 case 2: a live session is left alone.
func TestReconcileLeavesALiveSessionAlone(t *testing.T) {
	w := startedPlaced(t)
	holdSeat(t, w.lxc.boxes["c1"], lockRecord)
	got := w.reconcile()
	if !slices.Equal(got, []string{"LIVE\tc1/" + keyOf(t, "t1").String() + "\t"}) || len(w.bus.calls) != 0 || len(w.lxc.calls) != 0 {
		t.Fatalf("lines %q, systemd %v, attached %v", got, w.bus.calls, w.lxc.verbs())
	}
}

// C.2 case 3, and the record kept across a container stop: a stopped container is reported and
// never started, its seat stays recorded, and it is started once the container runs again.
func TestReconcileSkipsAStoppedContainerAndKeepsItsSeat(t *testing.T) {
	w := startedPlaced(t)
	w.lxc.boxes["c1"].state = lxccmd.Stopped
	got := w.reconcile()
	if !slices.Equal(got, []string{"STOPPED\tc1/" + keyOf(t, "t1").String() + "\t" + whyStopped}) || len(w.bus.calls) != 0 {
		t.Fatalf("lines %q, systemd %v", got, w.bus.calls)
	}
	if !w.started().Has(theSeat(t)) {
		t.Fatal("the seat was forgotten")
	}
	w.lxc.boxes["c1"].state = lxccmd.Running
	if got := w.reconcile(); len(got) != 1 || got[0][:8] != "STARTED\t" {
		t.Fatalf("after the container is back: %q", got)
	}
}

// C.2 case 4, and a refused start's exit: the start's refusal is the seat's line with its
// cause, and Refused counts it, so the verb exits non-zero.
func TestReconcileReportsARefusedStart(t *testing.T) {
	w := startedPlaced(t)
	w.lxc.answer = func(guestCall) error {
		return &lxccmd.GuestError{Op: "guest find-claude", Container: "c1", Exit: 78, Code: exits.CodeClaude}
	}
	got, err := Reconcile(context.Background(), w.deps(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != ReconcileRefused || Refused(got) != 1 || len(w.bus.calls) != 0 {
		t.Fatalf("lines %+v, systemd %v", got, w.bus.calls)
	}
	if !w.started().Has(theSeat(t)) {
		t.Fatal("a refused seat was forgotten")
	}
	if Refused([]ReconcileLine{{Kind: ReconcileStarted}, {Kind: ReconcileLive}, {Kind: ReconcileStopped}}) != 0 {
		t.Fatal("only a refusal counts")
	}
}

// A recorded seat no team's record holds any more is refused by name, not dropped.
func TestReconcileRefusesASeatNoRecordHolds(t *testing.T) {
	w := placed(t)
	stray := lxcstate.StartedSeat{Container: mustBox(t, "c1"), Key: keyOf(t, "t2")}
	if err := lxcstate.WriteStarted(w.host, lxcstate.Started{}.Add(stray)); err != nil {
		t.Fatal(err)
	}
	got, err := Reconcile(context.Background(), w.deps(), nil)
	if err != nil || len(got) != 1 || got[0].Kind != ReconcileRefused || got[0].Why == "" {
		t.Fatalf("%+v %v", got, err)
	}
}

// --container limits the run to that container's seats; a missing container is stopped too.
func TestReconcileContainerLimitsTheRun(t *testing.T) {
	w := startedPlaced(t)
	other := lxcstate.StartedSeat{Container: mustBox(t, "c2"), Key: keyOf(t, "t1")}
	if err := lxcstate.WriteStarted(w.host, w.started().Add(other)); err != nil {
		t.Fatal(err)
	}
	if got := w.reconcile("c2"); !slices.Equal(got, []string{"STOPPED\tc2/" + keyOf(t, "t1").String() + "\t" + whyMissing}) {
		t.Fatalf("c2 only: %q", got)
	}
	if len(w.bus.calls) != 0 {
		t.Fatalf("c1 was touched: %v", w.bus.calls)
	}
	if got := w.reconcile(); len(got) != 2 {
		t.Fatalf("every seat: %q", got)
	}
}

// No record is nothing to do; an unreadable one, or a failing container list, is the host's
// fault.
func TestReconcileFaults(t *testing.T) {
	w := placed(t)
	if got := w.reconcile(); len(got) != 0 {
		t.Fatalf("%q", got)
	}
	w = startedPlaced(t)
	w.lxc.listErr = errFault
	if _, err := Reconcile(context.Background(), w.deps(), nil); !errors.Is(err, errFault) {
		t.Fatalf("list: %v", err)
	}
	w = startedPlaced(t)
	w.lxc.boxes["c1"].stateErr = errFault
	got, err := Reconcile(context.Background(), w.deps(), nil)
	if err != nil || len(got) != 1 || got[0].Kind != ReconcileRefused {
		t.Fatalf("state: %+v %v", got, err)
	}
}

// C.2: a seat that leaves its team (`lxc apply --phase absent`) is a stop on purpose, so it is
// taken out of the record of started seats, and reconcile never reports a seat that cannot exist.
func TestAbsentForgetsTheStartedSeat(t *testing.T) {
	w := startedPlaced(t)
	m := w.member("t1", "c1")
	m.State = declare.StateAbsent
	w.declare([]string{"t1"}, m)
	if _, err := w.apply(PhaseAbsent); err != nil {
		t.Fatal(err)
	}
	if w.started().Has(theSeat(t)) {
		t.Fatal("the seat that left is still recorded")
	}
	if got := w.reconcile(); len(got) != 0 {
		t.Fatalf("reconcile after the leave: %q", got)
	}
}

// A leaving seat whose container is stopped is forgotten too: the leave is on purpose either way.
func TestAbsentForgetsTheStartedSeatOfAStoppedContainer(t *testing.T) {
	w := startedPlaced(t)
	w.lxc.boxes["c1"].state = lxccmd.Stopped
	m := w.member("t1", "c1")
	m.State = declare.StateAbsent
	w.declare([]string{"t1"}, m)
	if _, err := w.apply(PhaseAbsent); err != nil {
		t.Fatal(err)
	}
	if w.started().Has(theSeat(t)) {
		t.Fatal("the seat that left is still recorded")
	}
}
