package lxc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestfacts"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/unit"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

var (
	errFault = errors.New("planted fault")
	// errStall is a guest call abandoned at its bound, as guestroot answers it.
	errStall = fmt.Errorf("%w: /x did not answer within 10s", guestroot.ErrStalled)
)

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
	writeTree(t, w.host, map[string]string{paths.LXCDesiredCopy(mustBox(t, c), keyOf(t, "t1"))[1:]: "{not json"})
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
				hasLine(t, r, lineOf(t, "SKIPPED", "c1", "offline: container stopped; guest files pending"))
			} else {
				failedAt(t, err, tc.step)
			}
			if e := w.entry("c1"); e.Status != lxcstate.StatusLeft {
				t.Fatalf("entry %+v", e)
			}
		})
	}
}

func TestAbsentWrongUIDFailsTheMember(t *testing.T) {
	w := leaving(t)
	writeTree(t, w.lxc.boxes["c1"].dir, map[string]string{"etc/passwd": "root:x:0:0:root:/root:/bin/bash\nagent1:x:1000:1000::/srv/agent1:/bin/bash\n"})
	_, err := w.apply(PhaseAbsent)
	failedAt(t, err, "reads")
}

func TestAbsentDeletedUserIsCleanedUp(t *testing.T) {
	w := leaving(t)
	b := w.lxc.boxes["c1"]
	writeTree(t, b.dir, map[string]string{"etc/passwd": "root:x:0:0:root:/root:/bin/bash\n"})
	if _, err := w.apply(PhaseAbsent); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(w.lxc.verbs(), "c1 guest membership") || slices.Contains(w.lxc.verbs(), "c1 guest remove") {
		t.Fatalf("attached as a deleted user: %v", w.lxc.verbs())
	}
	if _, ok := guestFile(t, b, unitFileOf(t, "t1")); ok {
		t.Fatal("a deleted user's unit was kept")
	}
	if _, found, err := lxcstate.ReadCopy(w.host, mustBox(t, "c1"), seatID(t, "t1")); err != nil || found {
		t.Fatalf("a deleted user's copy was kept: %v %v", found, err)
	}
	if _, found := w.record().Find(mustBox(t, "c1"), checkout, 1); found {
		t.Fatal("a deleted user's entry was kept")
	}
	if !slices.Contains(w.roster.parks, "t1 "+firstHandle) {
		t.Fatalf("parks %v", w.roster.parks)
	}
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
	hasLine(t, r, lineOf(t, "CHANGED", "c1", "")+lineNoHandle)
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
	hasLine(t, r, lineOf(t, "CHANGED", "c1", "parked; guest files pending (offline: container stopped)"))
}

// lxc-ls or lxc-info failing is the host's fault, never an offline container: the leaver fails,
// its park already recorded, and the guest is not touched.
func TestAbsentLXCToolsFailingFailTheLeaver(t *testing.T) {
	for name, plant := range map[string]func(w *world){
		"lxc-ls":   func(w *world) { w.lxc.listErr = errFault },
		"lxc-info": func(w *world) { w.lxc.boxes["c1"].stateErr = errFault },
	} {
		t.Run(name, func(t *testing.T) {
			w := leaving(t)
			plant(w)
			r, err := w.apply(PhaseAbsent)
			failedAt(t, err, "state")
			if !errors.Is(err, errFault) || len(r.Lines) != 0 || len(w.lxc.calls) != 0 {
				t.Fatalf("err %v, lines %v, calls %v", err, lines(r), w.lxc.verbs())
			}
			if e := w.entry("c1"); e.Status != lxcstate.StatusLeft || e.Handle != firstHandle {
				t.Fatalf("entry %+v", e)
			}
		})
	}
}

// The present phase: lxc-info failing for one container fails its seats, not as offline, and
// the other container's seats still apply.
func TestPresentLXCInfoFailingFailsThatContainer(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1").stateErr = errFault
	w.addBox("c2")
	w.declare([]string{"t1"}, w.member("t1", "c1"), w.memberAt("t1", "c2", 2))
	r, err := w.apply(PhasePresent)
	failedAt(t, err, "state")
	if off, ok := errors.AsType[*OfflineError](err); ok && off != nil {
		t.Fatalf("an lxc-info fault was offline: %v", err)
	}
	hasLine(t, r, lineAt(t, "CHANGED", "t1", "agent1", "c2", 2, "joined as repo.2+local.lxc"))
	if _, found := w.record().Find(mustBox(t, "c1"), checkout, 1); found {
		t.Fatal("a seat whose container state was not known was recorded")
	}
}

