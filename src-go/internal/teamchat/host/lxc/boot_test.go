package lxc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcset"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// boxes is a world whose containers are in the states given, by name.
func boxes(t *testing.T, states map[string]lxccmd.State) *world {
	t.Helper()
	w := newWorld(t)
	for name, st := range states {
		w.lxc.boxes[name] = &box{state: st, dir: t.TempDir()}
	}
	return w
}

func (w *world) running() []string {
	w.t.Helper()
	got, err := lxcstate.ReadRunning(w.host)
	if err != nil {
		w.t.Fatal(err)
	}
	out := make([]string, 0, len(got))
	for _, c := range got {
		out = append(out, c.String())
	}
	return out
}

func (w *world) recordRunning(names ...string) {
	w.t.Helper()
	in := make([]paths.ContainerName, 0, len(names))
	for _, n := range names {
		in = append(in, mustBox(w.t, n))
	}
	if err := lxcstate.WriteRunning(w.host, in); err != nil {
		w.t.Fatal(err)
	}
}

func lineTexts(lines []BootLine) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, l.String())
	}
	return out
}

// C.2, the shutdown half: the containers running now are recorded, and only those; the record is
// replaced whole, so it holds no container that is stopped now.
func TestRecordRunningRecordsTheRunningContainers(t *testing.T) {
	w := boxes(t, map[string]lxccmd.State{"c1": lxccmd.Running, "c2": lxccmd.Stopped, "c3": lxccmd.Running, "c4": lxccmd.Frozen})
	w.recordRunning("c2")
	got, err := RecordRunning(context.Background(), w.deps())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"c1", "c3"}; !slices.Equal(lineTextsOfNames(got), want) || !slices.Equal(w.running(), want) {
		t.Fatalf("returned %v, recorded %v, want %v", got, w.running(), want)
	}
	if len(w.units.started) != 0 {
		t.Fatalf("a record started %v", w.units.started)
	}
}

func lineTextsOfNames(names []paths.ContainerName) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, n.String())
	}
	return out
}

// No container running is an empty record, not an old one kept.
func TestRecordRunningWithNoneRunningEmptiesTheRecord(t *testing.T) {
	w := boxes(t, map[string]lxccmd.State{"c1": lxccmd.Stopped})
	w.recordRunning("c1")
	if _, err := RecordRunning(context.Background(), w.deps()); err != nil {
		t.Fatal(err)
	}
	if got := w.running(); len(got) != 0 {
		t.Fatalf("recorded %v", got)
	}
}

// A container list or a state that cannot be read fails the record, which is left as it was: a
// half-read host is never written as the truth.
func TestRecordRunningFaultsLeaveTheRecord(t *testing.T) {
	w := boxes(t, map[string]lxccmd.State{"c1": lxccmd.Running})
	w.recordRunning("c9")
	w.lxc.listErr = errFault
	if _, err := RecordRunning(context.Background(), w.deps()); !errors.Is(err, errFault) {
		t.Fatalf("list: %v", err)
	}
	w.lxc.listErr = nil
	w.lxc.boxes["c1"].stateErr = errFault
	if _, err := RecordRunning(context.Background(), w.deps()); !errors.Is(err, errFault) {
		t.Fatalf("state: %v", err)
	}
	if got := w.running(); !slices.Equal(got, []string{"c9"}) {
		t.Fatalf("the record changed: %v", got)
	}
	if err := os.WriteFile(filepath.Join(w.host, paths.LXCRunningFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.lxc.boxes["c1"].stateErr = nil
	if _, err := RecordRunning(context.Background(), w.deps()); !errors.Is(err, lxcstate.ErrInvalid) {
		t.Fatalf("a record that does not decode: %v", err)
	}
}

// C.2, the boot half: exactly the recorded containers are started, each through the host's
// systemd as LXC's own lxc@<container>.service, never by lxc-start from the boot unit (whose
// mount namespace the container would inherit). One already running is left alone, one gone is
// reported, and a container not recorded is never started.
func TestStartRecordedStartsExactlyTheRecordedContainers(t *testing.T) {
	w := boxes(t, map[string]lxccmd.State{"c1": lxccmd.Stopped, "c2": lxccmd.Running, "c4": lxccmd.Stopped})
	w.recordRunning("c1", "c2", "c3")
	got, err := StartRecorded(context.Background(), w.deps())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"STARTED\tc1\t", "RUNNING\tc2\t", "MISSING\tc3\t" + whyMissing}
	if !slices.Equal(lineTexts(got), want) || BootRefusals(got) != 0 {
		t.Fatalf("lines %q, want %q", lineTexts(got), want)
	}
	if !slices.Equal(w.units.started, []string{"lxc@c1.service"}) {
		t.Fatalf("started %v, want lxc@c1.service alone", w.units.started)
	}
}

func runningTexts(lines []lxcset.Line) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, string(l.Kind)+"\t"+l.Container+"\t"+l.Why)
	}
	return out
}

