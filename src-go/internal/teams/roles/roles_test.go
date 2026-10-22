package roles

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/playlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

const (
	lxcHandle  = "proj.1+worker.lxc"
	ccyHandle  = "proj.3+local.podman"
	setRole    = teamtest.SudoBus + "set-role alpha "
	probeOfA   = `{"probe":"a"}`
	probeOfB   = `{"probe":"b"}`
	identProj  = "IDENTITY\tproj\tworker\n"
	identOther = "IDENTITY\tother\tworker\n"
)

// entryA and entryB are two present worker entries of team alpha that leave host and seat out.
var (
	entryA = declare.LxcMember{Team: "alpha", Container: "container-a", User: "agent", Role: "worker", State: "present", Checkout: "/srv/agent/one"}
	entryB = declare.LxcMember{Team: "alpha", Container: "container-b", User: "agent", Role: "worker", State: "present", Checkout: "/srv/agent/two"}
)

// reader is the host-side reader of a container's checkout, by container.
type reader struct {
	probes  map[string]string
	errs    map[string]error
	reads   []string
	running []string
	stopped []string
	listErr error
}

func (r *reader) Containers(context.Context) ([]string, []string, error) {
	return r.running, r.stopped, r.listErr
}

func (r *reader) ReadCheckout(_ context.Context, script, mode, container, user, path string) (string, error) {
	r.reads = append(r.reads, strings.Join([]string{script, mode, container, user, path}, " "))
	if err := r.errs[container]; err != nil {
		return "", err
	}
	return r.probes[container], nil
}

type rig struct {
	*teamtest.World

	s       *Setter
	r       *reader
	applied []string
	said    []string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	w := teamtest.Populated(t)
	g := &rig{World: w, r: &reader{probes: map[string]string{"container-a": probeOfA, "container-b": probeOfB}}}
	g.s = New(w.Host, flow.New(w.Host), func(_ context.Context, teams ...string) error {
		g.applied = append(g.applied, teams...)
		return nil
	}, nil)
	g.s.Reader = g.r
	w.On(setRole, "", 0)
	w.OnDeclare("lxc-script", "THE-READER")
	w.OnDeclare("lxc-role", "CHANGED\tLXC member\n")
	g.identities(map[string]string{probeOfA: identProj, probeOfB: identOther})
	return g
}

// identities scripts lxc-identity's answer for each probe.
func (g *rig) identities(by map[string]string) {
	g.Fake.OnFunc(strings.Fields(g.DeclarePrefix("lxc-identity")), func(c execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: []byte(by[c.Args[len(c.Args)-1]])}, nil
	})
}

func (g *rig) say(line string) { g.said = append(g.said, line) }

// changes are the role changes run, in order: "set-role <handle> <flag>" and
// "lxc-role <container> <role>".
func (g *rig) changes() []string {
	var got []string
	for _, c := range g.Fake.Calls() {
		line := c.Line()
		switch {
		case strings.HasPrefix(line, setRole):
			got = append(got, "set-role "+strings.TrimPrefix(line, setRole))
		case strings.HasPrefix(line, g.DeclarePrefix("lxc-role")):
			got = append(got, "lxc-role "+c.Args[len(c.Args)-5]+" "+c.Args[len(c.Args)-1])
		}
	}
	return got
}

func TestAMemberWhoseRoleIsNotDeclaredIsChangedLiveAndNothingIsRead(t *testing.T) {
	g := newRig(t)
	// The entries are read in-process: a file the reader refuses proves they were not read, as
	// a Set that read them would fail.
	g.WriteHostVars(t, teamtest.HostVarsWithLxc(declare.LxcMember{Team: "alpha", Container: "container-a", User: "agent", State: "gone", Checkout: "/srv/agent/one"}))
	done, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: ccyHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err != nil || done.Declared {
		t.Fatalf("%+v %v", done, err)
	}
	if got := g.changes(); !slices.Equal(got, []string{"set-role " + ccyHandle + " --role=coordinator"}) {
		t.Fatalf("changes %q", got)
	}
	if len(g.r.reads) != 0 {
		t.Fatal("a ccy member's declaration was looked for")
	}
	if !slices.Equal(g.said, []string{ccyHandle + " set to coordinator"}) {
		t.Fatalf("said %q", g.said)
	}
}

