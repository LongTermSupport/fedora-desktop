package lxcadd

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// lxc is the host's LXC as the flow sees it.
type lxc struct {
	running, stopped []string
	users            []string
	usersErr         error
	err              error
	readUsersOf      []string
}

func (l *lxc) Containers(context.Context) ([]string, []string, error) {
	return l.running, l.stopped, l.err
}

func (l *lxc) Users(_ context.Context, container string) ([]string, error) {
	l.readUsersOf = append(l.readUsersOf, container)
	return l.users, l.usersErr
}

// rig is the fake machine on a terminal with scripted gum, and the flow over it.
type rig struct {
	*teamtest.World
	*teamtest.Console

	a       *Adder
	lxc     *lxc
	applied []string
	roster  agentbus.Roster
	rosterE error
	sudoOK  bool
	events  []string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	w := teamtest.Populated(t)
	c := teamtest.Interactive(t, w)
	c.Script.SkipStyle = true
	b := board.New(w.Host)
	r := &rig{World: w, Console: c, lxc: &lxc{
		running: []string{"container-a", "container-b"}, stopped: []string{"container-c"},
		users: []string{"agent"},
	}, sudoOK: true}
	r.a = New(w.Host, flow.New(w.Host), ops.New(w.Host, b.Statuses, nil))
	r.a.Lister = r.lxc
	r.a.Roster = func(context.Context, string) (agentbus.Roster, error) { return r.roster, r.rosterE }
	r.a.SudoReady = func(context.Context, string) bool { return r.sudoOK }
	r.a.ApplyTeams = func(_ context.Context, teams ...string) error {
		r.applied = append(r.applied, teams...)
		r.events = append(r.events, "apply")
		return nil
	}
	w.On(w.DeclarePrefix("declared"), teamtest.DeclaredAlpha, 0)
	w.OnDeclare("lxc-members", "")
	w.OnDeclare("lxc-suggest-seat", "SEAT\tdefault\n")
	w.OnDeclare("lxc-check", "")
	w.OnDeclare("lxc-add", "CHANGED\tLXC member agent@container-a in team alpha, in "+w.Host.HostVars+"\n")
	return r
}

func (r *rig) run(args ...string) term.ExitCode { return r.a.Run(context.Background(), args) }

// answers queues what gum is asked, in order.
func (r *rig) answers(a ...string) { r.Script.Gum = a }

func choose(s string) string { return "CHOOSE:" + s }

// happy is every question answered for container-a, then the yes.
func (r *rig) happy() {
	r.answers(choose("container-a"), choose("agent"), choose("/srv/agent/one"), choose("worker"), choose("worker"), "YES")
}

func (r *rig) addLine() string {
	for _, c := range r.Fake.Calls() {
		if strings.Contains(c.Line(), "helpers.agent_bus.declare lxc-add ") && !strings.HasSuffix(c.Line(), "--dry-run") {
			return c.Line()
		}
	}
	return ""
}

func TestHappyPathAsksEachThingThenWritesAndAppliesAndSaysWhatIsLeft(t *testing.T) {
	r := newRig(t)
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	want := r.DeclarePrefix("lxc-add") + " --file " + r.Host.HostVars +
		" --team alpha --container container-a --user agent --host worker --checkout /srv/agent/one"
	if got := r.addLine(); got != want {
		t.Fatalf("lxc-add ran as\n%q\nwant\n%q", got, want)
	}
	if !slices.Equal(r.applied, []string{"alpha"}) {
		t.Fatalf("applied %v", r.applied)
	}
	if got := r.Out.String(); !strings.HasPrefix(got, "CHANGED\t") {
		t.Fatalf("stdout %q", got)
	}
	said := r.ErrOut.String()
	for _, want := range []string{"agent@container-a", "ccy --teams alpha", "/srv/agent/one", "gh auth login", "container-a"} {
		if !strings.Contains(said, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, said)
		}
	}
	if !slices.Equal(r.lxc.readUsersOf, []string{"container-a"}) {
		t.Fatalf("users read of %v", r.lxc.readUsersOf)
	}
}