// After a guest op fails, lxc-info failing too is not a stopped container: the seat fails.
func TestGoneAskedOfAFailingLXCInfoIsNotOffline(t *testing.T) {
	w := newWorld(t)
	b := w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.lxc.answer = func(c guestCall) error {
		if c.Verb == "guest place-bundle" {
			b.stateErr = errFault
			return &lxccmd.AttachError{Op: c.Verb, Exit: -1}
		}
		return nil
	}
	_, err := w.apply(PhasePresent)
	failedAt(t, err, "guest place-bundle")
}

// The L3 wait: a probe error in a running container fails the leaver, as does lxc-info failing
// after one; only an answered stop is offline.
func TestAbsentWaitProbeFailures(t *testing.T) {
	for _, probeErr := range []error{errFault, errStall} {
		w := leaving(t)
		w.lxc.boxes["c1"].probeErr = probeErr
		_, err := w.apply(PhaseAbsent)
		failedAt(t, err, "wait")
		if slices.Contains(w.lxc.verbs(), "c1 guest remove") {
			t.Fatalf("%v: a seat whose lock could not be probed was removed", probeErr)
		}
	}

	w := leaving(t)
	b := w.lxc.boxes["c1"]
	holdSeat(t, b, `{"v":1,"run":"`+runID+`","started_ms":1}`)
	w.clock.onSleep = func() { b.stateErr = errFault }
	_, err := w.apply(PhaseAbsent)
	if !errors.Is(err, errFault) {
		t.Fatalf("err %v", err)
	}
}

// A seat whose layout cannot be made is an error of the wait, never a free seat.
func TestWaitFreeLayoutError(t *testing.T) {
	w := leaving(t)
	r := &run{d: w.deps()}
	m := &member{team: w.team("t1"), key: keyOf(t, "t1"), acct: guestfacts.Account{Home: "relative"}}
	wait, err := r.waitFree(t.Context(), &guestBox{name: mustBox(t, "c1")}, m)
	if err == nil {
		t.Fatalf("a layout error was a free seat: %+v", wait)
	}
}

// guestErrno is a call below the guest root failing with errno, as guestroot answers it: a
// guest user's FUSE mount answering host root (R2-1).
func guestErrno(p string, errno syscall.Errno) error {
	return fmt.Errorf("%w: guestroot: %s: %w", guestroot.ErrGuest, p, errno)
}

// guestErrnos are errnos a guest's FUSE server can answer, those that look like the host out of
// descriptors or memory included (C1).
var guestErrnos = []syscall.Errno{syscall.EACCES, syscall.EIO, syscall.EMFILE, syscall.ENFILE, syscall.ENOMEM}

// R2-1: a guest user's run root that cannot be listed, whatever the errno, keeps every digest
// and fails no seat; the kit sweep goes on for the rest.
func TestKitSweepListingFaultKeepsEveryDigest(t *testing.T) {
	for _, errno := range guestErrnos {
		w := newWorld(t)
		b := w.addBox("c1")
		dir := oldKit(t, b)
		root, err := paths.GuestLoginRunRoot(agent2UID)
		if err != nil {
			t.Fatal(err)
		}
		sessionPIDUnder(t, b, root, recordedTicks+1)
		b.faults = map[string]error{root: guestErrno(root, errno)}
		w.declare([]string{"t1"}, w.member("t1", "c1"))
		if _, err := w.apply(PhasePresent); err != nil {
			t.Fatalf("%v: %v", errno, err)
		}
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("%v: the old kit was removed: %v", errno, err)
		}
		if e := w.entry("c1"); e.Status != lxcstate.StatusJoined {
			t.Fatalf("%v: entry %+v", errno, e)
		}
	}
}

// R2-1: a checkout the guest answers with an errno fails the seats on it alone, as refused; a
// seat of the same container on another checkout applies.
func TestCheckoutReadFaultFailsThatSeatAlone(t *testing.T) {
	const other = "/srv/agent2/src/repo"
	for _, errno := range guestErrnos {
		w := newWorld(t)
		b := w.addBox("c1")
		writeTree(t, b.dir, map[string]string{
			other[1:] + gitConfig: "[remote \"origin\"]\n\turl = https://github.com/example/repo.git\n",
		})
		b.faults = map[string]error{checkout + gitConfig: guestErrno(checkout+gitConfig, errno)}
		second := w.memberAt("t1", "c1", 2)
		second.User, second.Checkout = agent2(t), other
		w.declare([]string{"t1"}, w.member("t1", "c1"), second)
		_, err := w.apply(PhasePresent)
		herr, ok := errors.AsType[*host.Error](err)
		if !ok || !herr.Drift || herr.Step != stepReads {
			t.Fatalf("%v: err %v, want seat 1 refused at the reads", errno, err)
		}
		if e, found := w.record().Find(mustBox(t, "c1"), other, 2); !found || e.Status != lxcstate.StatusJoined {
			t.Fatalf("%v: the other seat's entry %+v %v", errno, e, found)
		}
	}
}