func TestADeclaredLxcMemberHasItsEntryChangedAsWellAsItsLiveRole(t *testing.T) {
	g := newRig(t)
	g.WriteHostVars(t, teamtest.HostVarsWithLxc(entryA, entryB))
	done, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err != nil || !done.Declared {
		t.Fatalf("%+v %v", done, err)
	}
	want := []string{"lxc-role container-a coordinator", "set-role " + lxcHandle + " --role=coordinator"}
	if got := g.changes(); !slices.Equal(got, want) {
		t.Fatalf("changes %q, want %q", got, want)
	}
	if !slices.Contains(g.r.reads, "THE-READER probe container-a agent /srv/agent/one") {
		t.Fatalf("reads %q", g.r.reads)
	}
	if len(g.applied) != 0 {
		t.Fatalf("Set applied %v; only Change applies", g.applied)
	}
}

func TestAnLxcMemberNoEntryDeclaresIsChangedLive(t *testing.T) {
	otherTeam, absent, otherHost, otherSeat := entryA, entryA, entryA, entryA
	otherTeam.Team, absent.State, otherHost.Host, otherSeat.Seat = "beta", "absent", "desk", "dev"
	for name, members := range map[string][]declare.LxcMember{
		"no entries":               nil,
		"another team's entry":     {otherTeam},
		"an absent entry":          {absent},
		"an entry of another repo": {entryB},
		"an entry of another host": {otherHost},
		"an entry of another seat": {otherSeat},
	} {
		g := newRig(t)
		g.WriteHostVars(t, teamtest.HostVarsWithLxc(members...))
		done, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
		if err != nil || done.Declared {
			t.Fatalf("%s: %+v %v", name, done, err)
		}
		if got := g.changes(); !slices.Equal(got, []string{"set-role " + lxcHandle + " --role=coordinator"}) {
			t.Fatalf("%s: changes %q", name, got)
		}
	}
}

func TestAnEntryThatCannotMatchTheHandleIsNotRead(t *testing.T) {
	g := newRig(t)
	onDesk, onDev := entryA, entryB
	onDesk.Host, onDev.Seat = "desk", "dev"
	g.WriteHostVars(t, teamtest.HostVarsWithLxc(onDesk, onDev))
	if _, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleWorker}); err != nil {
		t.Fatal(err)
	}
	if len(g.r.reads) != 0 {
		t.Fatalf("reads %q", g.r.reads)
	}
}

// With no LXC member record, nothing says which entry a handle is but the checkout: one that
// cannot be read stops the change before anything, and the way on is the apply that writes the
// record, never starting the container.
func TestAnEntryWhoseCheckoutCannotBeReadWithNoRecordStopsTheChangeBeforeAnything(t *testing.T) {
	g := newRig(t)
	g.WriteHostVars(t, teamtest.HostVarsWithLxc(entryA))
	g.r.errs = map[string]error{"container-a": errors.New("/srv/agent/one of agent@container-a could not be read from the host: not running")}
	_, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err == nil || !strings.Contains(err.Error(), "not running") ||
		!strings.Contains(err.Error(), "the entry of agent@container-a blocks the change of "+lxcHandle) ||
		!strings.Contains(err.Error(), "ccy-teams apply alpha records which entry each member is") ||
		!strings.Contains(err.Error(), "give each of team alpha's entries its seat") ||
		!strings.Contains(err.Error(), "no role was changed") || strings.Contains(err.Error(), "Start container") {
		t.Fatalf("err %v", err)
	}
	if got := g.changes(); len(got) != 0 {
		t.Fatalf("changes %q", got)
	}
}

// An entry that gives its seat is still read (its repo is only in its checkout), so the advice
// to give each entry its seat could not help and is not given.
func TestAnEntryThatGivesItsSeatAndCannotBeReadIsNotToldToGiveItsSeat(t *testing.T) {
	g := newRig(t)
	seated := entryA
	seated.Seat = "1"
	g.WriteHostVars(t, teamtest.HostVarsWithLxc(seated))
	g.r.errs = map[string]error{"container-a": errors.New("not running")}
	_, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err == nil || !strings.Contains(err.Error(), "ccy-teams apply alpha records which entry each member is") ||
		strings.Contains(err.Error(), "Start container") ||
		strings.Contains(err.Error(), "its seat") || strings.Contains(err.Error(), "gives no seat") {
		t.Fatalf("err %v", err)
	}
	if got := g.changes(); len(got) != 0 {
		t.Fatalf("changes %q", got)
	}
}

// record writes team alpha's LXC member record, the play's account of each entry's member.
func (g *rig) record(t *testing.T, items ...string) {
	t.Helper()
	teamtest.MustWrite(t, filepath.Join(g.Host.AppliedDir, "lxc", "alpha.json"),
		`{"version": 1, "team": "alpha", "members": [`+strings.Join(items, ", ")+`]}`)
}

