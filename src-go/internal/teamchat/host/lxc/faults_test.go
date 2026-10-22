package lxc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

var errFault = errors.New("planted fault")

// failedAt checks that err is a host.Error of step.
func failedAt(t *testing.T, err error, step string) {
	t.Helper()
	herr, ok := errors.AsType[*host.Error](err)
	if !ok || herr.Step != step {
		t.Fatalf("err %v, want a failure of step %s", err, step)
	}
}

// breakCopy plants a host copy of agent1 in c that does not decode.
func breakCopy(t *testing.T, w *world, c string) {
	t.Helper()
	writeTree(t, w.host, map[string]string{paths.LXCDesiredCopy(mustBox(t, c), agent1(t))[1:]: "{not json"})
}

type failingApprovals struct{}

func (failingApprovals) Load(paths.TeamID) (*declare.Gated, error) { return nil, errFault }

func TestHostFaultErrorWraps(t *testing.T) {
	err := &hostFaultError{errFault}
	if err.Error() != errFault.Error() || !errors.Is(err, errFault) {
		t.Fatalf("err %v", err)
	}
}

func TestValidateFaultsFailThePhase(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	d := w.deps()
	d.Approvals = failingApprovals{}
	_, err := Apply(context.Background(), d, Request{Phase: PhasePresent, Decl: w.decl})
	failedAt(t, err, stepValidate)
	d = w.deps()
	d.Origin = func(context.Context, paths.TeamID) (string, error) { return "", errFault }
	_, err = Apply(context.Background(), d, Request{Phase: PhasePresent, Decl: w.decl})
	failedAt(t, err, stepValidate)
	if w.lxc.states != 0 {
		t.Fatal("a container was asked its state before validation passed")
	}
}

func TestContainerListFailingFailsThePresentPhase(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.lxc.listErr = errFault
	_, err := w.apply(PhasePresent)
	failedAt(t, err, "state")
}

func TestRosterFailingFailsTheTeamsMembers(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.roster.membersErr = errFault
	_, err := w.apply(PhasePresent)
	failedAt(t, err, "roster")
	if len(w.roster.joins) != 0 {
		t.Fatalf("joined without a roster: %v", w.roster.joins)
	}
}

func TestJoinAnsweringAnotherHandleFailsTheMember(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.roster.rename = "other.1+local.lxc"
	_, err := w.apply(PhasePresent)
	failedAt(t, err, "roster")

	w = placed(t)
	w.roster.roster[mustTeam(t, "t1")][firstHandle] = apply.MemberParked
	w.roster.rename = "repo.2+local.lxc"
	_, err = w.apply(PhasePresent)
	failedAt(t, err, "roster")
	if e := w.entry("c1"); e.Handle != firstHandle {
		t.Fatalf("entry %+v", e)
	}
}

func TestPlaceBundleFailingInARunningContainerFailsTheMember(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.lxc.answer = func(c guestCall) error {
		if c.Verb == "guest place-bundle" {
			return &lxccmd.GuestError{Op: c.Verb, Exit: 1, Code: "io"}
		}
		return nil
	}
	_, err := w.apply(PhasePresent)
	failedAt(t, err, "guest place-bundle")
	if e := w.entry("c1"); e.Status != lxcstate.StatusDeferred || e.Handle != firstHandle {
		t.Fatalf("a joined handle was not kept for a reissue: %+v", e)
	}
}

func TestUnreadableHostCopyFailsTheMembership(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	breakCopy(t, w, "c1")
	_, err := w.apply(PhasePresent)
	failedAt(t, err, stepMembership)
	if slices.Contains(w.lxc.verbs(), "c1 guest membership") {
		t.Fatal("guest membership ran without the host copy")
	}
}

func TestAbsentParkFailingFailsTheMember(t *testing.T) {
	w := leaving(t)
	w.roster.parkErr = errFault
	_, err := w.apply(PhaseAbsent)
	failedAt(t, err, "park")
	if e := w.entry("c1"); e.Status != lxcstate.StatusJoined {
		t.Fatalf("a member whose park failed changed its entry: %+v", e)
	}
}

func TestAbsentUnreadableHostCopyFailsTheMember(t *testing.T) {
	w := leaving(t)
	breakCopy(t, w, "c1")
	_, err := w.apply(PhaseAbsent)
	failedAt(t, err, stepMembership)
}

