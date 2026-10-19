package roles

import (
	"context"
	"errors"
	"fmt"
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
	entryA     = "LXCMEMBER\talpha\tcontainer-a\tagent\t-\tworker\tpresent\t/srv/agent/one\t-\n"
	entryB     = "LXCMEMBER\talpha\tcontainer-b\tagent\t-\tworker\tpresent\t/srv/agent/two\t-\n"
	probeOfA   = `{"probe":"a"}`
	probeOfB   = `{"probe":"b"}`
	identProj  = "IDENTITY\tproj\tworker\n"
	identOther = "IDENTITY\tother\tworker\n"
)

// reader is the host-side reader of a container's checkout, by container.
type reader struct {
	probes map[string]string
	errs   map[string]error
	reads  []string
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
	w.OnDeclare("lxc-members", "")
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
	done, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: ccyHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err != nil || done.Declared {
		t.Fatalf("%+v %v", done, err)
	}
	if got := g.changes(); !slices.Equal(got, []string{"set-role " + ccyHandle + " --role=coordinator"}) {
		t.Fatalf("changes %q", got)
	}
	if g.Fake.Called(g.DeclarePrefix("lxc-members")) || len(g.r.reads) != 0 {
		t.Fatal("a ccy member's declaration was looked for")
	}
	if !slices.Equal(g.said, []string{ccyHandle + " set to coordinator"}) {
		t.Fatalf("said %q", g.said)
	}
}

func TestADeclaredLxcMemberHasItsEntryChangedAsWellAsItsLiveRole(t *testing.T) {
	g := newRig(t)
	g.OnDeclare("lxc-members", entryA+entryB)
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
	for name, members := range map[string]string{
		"no entries":               "",
		"another team's entry":     strings.Replace(entryA, "alpha", "beta", 1),
		"an absent entry":          strings.Replace(entryA, "present", "absent", 1),
		"an entry of another repo": entryB,
		"an entry of another host": "LXCMEMBER\talpha\tcontainer-a\tagent\tdesk\tworker\tpresent\t/srv/agent/one\t-\n",
		"an entry of another seat": "LXCMEMBER\talpha\tcontainer-a\tagent\t-\tworker\tpresent\t/srv/agent/one\tdev\n",
	} {
		g := newRig(t)
		g.OnDeclare("lxc-members", members)
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
	g.OnDeclare("lxc-members", "LXCMEMBER\talpha\tcontainer-a\tagent\tdesk\tworker\tpresent\t/srv/agent/one\t-\n"+
		"LXCMEMBER\talpha\tcontainer-b\tagent\t-\tworker\tpresent\t/srv/agent/two\tdev\n")
	if _, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleWorker}); err != nil {
		t.Fatal(err)
	}
	if len(g.r.reads) != 0 {
		t.Fatalf("reads %q", g.r.reads)
	}
}

func TestAnEntryWhoseCheckoutCannotBeReadStopsTheChangeBeforeAnything(t *testing.T) {
	g := newRig(t)
	g.OnDeclare("lxc-members", entryA)
	g.r.errs = map[string]error{"container-a": errors.New("/srv/agent/one of agent@container-a could not be read from the host: not running")}
	_, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err == nil || !strings.Contains(err.Error(), "not running") ||
		!strings.Contains(err.Error(), "the entry of agent@container-a blocks the change of "+lxcHandle) ||
		!strings.Contains(err.Error(), "give each of team alpha's entries its seat") ||
		!strings.Contains(err.Error(), "no role was changed") {
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
	g.OnDeclare("lxc-members", "LXCMEMBER\talpha\tcontainer-a\tagent\t-\tworker\tpresent\t/srv/agent/one\t1\n")
	g.r.errs = map[string]error{"container-a": errors.New("not running")}
	_, err := g.s.Set(t.Context(), "alpha", g.say, Step{Handle: lxcHandle, From: agentbus.RoleWorker, To: agentbus.RoleCoordinator})
	if err == nil || !strings.Contains(err.Error(), "Start container container-a") ||
		strings.Contains(err.Error(), "its seat") || strings.Contains(err.Error(), "gives no seat") {
		t.Fatalf("err %v", err)
	}
	if got := g.changes(); len(got) != 0 {
		t.Fatalf("changes %q", got)
	}
}

func TestTwoEntriesThatGiveTheHandleAreRefused(t *testing.T) {
	g := newRig(t)
	g.OnDeclare("lxc-members", entryA+entryB)
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
	g.OnDeclare("lxc-members", entryA)
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
	g.OnDeclare("lxc-members", entryA)
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
	g.OnDeclare("lxc-members", entryA)
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
	g.OnDeclare("lxc-members", entryA)
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
	g.OnDeclare("lxc-members", entryA)
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