func TestOnlyRunningContainersAreOfferedAndTheStoppedOnesAreNamed(t *testing.T) {
	r := newRig(t)
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	first := r.Script.GumLog[0]
	if !strings.Contains(first, "container-a") || !strings.Contains(first, "container-b") || strings.Contains(first, "container-c") {
		t.Fatalf("the container question was %q", first)
	}
	if !strings.Contains(r.ErrOut.String(), "container-c") {
		t.Fatalf("the stopped container is not named: %s", r.ErrOut.String())
	}
}

func TestCoordinatorIsOfferedOnlyWhenTheTeamHasNone(t *testing.T) {
	cases := map[string]struct {
		members string
		roster  agentbus.Roster
		offered bool
	}{
		"none": {offered: true},
		"declared coordinator": {
			members: "LXCMEMBER\talpha\tcontainer-b\tagent\tworker\tcoordinator\tpresent\n",
		},
		"coordinator declared absent does not count": {
			members: "LXCMEMBER\talpha\tcontainer-b\tagent\tworker\tcoordinator\tabsent\n", offered: true,
		},
		"another team's coordinator does not count": {
			members: "LXCMEMBER\tbeta\tcontainer-b\tagent\tworker\tcoordinator\tpresent\n", offered: true,
		},
		"live coordinator": {
			roster: agentbus.Roster{Members: []agentbus.Member{{Handle: "proj.1+local.podman", Role: agentbus.RoleCoordinator}}},
		},
	}
	for name, tc := range cases {
		r := newRig(t)
		r.OnDeclare("lxc-members", tc.members)
		r.roster = tc.roster
		r.answers(choose("container-a"), choose("agent"), choose("/srv/agent/one"), choose("worker"))
		if tc.offered {
			r.Script.Gum = append(r.Script.Gum, choose("coordinator"))
		}
		r.Script.Gum = append(r.Script.Gum, "YES")
		if code := r.run("alpha"); code != term.ExitOK {
			t.Fatalf("%s: exit %d: %s", name, code, r.ErrOut.String())
		}
		asked := 0
		for _, line := range r.Script.GumLog {
			if strings.Contains(line, "coordinator") && strings.Contains(line, "worker") {
				asked++
			}
		}
		if (asked == 1) != tc.offered {
			t.Fatalf("%s: the role was asked %d times (offered %v)", name, asked, tc.offered)
		}
		if got := strings.Contains(r.addLine(), "--role coordinator"); got != tc.offered {
			t.Fatalf("%s: lxc-add %q", name, r.addLine())
		}
	}
}

func TestAnUnreadableRosterOffersWorkerOnlyAndSaysWhy(t *testing.T) {
	r := newRig(t)
	r.rosterE = errors.New("agent-bus list failed")
	r.happy()
	r.Script.Gum = []string{choose("container-a"), choose("agent"), choose("/srv/agent/one"), choose("worker"), "YES"}
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if !strings.Contains(r.ErrOut.String(), "agent-bus list failed") || strings.Contains(r.addLine(), "--role") {
		t.Fatalf("stderr %s; add %q", r.ErrOut.String(), r.addLine())
	}
}

func TestTheUserIsSuggestedFromTheContainerAndAskedAgainWhenRefused(t *testing.T) {
	r := newRig(t)
	r.lxc.users = []string{"agent", "other"}
	r.answers(choose("container-a"), choose("Bad User"), choose("agent"), choose("/srv/agent/one"), choose("worker"), choose("worker"), "YES")
	r.Fake.OnFunc(strings.Fields(r.DeclarePrefix("lxc-check")), func(c execx.Cmd) (execx.Result, error) {
		if slices.Contains(c.Args, "Bad User") {
			return execx.Result{ExitCode: 64, Stderr: []byte("declare: user must be a user name\n")}, nil
		}
		return execx.Result{}, nil
	})
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	said := r.ErrOut.String()
	if !strings.Contains(said, "user must be a user name. Try again.") ||
		!strings.Contains(strings.Join(r.Script.Styled, "\n"), "It has: agent, other.") {
		t.Fatalf("stderr:\n%s", said)
	}
	userQuestion := r.Script.GumLog[1]
	if !strings.Contains(userQuestion, "--value agent") {
		t.Fatalf("the user question did not suggest agent: %q", userQuestion)
	}
	if !strings.Contains(r.addLine(), "--user agent") {
		t.Fatalf("add %q", r.addLine())
	}
}