func TestAbsentGuestStepFailures(t *testing.T) {
	for _, tc := range []struct {
		name, verb string
		err        error
		stop       bool
		step       string
	}{
		{"guest remove fails", "guest remove", &lxccmd.GuestError{Op: "guest remove", Exit: 1, Code: "io"}, false, "guest remove"},
		{"guest membership fails", "guest membership", &lxccmd.GuestError{Op: "guest membership", Exit: 1, Code: "io"}, false, stepMembership},
		{"the container stops during membership", "guest membership", &lxccmd.AttachError{Op: "guest membership", Exit: -1}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := leaving(t)
			b := w.lxc.boxes["c1"]
			w.lxc.answer = func(c guestCall) error {
				if c.Verb != tc.verb {
					return nil
				}
				if tc.stop {
					b.state = lxccmd.Stopped
				}
				return tc.err
			}
			r, err := w.apply(PhaseAbsent)
			if tc.step == "" {
				if err != nil {
					t.Fatal(err)
				}
				hasLine(t, r, lineOf(t, "SKIPPED", "t1", "c1", "offline: container stopped; guest files pending"))
			} else {
				failedAt(t, err, tc.step)
			}
			if e := w.entry("c1"); e.Status != lxcstate.StatusLeft {
				t.Fatalf("entry %+v", e)
			}
		})
	}
}

func TestAbsentWrongPasswdFailsTheMember(t *testing.T) {
	w := leaving(t)
	writeTree(t, w.lxc.boxes["c1"].dir, map[string]string{"etc/passwd": "root:x:0:0:root:/root:/bin/bash\n"})
	_, err := w.apply(PhaseAbsent)
	failedAt(t, err, "reads")
}

func TestAbsentMemberThatNeverJoinedIsDropped(t *testing.T) {
	w := newWorld(t)
	b := w.addBox("c1")
	b.state = lxccmd.Stopped
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	if _, err := w.apply(PhasePresent); err != nil {
		t.Fatal(err)
	}
	b.state = lxccmd.Running
	m := w.member("t1", "c1")
	m.State = declare.StateAbsent
	w.declare([]string{"t1"}, m)
	r, err := w.apply(PhaseAbsent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "CHANGED", "t1", "c1", "")+lineNoHandle)
	if _, found := w.record().Find(mustBox(t, "c1"), checkout, 1); found {
		t.Fatal("a member that never joined kept its entry")
	}
	if len(w.roster.parks) != 0 || len(w.lxc.calls) != 0 {
		t.Fatalf("parked %v or attached %v for a member with no handle", w.roster.parks, w.lxc.verbs())
	}
}

func TestAbsentOfflineTargetIs69(t *testing.T) {
	w := leaving(t)
	w.lxc.boxes["c1"].state = lxccmd.Stopped
	w.members = []Target{{Container: mustBox(t, "c1"), Key: keyOf(t, "t1")}}
	r, err := w.apply(PhaseAbsent)
	if off, ok := errors.AsType[*OfflineError](err); !ok || off.Container.String() != "c1" {
		t.Fatalf("err %v", err)
	}
	hasLine(t, r, lineOf(t, "CHANGED", "t1", "c1", "parked; guest files pending (offline: container stopped)"))
}

func TestAbsentContainerListFailingIsOffline(t *testing.T) {
	w := leaving(t)
	w.lxc.listErr = errFault
	r, err := w.apply(PhaseAbsent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "CHANGED", "t1", "c1", "parked; guest files pending (offline: container stopped)"))
}

func TestHostKitWithAFifoIsRefused(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	if err := syscall.Mkfifo(filepath.Join(w.host, "kit/bin/pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	_, err := w.apply(PhasePresent)
	failedAt(t, err, "kit")
	if _, statErr := os.Stat(filepath.Join(w.lxc.boxes["c1"].dir, paths.HostKitRoot, kitDigest)); statErr == nil {
		t.Fatal("a kit was placed from a host kit holding a FIFO")
	}
}

func TestUnreachableSystemdIsTheUnitDown(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.busErr = errFault
	r, err := w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "SKIPPED", "t1", "c1", "")+whyUnitDown)
	if e := w.entry("c1"); e.Status != lxcstate.StatusOffline || e.Why != whyUnitDown {
		t.Fatalf("entry %+v", e)
	}
	w.busErr = nil
	if _, err = w.apply(PhasePresent); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.bus.calls, []string{"Reload", "Start "+unitOf(t, "t1")}) {
		t.Fatalf("an unchanged unit last found down was not started: %v", w.bus.calls)
	}
}

func TestRefusedSystemdSocketFailsTheMember(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.busErr = guestroot.ErrRefused
	_, err := w.apply(PhasePresent)
	failedAt(t, err, "unit")
}