func item(container, status, handle string) string {
	return `{"container": "` + container + `", "user": "agent", "state": "present", "status": "` + status +
		`", "handle": "` + handle + `", "checkout": "/srv/agent", "why": "container not running", "since": 1}`
}

// An offline member is normal lifecycle (CLAUDE/AgentNotes.md, "An offline team member never
// blocks anything"): the record names the entry's handle, so a stopped container is not read
// and its role is changed in its entry and live.
func TestAnEntryTheRecordNamesIsJudgedByItsHandleWithNoRead(t *testing.T) {
	g := newRig(t)
	g.WriteHostVars(t, teamtest.HostVarsWithLxc(entryA, entryB))
	g.record(t, item("container-a", "stopped", lxcHandle), item("container-b", "joined", "other.1+worker.lxc"))
	g.r.errs = map[string]error{"container-a": errors.New("not running"), "container-b": errors.New("not running")}
	done, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err != nil || !done.Declared {
		t.Fatalf("%+v %v", done, err)
	}
	want := []string{"lxc-role container-a coordinator", "set-role " + lxcHandle + " --role=coordinator"}
	if got := g.changes(); !slices.Equal(got, want) {
		t.Fatalf("changes %q, want %q", got, want)
	}
	if len(g.r.reads) != 0 {
		t.Fatalf("reads %q", g.r.reads)
	}
}

// A deferred entry with no handle never joined, so it is not the member: passed over, named
// offline, with no read; the entry that is the member is still found by its checkout.
func TestAnEntryTheRecordDeferredUnjoinedIsPassedOverAsOffline(t *testing.T) {
	g := newRig(t)
	g.WriteHostVars(t, teamtest.HostVarsWithLxc(entryB, entryA))
	g.record(t, item("container-b", "deferred", ""))
	g.r.errs = map[string]error{"container-b": errors.New("not running")}
	done, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err != nil || !done.Declared {
		t.Fatalf("%+v %v", done, err)
	}
	if got := g.changes(); !slices.Contains(got, "lxc-role container-a coordinator") {
		t.Fatalf("changes %q", got)
	}
	if slices.ContainsFunc(g.r.reads, func(r string) bool { return strings.Contains(r, "container-b") }) {
		t.Fatalf("the deferred entry was read: %q", g.r.reads)
	}
	if !slices.ContainsFunc(g.said, func(s string) bool { return strings.HasPrefix(s, "agent@container-b is offline") }) {
		t.Fatalf("said %q", g.said)
	}
}

// With a record, an entry it does not name was not joined by the last apply, so it is not the
// member: a checkout that cannot be read passes it over and the change goes on. It is named
// offline only when the container list says its container is stopped.
func TestAnEntryTheRecordDoesNotNameAndCannotBeReadIsPassedOverAsOffline(t *testing.T) {
	g := newRig(t)
	g.WriteHostVars(t, teamtest.HostVarsWithLxc(entryA))
	g.record(t, item("container-z", "joined", "other.1+worker.lxc"))
	g.r.errs = map[string]error{"container-a": errors.New("not running")}
	g.r.stopped = []string{"container-a"}
	done, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err != nil || done.Declared {
		t.Fatalf("%+v %v", done, err)
	}
	if got := g.changes(); !slices.Equal(got, []string{"set-role " + lxcHandle + " --role=coordinator"}) {
		t.Fatalf("changes %q", got)
	}
	if !slices.ContainsFunc(g.said, func(s string) bool {
		return strings.HasPrefix(s, "agent@container-a is offline (container container-a stopped") && strings.Contains(s, "not running")
	}) {
		t.Fatalf("said %q", g.said)
	}
}

// A checkout that cannot be read in a container that runs is not offline: it is passed over
// all the same (the last apply did not join it), and said as a failed read, never as offline.
func TestAnUnreadableCheckoutInARunningContainerIsNotCalledOffline(t *testing.T) {
	for name, set := range map[string]func(r *reader){
		"running":           func(r *reader) { r.running = []string{"container-a"} },
		"no container list": func(r *reader) { r.listErr = errors.New("lxc-ls failed") },
	} {
		t.Run(name, func(t *testing.T) {
			g := newRig(t)
			g.WriteHostVars(t, teamtest.HostVarsWithLxc(entryA))
			g.record(t, item("container-z", "joined", "other.1+worker.lxc"))
			g.r.errs = map[string]error{"container-a": errors.New("timed out")}
			set(g.r)
			done, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
			if err != nil || done.Declared {
				t.Fatalf("%+v %v", done, err)
			}
			if slices.ContainsFunc(g.said, func(s string) bool { return strings.Contains(s, "offline") }) {
				t.Fatalf("called offline: %q", g.said)
			}
			if !slices.ContainsFunc(g.said, func(s string) bool {
				return strings.HasPrefix(s, "agent@container-a: its checkout could not be read (timed out)")
			}) {
				t.Fatalf("said %q", g.said)
			}
		})
	}
}