// C.3: the set the record of the last shutdown names, beside the set running now. A recorded
// container is UP when it runs, else DOWN with its state (ABSENT when it is gone); one running
// that the record does not name is UNRECORDED. Nothing is started or recorded.
func TestCheckRunningComparesTheRecordWithTheContainersNow(t *testing.T) {
	w := boxes(t, map[string]lxccmd.State{"c1": lxccmd.Running, "c2": lxccmd.Stopped, "c4": lxccmd.Running, "c5": lxccmd.Frozen})
	w.recordRunning("c1", "c2", "c3")
	got, err := checkRunning(context.Background(), w.deps(), bootVerb)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"UP\tc1\t", "DOWN\tc2\tSTOPPED", "DOWN\tc3\tABSENT", "UNRECORDED\tc4\t"}
	if !slices.Equal(runningTexts(got), want) {
		t.Fatalf("lines %q, want %q", runningTexts(got), want)
	}
	if len(w.units.started) != 0 || !slices.Equal(w.running(), []string{"c1", "c2", "c3"}) {
		t.Fatalf("a check started %v or rewrote the record: %v", w.units.started, w.running())
	}
}

// No record at all is a NO-RECORD line, not an empty set, and the running containers are still
// named; an empty record names only those.
func TestCheckRunningWithNoRecord(t *testing.T) {
	w := boxes(t, map[string]lxccmd.State{"c1": lxccmd.Running})
	got, err := checkRunning(context.Background(), w.deps(), bootVerb)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"NO-RECORD\t\t" + paths.LXCRunningFile, "UNRECORDED\tc1\t"}; !slices.Equal(runningTexts(got), want) {
		t.Fatalf("lines %q, want %q", runningTexts(got), want)
	}
	w.recordRunning()
	got, err = checkRunning(context.Background(), w.deps(), bootVerb)
	if err != nil || !slices.Equal(runningTexts(got), []string{"UNRECORDED\tc1\t"}) {
		t.Fatalf("empty record: %q %v", runningTexts(got), err)
	}
}