func TestAnUnreadableUserListIsSaidAndTheUserIsTyped(t *testing.T) {
	r := newRig(t)
	r.lxc.users = nil
	r.lxc.usersErr = errors.New("the users of container-a could not be read: boom")
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if !strings.Contains(r.ErrOut.String(), "could not be read: boom") || strings.Contains(r.Script.GumLog[1], "--value agent") {
		t.Fatalf("stderr %s; question %q", r.ErrOut.String(), r.Script.GumLog[1])
	}
}

func TestTheHostWordSuggestedIsTheRoleWordNeverTheContainerName(t *testing.T) {
	r := newRig(t)
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	hostQuestion := r.Script.GumLog[3]
	if !strings.Contains(hostQuestion, "--value worker") || strings.Contains(hostQuestion, "container-a") {
		t.Fatalf("the host question was %q", hostQuestion)
	}
}

// existingMember declares agent@container-a in another team with the host, and makes lxc-check
// judge as declare.py does: one user of one container has one host in every team.
func (r *rig) existingMember(host string) {
	r.OnDeclare("lxc-members", "LXCMEMBER\tbeta\tcontainer-a\tagent\t"+host+"\tworker\tpresent\n")
	r.Fake.OnFunc(strings.Fields(r.DeclarePrefix("lxc-check")), func(c execx.Cmd) (execx.Result, error) {
		if i := slices.Index(c.Args, "--host"); i >= 0 && c.Args[i+1] != host {
			return execx.Result{ExitCode: 64, Stderr: []byte("declare: agent_bus_lxc_members gives agent@container-a two hosts\n")}, nil
		}
		return execx.Result{}, nil
	})
}

func TestAUserAlreadyInOneTeamIsAddedToASecond(t *testing.T) {
	r := newRig(t)
	r.existingMember("worker")
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	for _, c := range r.Fake.Calls() {
		line := c.Line()
		if strings.Contains(line, "lxc-check") && strings.Contains(line, "--user agent") && strings.Contains(line, "--host") &&
			!strings.Contains(line, "--host worker") {
			t.Fatalf("the user was checked with another host: %q", line)
		}
	}
	if strings.Contains(r.ErrOut.String(), "two hosts") || !strings.Contains(r.addLine(), "--user agent --host worker") {
		t.Fatalf("stderr %s; add %q", r.ErrOut.String(), r.addLine())
	}
}

func TestTheHostIsCheckedAgainstTheUserAtTheHostQuestion(t *testing.T) {
	r := newRig(t)
	r.existingMember("worker")
	r.answers(choose("container-a"), choose("agent"), choose("/srv/agent/one"), choose("other"), choose("worker"), choose("worker"), "YES")
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if !strings.Contains(r.ErrOut.String(), "two hosts. Try again.") || !strings.Contains(r.addLine(), "--host worker") {
		t.Fatalf("stderr %s; add %q", r.ErrOut.String(), r.addLine())
	}
}

func TestASeatIsAskedForWhenTheContainerNameGivesNone(t *testing.T) {
	r := newRig(t)
	r.lxc.running = []string{"2024box"}
	r.OnDeclare("lxc-suggest-seat", "SEAT\tneeded\tc2024box\n")
	r.answers(choose("2024box"), choose("c2024box"), choose("agent"), choose("/srv/agent/one"), choose("worker"), choose("worker"), "YES")
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	seatQuestion := r.Script.GumLog[1]
	if !strings.Contains(seatQuestion, "--value c2024box") {
		t.Fatalf("the seat question was %q", seatQuestion)
	}
	if !strings.HasSuffix(r.addLine(), "--seat c2024box") {
		t.Fatalf("add %q", r.addLine())
	}
}