func TestTwoEntriesThatGiveTheHandleAreRefused(t *testing.T) {
	g := newRig(t)
	g.WriteHostVars(t, teamtest.HostVarsWithLxc(entryA, entryB))
	g.identities(map[string]string{probeOfA: identProj, probeOfB: identProj})
	_, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err == nil || !strings.Contains(err.Error(), "agent@container-a") || !strings.Contains(err.Error(), "agent@container-b") {
		t.Fatalf("err %v", err)
	}
	if got := g.changes(); len(got) != 0 {
		t.Fatalf("changes %q", got)
	}
}

func TestAStepGivenItsEntryWritesTheEntryAloneWhenTheMemberHasNoHandle(t *testing.T) {
	g := newRig(t)
	entry := declare.LxcMember{Team: "alpha", Container: "container-b", User: "agent", Role: agentbus.RoleCoordinator}
	done, err := g.s.Set(t.Context(), "alpha", g.say, Step{Entry: &entry, From: agentbus.RoleCoordinator, To: agentbus.RoleWorker})
	if err != nil || !done.Declared {
		t.Fatalf("%+v %v", done, err)
	}
	if got := g.changes(); !slices.Equal(got, []string{"lxc-role container-b worker"}) {
		t.Fatalf("changes %q", got)
	}
}