// R2-3 with C2: a stall is waited out once per seat and run, and fails that seat alone. agent1's
// seat 1 on a stalled checkout fails at the reads after one bound; agent1's seat 2 on a healthy
// checkout of the same container, and agent2's seat on another, wait nothing and apply.
func TestAStallFailsOnlyItsSeat(t *testing.T) {
	const (
		healthy = "/srv/agent1/src/zrepo"
		other   = "/srv/agent2/src/repo"
	)
	w := newWorld(t)
	b := w.addBox("c1")
	origin := "[remote \"origin\"]\n\turl = https://github.com/example/repo.git\n"
	writeTree(t, b.dir, map[string]string{healthy[1:] + gitConfig: origin, other[1:] + gitConfig: origin})
	b.stalls = &stallLog{paths: map[string]bool{checkout + gitConfig: true}, stalled: map[*guestroot.Memo]bool{}}
	second := w.memberAt("t1", "c1", 2)
	second.Checkout = healthy
	third := w.memberAt("t1", "c1", 3)
	third.User, third.Checkout = agent2(t), other
	w.declare([]string{"t1"}, w.member("t1", "c1"), second, third)
	_, err := w.apply(PhasePresent)
	if !errors.Is(err, guestroot.ErrStalled) {
		t.Fatalf("err %v", err)
	}
	if b.stalls.waits != 1 {
		t.Fatalf("the stall was waited out %d times", b.stalls.waits)
	}
	if e, found := w.entryAt("c1", 1); found && e.Status == lxcstate.StatusJoined {
		t.Fatalf("the seat on the stalled checkout joined: %+v", e)
	}
	for _, s := range []struct {
		checkout string
		seat     int
	}{{healthy, 2}, {other, 3}} {
		if e, found := w.record().Find(mustBox(t, "c1"), s.checkout, s.seat); !found || e.Status != lxcstate.StatusJoined {
			t.Fatalf("seat %d on %s: %+v %v", s.seat, s.checkout, e, found)
		}
	}
}

// R2-1 with R2-2: a lock the guest answers with an errno is a probe that failed, never a free
// seat.
func TestLockProbeErrnoIsNotFree(t *testing.T) {
	for _, errno := range guestErrnos {
		w := leaving(t)
		w.lxc.boxes["c1"].probeErr = guestErrno("/lock", errno)
		_, err := w.apply(PhaseAbsent)
		failedAt(t, err, "wait")
		if slices.Contains(w.lxc.verbs(), "c1 guest remove") {
			t.Fatalf("%v: a seat whose lock could not be probed was removed", errno)
		}

		w = placed(t)
		g := w.faulty(func(r Root) Root { return faultyRoot{Root: r, lockErr: guestErrno("/lock", errno)} })
		if got, err := g.Probe(t.Context(), mustTeam(t, "t1"), firstHandle); err == nil {
			t.Fatalf("%v: probe %+v, want an error", errno, got)
		}
	}
}

// N1: a tmux the guest answers with a stall or an errno keeps that error, the member's; only
// a tmux that is not there, or not a regular file, sends the operator to the container's play.
func TestTmuxFaultKeepsItsCause(t *testing.T) {
	r, err := guestroot.OpenDir(newGuest(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, cause := range []error{errStall, guestErrno(unit.Tmux, syscall.EIO)} {
		err := tmux(fakeRoot{Root: r, faults: map[string]error{unit.Tmux: cause}})
		if !errors.Is(err, cause) || !refused(err) || strings.Contains(err.Error(), "installs it") {
			t.Errorf("%v: %v", cause, err)
		}
	}
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
	hasLine(t, r, lineOf(t, "SKIPPED", "c1", "")+whyUnitDown)
	if e := w.entry("c1"); e.Status != lxcstate.StatusOffline || e.Why != whyUnitDown {
		t.Fatalf("entry %+v", e)
	}
	w.busErr = nil
	if _, err = w.apply(PhasePresent); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.bus.calls, []string{"Reload", "Start " + unitOf(t, "t1")}) {
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