func TestASeatCollisionIsCaughtAtTheSeatQuestionAndAskedAgain(t *testing.T) {
	r := newRig(t)
	r.Fake.OnFunc(strings.Fields(r.DeclarePrefix("lxc-suggest-seat")), func(c execx.Cmd) (execx.Result, error) {
		i := slices.Index(c.Args, "--seat")
		switch {
		case i < 0:
			return execx.Result{Stdout: []byte("SEAT\tneeded\tlongcontain2\n")}, nil
		case c.Args[i+1] == "longcontaine":
			return execx.Result{ExitCode: 64, Stderr: []byte("declare: --seat: seat longcontaine of team alpha to two containers\n")}, nil
		}
		return execx.Result{}, nil
	})
	r.answers(choose("container-a"), choose("longcontaine"), choose("longcontain2"), choose("agent"), choose("/srv/agent/one"),
		choose("worker"), choose("worker"), "YES")
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if !strings.Contains(r.ErrOut.String(), "seat longcontaine of team alpha to two containers. Try again.") ||
		!strings.HasSuffix(r.addLine(), "--seat longcontain2") {
		t.Fatalf("stderr %s; add %q", r.ErrOut.String(), r.addLine())
	}
}

func TestASeatRefusedThreeTimesEndsTheRunWithNothingWritten(t *testing.T) {
	r := newRig(t)
	r.Fake.OnFunc(strings.Fields(r.DeclarePrefix("lxc-suggest-seat")), func(c execx.Cmd) (execx.Result, error) {
		if slices.Contains(c.Args, "--seat") {
			return execx.Result{ExitCode: 64, Stderr: []byte("declare: --seat: taken\n")}, nil
		}
		return execx.Result{Stdout: []byte("SEAT\tneeded\t\n")}, nil
	})
	r.answers(choose("container-a"), choose("x"), choose("x"), choose("x"))
	if code := r.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if r.addLine() != "" || !strings.Contains(r.ErrOut.String(), "Giving up after 3 tries; nothing was changed.") {
		t.Fatalf("add %q: %s", r.addLine(), r.ErrOut.String())
	}
}

func TestARefusedAnswerIsAskedThreeTimesThenTheRunEndsWithNothingWritten(t *testing.T) {
	r := newRig(t)
	r.answers(choose("container-a"), choose("agent"), choose("rel/path"), choose("rel/path"), choose("rel/path"))
	r.Fake.OnFunc(strings.Fields(r.DeclarePrefix("lxc-check")), func(c execx.Cmd) (execx.Result, error) {
		if slices.Contains(c.Args, "--checkout") {
			return execx.Result{ExitCode: 64, Stderr: []byte("declare: checkout must be an absolute path inside the container\n")}, nil
		}
		return execx.Result{}, nil
	})
	if code := r.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if r.addLine() != "" || len(r.applied) != 0 || !strings.Contains(r.ErrOut.String(), "Giving up after 3 tries; nothing was changed.") {
		t.Fatalf("add %q applied %v: %s", r.addLine(), r.applied, r.ErrOut.String())
	}
}

func TestDecliningTheSummaryWritesNothing(t *testing.T) {
	r := newRig(t)
	r.answers(choose("container-a"), choose("agent"), choose("/srv/agent/one"), choose("worker"), choose("worker"), "NO")
	if code := r.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if r.addLine() != "" || len(r.applied) != 0 || !strings.Contains(r.ErrOut.String(), "Nothing was added.") {
		t.Fatalf("add %q applied %v: %s", r.addLine(), r.applied, r.ErrOut.String())
	}
	if !strings.Contains(strings.Join(r.Script.GumLog, "\n"), "agent@container-a") {
		t.Fatalf("the summary did not name the member:\n%s", strings.Join(r.Script.GumLog, "\n"))
	}
}

func TestCtrlCAnywhereIsACancelWithNothingWritten(t *testing.T) {
	for at := range 5 {
		r := newRig(t)
		r.happy()
		r.Script.Gum = append(slices.Clone(r.Script.Gum[:at]), "CANCEL")
		if code := r.run("alpha"); code != term.ExitCancelled {
			t.Fatalf("cancel at %d: exit %d", at, code)
		}
		if r.addLine() != "" || len(r.applied) != 0 || !strings.Contains(r.ErrOut.String(), "Cancelled; nothing was changed.") {
			t.Fatalf("cancel at %d: add %q: %s", at, r.addLine(), r.ErrOut.String())
		}
	}
}