func TestATakeoverWhoseSecondStepFailsPutsTheFirstBack(t *testing.T) {
	g := newRig(t)
	g.WriteHostVars(t, teamtest.HostVarsWithLxc(entryA))
	g.OnErr(setRole+ccyHandle+" --role=coordinator", "agent-bus: refused\n", 1)
	_, err := g.s.Set(t.Context(), "alpha", g.say,
		Step{Handle: lxcHandle, From: agentbus.RoleCoordinator, To: agentbus.RoleWorker, Note: " (it was alpha's coordinator)"},
		Step{Handle: ccyHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err == nil || !strings.Contains(err.Error(), ccyHandle+" was not made a coordinator") {
		t.Fatalf("err %v", err)
	}
	want := []string{
		"lxc-role container-a worker", "set-role " + lxcHandle + " --role=worker",
		"set-role " + ccyHandle + " --role=coordinator",
		"set-role " + lxcHandle + " --role=coordinator", "lxc-role container-a coordinator",
	}
	if got := g.changes(); !slices.Equal(got, want) {
		t.Fatalf("changes %q,\nwant %q", got, want)
	}
	if !slices.Contains(g.said, lxcHandle+" set to worker (it was alpha's coordinator)") {
		t.Fatalf("said %q", g.said)
	}
}

func TestUndoPutsEveryStepBackInReverse(t *testing.T) {
	g := newRig(t)
	done, err := g.s.Set(t.Context(), "alpha", g.say,
		Step{Handle: "other.5+local.podman", From: agentbus.RoleCoordinator, To: agentbus.RoleWorker},
		Step{Handle: ccyHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err != nil {
		t.Fatal(err)
	}
	if err = done.Undo(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"set-role other.5+local.podman --role=worker", "set-role " + ccyHandle + " --role=coordinator",
		"set-role " + ccyHandle + " --role=worker", "set-role other.5+local.podman --role=coordinator",
	}
	if got := g.changes(); !slices.Equal(got, want) {
		t.Fatalf("changes %q", got)
	}
}

func TestChangeAppliesTheTeamOnlyWhenAnEntryWasWritten(t *testing.T) {
	g := newRig(t)
	if _, err := g.s.Change(t.Context(), "alpha", g.say, Step{Handle: ccyHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator}); err != nil {
		t.Fatal(err)
	}
	if len(g.applied) != 0 {
		t.Fatalf("applied %v", g.applied)
	}
	g.WriteHostVars(t, teamtest.HostVarsWithLxc(entryA))
	done, err := g.s.Change(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(g.applied, []string{"alpha"}) {
		t.Fatalf("applied %v", g.applied)
	}
	if err = done.Undo(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(g.applied, []string{"alpha", "alpha"}) || !slices.Contains(g.changes(), "lxc-role container-a worker") {
		t.Fatalf("an undone entry was not applied again: applied %v changes %q", g.applied, g.changes())
	}
}

// locker is ops.Locked as the test sees it: whether a hold is open, how many were taken, and
// refused, the wait stopped before the lock was taken (fn never runs).
type locker struct {
	holding  bool
	holds    int
	refused  error
	purposes []string
}

func (l *locker) locked(ctx context.Context, purpose string, fn func(context.Context) error) error {
	l.purposes = append(l.purposes, purpose)
	if l.refused != nil {
		return l.refused
	}
	l.holds++
	l.holding = true
	defer func() { l.holding = false }()
	return fn(ctx)
}

func TestADeclaredChangeWritesTheEntryTheLiveRoleAndAppliesInOneHoldOfThePlayLock(t *testing.T) {
	g := newRig(t)
	g.WriteHostVars(t, teamtest.HostVarsWithLxc(entryA))
	l := &locker{}
	g.s.Locked = l.locked
	var inHold []string
	g.Fake.OnFunc(strings.Fields(g.DeclarePrefix("lxc-role")), func(execx.Cmd) (execx.Result, error) {
		inHold = append(inHold, "lxc-role "+map[bool]string{true: "held", false: "free"}[l.holding])
		return execx.Result{Stdout: []byte("CHANGED\tLXC member\n")}, nil
	})
	g.Fake.OnFunc(strings.Fields(setRole), func(execx.Cmd) (execx.Result, error) {
		inHold = append(inHold, "set-role "+map[bool]string{true: "held", false: "free"}[l.holding])
		return execx.Result{}, nil
	})
	g.s.Apply = func(context.Context, ...string) error {
		inHold = append(inHold, "apply "+map[bool]string{true: "held", false: "free"}[l.holding])
		return nil
	}
	if _, err := g.s.Change(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"lxc-role held", "set-role held", "apply held"}; l.holds != 1 || !slices.Equal(inHold, want) {
		t.Fatalf("holds %d, steps %q, want %q in one hold", l.holds, inHold, want)
	}
}

func TestAStoppedWaitForThePlayLockChangesNothing(t *testing.T) {
	g := newRig(t)
	g.WriteHostVars(t, teamtest.HostVarsWithLxc(entryA))
	stopped := fmt.Errorf("the play lock was not taken: %w", playlock.ErrStopped)
	g.s.Locked = (&locker{refused: stopped}).locked
	_, err := g.s.Change(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if !errors.Is(err, playlock.ErrStopped) || !strings.Contains(err.Error(), "no role was changed") {
		t.Fatalf("err %v", err)
	}
	if got := g.changes(); len(got) != 0 || len(g.applied) != 0 {
		t.Fatalf("a stopped wait changed %q, applied %v", got, g.applied)
	}
}

func TestALiveOnlyChangeTakesNoPlayLock(t *testing.T) {
	g := newRig(t)
	l := &locker{}
	g.s.Locked = l.locked
	if _, err := g.s.Change(t.Context(), "alpha", g.say, Step{Handle: ccyHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator}); err != nil {
		t.Fatal(err)
	}
	if len(l.purposes) != 0 {
		t.Fatalf("a change with no entry to write waited for the play lock: %q", l.purposes)
	}
}

func TestAFailedApplySaysTheRoleIsChangedAndHowToFinish(t *testing.T) {
	g := newRig(t)
	g.WriteHostVars(t, teamtest.HostVarsWithLxc(entryA))
	g.s.Apply = func(context.Context, ...string) error { return errors.New("play failed") }
	_, err := g.s.Change(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err == nil || !strings.Contains(err.Error(), "ccy-teams apply alpha") || !strings.Contains(err.Error(), lxcHandle+" is a coordinator now") {
		t.Fatalf("err %v", err)
	}
}

func TestAHandleIsOneOfAKnownKind(t *testing.T) {
	for _, word := range []string{lxcHandle, ccyHandle, "proj.2+desk.host", "a.b+c.docker", "a.dev+lab.vm"} {
		if !IsHandle(word) {
			t.Errorf("%s is a handle", word)
		}
	}
	for _, word := range []string{"ccy-proj", "proj.1+worker", "proj+worker.lxc", "proj.1+worker.boat", ""} {
		if IsHandle(word) {
			t.Errorf("%s is not a handle", word)
		}
	}
}

func TestALiveChangeThatFailsIsReportedAsRootsAndNamesTheMember(t *testing.T) {
	g := newRig(t)
	g.OnErr(setRole+ccyHandle, "agent-bus: no such member\n", 1)
	_, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: ccyHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err == nil || err.Error() != ccyHandle+" was not made a coordinator" {
		t.Fatalf("err %v", err)
	}
	if !strings.Contains(g.ErrOut.String(), "no such member") {
		t.Fatalf("stderr %q", g.ErrOut.String())
	}
}