// A list, a state or a record that cannot be read fails the check: could not look is never a
// container's state.
func TestCheckRunningFaults(t *testing.T) {
	w := boxes(t, map[string]lxccmd.State{"c1": lxccmd.Running})
	w.recordRunning("c1")
	w.lxc.listErr = errFault
	if _, err := checkRunning(context.Background(), w.deps(), bootVerb); !errors.Is(err, errFault) {
		t.Fatalf("list: %v", err)
	}
	w.lxc.listErr = nil
	w.lxc.boxes["c1"].stateErr = errFault
	if _, err := checkRunning(context.Background(), w.deps(), bootVerb); !errors.Is(err, errFault) {
		t.Fatalf("state: %v", err)
	}
	w.lxc.boxes["c1"].stateErr = nil
	if err := os.WriteFile(filepath.Join(w.host, paths.LXCRunningFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := checkRunning(context.Background(), w.deps(), bootVerb); !errors.Is(err, lxcstate.ErrInvalid) {
		t.Fatalf("a record that does not decode: %v", err)
	}
}

// writeBootID gives the world's host a boot id, as the kernel does.
func (w *world) writeBootID(id string) {
	w.t.Helper()
	path := filepath.Join(w.host, bootIDFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

// C.3: the boot unit's verbs leave the set they found in the status tree, the owner group's to
// read, with this boot's id and the time, so verify-restore reads it without root.
func TestWriteSetStatusRecordsTheSetForTheOwnerGroup(t *testing.T) {
	w := boxes(t, map[string]lxccmd.State{"c1": lxccmd.Running, "c2": lxccmd.Stopped})
	w.recordRunning("c1", "c2")
	w.writeBootID("boot-7")
	if err := WriteSetStatus(context.Background(), w.deps(), bootVerb); err != nil {
		t.Fatal(err)
	}
	got, found, err := lxcset.Read(w.host)
	if err != nil || !found {
		t.Fatalf("%v %v", found, err)
	}
	if got.BootID != "boot-7" || got.WrittenMS != w.clock.Now().UnixMilli() ||
		!slices.Equal(runningTexts(got.Lines), []string{"UP\tc1\t", "DOWN\tc2\tSTOPPED"}) {
		t.Fatalf("%+v", got)
	}
	info, err := os.Stat(filepath.Join(w.host, paths.LXCSetStatusFile))
	if err != nil || info.Mode().Perm() != lxcset.Mode {
		t.Fatalf("%v %v", info, err)
	}
}

// Could not look leaves the last status as it was, and fails: no boot id, or a state that
// cannot be read. The reader then finds a status of another boot, which is a finding.
func TestWriteSetStatusFaultsLeaveTheLastStatus(t *testing.T) {
	w := boxes(t, map[string]lxccmd.State{"c1": lxccmd.Running})
	w.recordRunning("c1")
	if err := WriteSetStatus(context.Background(), w.deps(), bootVerb); err == nil || !strings.Contains(err.Error(), "lxc boot, step set status") {
		t.Fatalf("no boot id: %v", err)
	}
	w.writeBootID("boot-1")
	w.lxc.boxes["c1"].stateErr = errFault
	if err := WriteSetStatus(context.Background(), w.deps(), bootVerb); !errors.Is(err, errFault) {
		t.Fatalf("state: %v", err)
	}
	if _, found, err := lxcset.Read(w.host); found || err != nil {
		t.Fatalf("a status was written: %v %v", found, err)
	}
}

// No record (a host never shut down by the unit) starts nothing.
func TestStartRecordedWithNoRecordStartsNothing(t *testing.T) {
	w := boxes(t, map[string]lxccmd.State{"c1": lxccmd.Stopped})
	got, err := StartRecorded(context.Background(), w.deps())
	if err != nil || len(got) != 0 || len(w.units.started) != 0 {
		t.Fatalf("lines %q, started %v, %v", lineTexts(got), w.units.started, err)
	}
}

// A start or a state that fails is the container's REFUSED line with its cause, counted, and the
// other containers still start; a list or a record that cannot be read is the host's error.
func TestStartRecordedRefusals(t *testing.T) {
	w := boxes(t, map[string]lxccmd.State{"c1": lxccmd.Stopped, "c2": lxccmd.Stopped, "c3": lxccmd.Stopped})
	w.recordRunning("c1", "c2", "c3")
	w.units.err = map[string]error{"lxc@c1.service": errFault}
	w.lxc.boxes["c2"].stateErr = errFault
	got, err := StartRecorded(context.Background(), w.deps())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("lines %q", lineTexts(got))
	}
	kinds := []BootKind{got[0].Kind, got[1].Kind, got[2].Kind}
	if !slices.Equal(kinds, []BootKind{BootRefused, BootRefused, BootStarted}) ||
		got[0].Why == "" || got[1].Why == "" || BootRefusals(got) != 2 {
		t.Fatalf("lines %q", lineTexts(got))
	}
	w.lxc.listErr = errFault
	if _, err := StartRecorded(context.Background(), w.deps()); !errors.Is(err, errFault) {
		t.Fatalf("list: %v", err)
	}
	w.lxc.listErr = nil
	if err := os.WriteFile(filepath.Join(w.host, paths.LXCRunningFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := StartRecorded(context.Background(), w.deps()); !errors.Is(err, lxcstate.ErrInvalid) {
		t.Fatalf("record: %v", err)
	}
}

// A started container is waited for, on the clock, until lxc-info answers RUNNING: the start
// of lxc@<c>.service returns before the container's init runs, and reconcile would report it
// stopped. A container that comes up within the wait is STARTED.
func TestStartRecordedWaitsForRunning(t *testing.T) {
	w := boxes(t, map[string]lxccmd.State{"c1": lxccmd.Stopped})
	w.recordRunning("c1")
	w.units.stays = map[string]bool{"c1": true}
	w.clock.onSleep = func() {
		if w.clock.sleeps == 3 {
			w.lxc.boxes["c1"].state = lxccmd.Running
		}
	}
	got, err := StartRecorded(context.Background(), w.deps())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"STARTED\tc1\t"}; !slices.Equal(lineTexts(got), want) || w.clock.sleeps != 3 {
		t.Fatalf("lines %q after %d sleeps, want %q after 3", lineTexts(got), w.clock.sleeps, want)
	}
}

// A container still not RUNNING when the wait ends is REFUSED, naming the container, after a
// bounded number of waits; the other containers still start.
func TestStartRecordedWaitIsBounded(t *testing.T) {
	w := boxes(t, map[string]lxccmd.State{"c1": lxccmd.Stopped, "c2": lxccmd.Stopped})
	w.recordRunning("c1", "c2")
	w.units.stays = map[string]bool{"c1": true}
	got, err := StartRecorded(context.Background(), w.deps())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Kind != BootRefused || !strings.Contains(got[0].Why, "c1") ||
		!strings.Contains(got[0].Why, string(lxccmd.Stopped)) || got[1].Kind != BootStarted || BootRefusals(got) != 1 {
		t.Fatalf("lines %q", lineTexts(got))
	}
	if bound := int(BootRunningWait / BootRunningGap); w.clock.sleeps < bound || w.clock.sleeps > bound+1 {
		t.Fatalf("%d sleeps, want about %d", w.clock.sleeps, bound)
	}
}

// A context cut short during the wait is the container's REFUSED line, not a hang.
func TestStartRecordedWaitEndsWithItsContext(t *testing.T) {
	w := boxes(t, map[string]lxccmd.State{"c1": lxccmd.Stopped})
	w.recordRunning("c1")
	w.units.stays = map[string]bool{"c1": true}
	ctx, cancel := context.WithCancel(context.Background())
	w.clock.onSleep = cancel
	got, err := StartRecorded(ctx, w.deps())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != BootRefused || !strings.Contains(got[0].Why, context.Canceled.Error()) {
		t.Fatalf("lines %q", lineTexts(got))
	}
}

// The unit is built only from a name the container grammar accepts: nothing that would change
// the unit systemd starts ('@', '/', '\', a space, a leading dash or dot) reaches it.
func TestContainerUnitName(t *testing.T) {
	if got, err := containerUnit("box-1.a_b"); err != nil || got != "lxc@box-1.a_b.service" {
		t.Fatalf("containerUnit = %q, %v", got, err)
	}
	for _, bad := range []string{"", "a@b", "a/b", `a\x2fb`, "a b", "-a", ".a", "a.service@x", strings.Repeat("a", 65)} {
		if got, err := containerUnit(bad); err == nil {
			t.Errorf("containerUnit(%q) = %q, want a refusal", bad, got)
		}
	}
}