func TestAFailedApplyKeepsTheEntryAndSaysHowToGoOn(t *testing.T) {
	r := newRig(t)
	r.happy()
	r.a.ApplyTeams = func(context.Context, ...string) error { return errors.New("play failed") }
	if code := r.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	said := r.ErrOut.String()
	for _, want := range []string{"is declared in", "ccy-teams apply alpha", "gh auth login", "lxc-attach -n container-a", "agent"} {
		if !strings.Contains(said, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, said)
		}
	}
	if strings.Contains(said, "ccy --teams alpha") {
		t.Fatalf("the sessions step is offered before the member is applied:\n%s", said)
	}
}

func TestATeamNotDeclaredIsRefusedBeforeAnythingIsAsked(t *testing.T) {
	r := newRig(t)
	if code := r.run("nosuch"); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(r.ErrOut.String(), "team nosuch is not declared present") || len(r.Script.GumLog) != 0 {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}

func TestTheWrongArgumentsAreAUsageMistake(t *testing.T) {
	for _, args := range [][]string{nil, {"alpha", "beta"}, {"--yes"}} {
		r := newRig(t)
		if code := r.run(args...); code != term.ExitUsage {
			t.Fatalf("%v: exit %d", args, code)
		}
		if !strings.Contains(r.ErrOut.String(), "ccy-teams lxc-add <team>") {
			t.Fatalf("%v: stderr %s", args, r.ErrOut.String())
		}
	}
}

func TestWithoutATerminalItAsksNothingAndSaysSo(t *testing.T) {
	r := newRig(t)
	r.Host.IsTerminal = func(any) bool { return false }
	r.Host.Term.IsTerminal = r.Host.IsTerminal
	r.a.NewTerm = r.Host.FreshTerm
	if code := r.run("alpha"); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(r.ErrOut.String(), "asks its questions on a terminal") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}

func TestWithoutGumItNamesThePlayThatInstallsIt(t *testing.T) {
	r := newRig(t)
	r.a.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	if code := r.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(r.ErrOut.String(), "gum is not installed") || !strings.Contains(r.ErrOut.String(), "play-claude-yolo.yml") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}

func TestLxcFailuresStopTheRunBeforeAnythingIsWritten(t *testing.T) {
	cases := map[string]func(r *rig){
		"lxc not installed": func(r *rig) { r.lxc.err = errors.New("LXC is not installed on this host (missing: lxc-ls)") },
		"sudo refused":      func(r *rig) { r.sudoOK = false },
		"no running container": func(r *rig) {
			r.lxc.running = nil
		},
	}
	want := map[string]string{
		"lxc not installed":    "LXC is not installed on this host",
		"sudo refused":         "nothing was changed",
		"no running container": "no running LXC container",
	}
	for name, setup := range cases {
		r := newRig(t)
		setup(r)
		if code := r.run("alpha"); code != term.ExitFailure {
			t.Fatalf("%s: exit %d", name, code)
		}
		if !strings.Contains(r.ErrOut.String(), want[name]) || r.addLine() != "" || len(r.Script.GumLog) != 0 {
			t.Fatalf("%s: stderr %s", name, r.ErrOut.String())
		}
	}
	r := newRig(t)
	r.lxc.running = nil
	if r.run("alpha"); !strings.Contains(r.ErrOut.String(), "container-c") {
		t.Fatalf("the stopped container is not named: %s", r.ErrOut.String())
	}
}

func TestRefusalFromDeclarePyThatIsNotATypedAnswerEndsTheRunWithItsCode(t *testing.T) {
	r := newRig(t)
	r.happy()
	r.OnErr(r.DeclarePrefix("lxc-add"), "declare: team alpha is gone\n", 78)
	if code := r.run("alpha"); code != 78 {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if !strings.Contains(r.ErrOut.String(), "declare: team alpha is gone") || len(r.applied) != 0 {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}
