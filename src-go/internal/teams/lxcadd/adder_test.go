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

const (
	theScript   = "THE-ROOT-READER"
	probeOutput = `{"path": "/srv/agent/one", "uid": 1000}`
	derivedLine = "DERIVED\tproj\tworker\t1\tproj.1+worker.lxc\n"
	handle      = "proj.1+worker.lxc"
)

// lxc is the host's LXC as the flow sees it: which containers there are, who lives in one, and
// what the host-side reader prints for a checkout (the flow never enters a container).
type lxc struct {
	running, stopped []string
	users            []string
	usersErr         error
	err              error
	readUsersOf      []string

	probe, scan       string
	probeErr, scanErr error
	reads             []string
	scriptsRun        []string
}

func (l *lxc) Containers(context.Context) ([]string, []string, error) {
	return l.running, l.stopped, l.err
}

func (l *lxc) Users(_ context.Context, container string) ([]string, error) {
	l.readUsersOf = append(l.readUsersOf, container)
	return l.users, l.usersErr
}

func (l *lxc) ReadCheckout(_ context.Context, script, mode, container, user, path string) (string, error) {
	l.reads = append(l.reads, strings.Join([]string{mode, container, user, path}, " "))
	l.scriptsRun = append(l.scriptsRun, script)
	if mode == "probe" {
		return l.probe, l.probeErr
	}
	return l.scan, l.scanErr
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
		users: []string{"agent"}, probe: probeOutput,
		scan: `[{"path": "/srv/agent/one"}]`,
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
	w.OnDeclare("lxc-script", theScript)
	w.OnDeclare("lxc-checkouts", "CHECKOUT\t/srv/agent/one\texample-org/one\n")
	w.OnDeclare("lxc-derive", derivedLine)
	w.OnDeclare("lxc-check", "")
	w.OnDeclare("lxc-add", "CHANGED\tLXC member agent@container-a in team alpha, in "+w.Host.HostVars+"\n")
	return r
}

func (r *rig) run(args ...string) term.ExitCode { return r.a.Run(context.Background(), args) }

// answers queues what gum is asked, in order.
func (r *rig) answers(a ...string) { r.Script.Gum = a }

func choose(s string) string { return "CHOOSE:" + s }

// happy is every question answered for container-a, then the yes: the container, the user, the
// checkout and the role, and nothing else.
func (r *rig) happy() {
	r.answers(choose("container-a"), choose("agent"), choose("/srv/agent/one"), choose("worker"), "YES")
}

func (r *rig) addLine() string {
	for _, c := range r.Fake.Calls() {
		if strings.Contains(c.Line(), "helpers.agent_bus.declare lxc-add ") && !strings.HasSuffix(c.Line(), "--dry-run") {
			return c.Line()
		}
	}
	return ""
}

func (r *rig) deriveLine() string {
	for _, c := range r.Fake.Calls() {
		if strings.Contains(c.Line(), "helpers.agent_bus.declare lxc-derive ") {
			return c.Line()
		}
	}
	return ""
}

func TestHappyPathAsksOnlyTheContainerUserCheckoutAndRoleThenWritesAndAppliesAndSaysWhatIsLeft(t *testing.T) {
	r := newRig(t)
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	want := r.DeclarePrefix("lxc-add") + " --file " + r.Host.HostVars +
		" --team alpha --container container-a --user agent --checkout /srv/agent/one"
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
	for _, want := range []string{"agent@container-a", "ccy --teams alpha", "/srv/agent/one", "gh auth login", "container-a", handle} {
		if !strings.Contains(said, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, said)
		}
	}
	if !slices.Equal(r.lxc.readUsersOf, []string{"container-a"}) {
		t.Fatalf("users read of %v", r.lxc.readUsersOf)
	}
	if len(r.Script.GumLog) != 5 {
		t.Fatalf("%d questions were asked, want 5:\n%s", len(r.Script.GumLog), strings.Join(r.Script.GumLog, "\n"))
	}
}

func TestNoQuestionIsAskedAboutAHostWordOrASeat(t *testing.T) {
	r := newRig(t)
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	asked := strings.ToLower(strings.Join(r.Script.GumLog, "\n") + strings.Join(r.Script.Styled, "\n"))
	for _, banned := range []string{"host word", "handle host", "seat for", "step 5"} {
		if strings.Contains(asked, banned) {
			t.Fatalf("a question mentions %q:\n%s", banned, asked)
		}
	}
	for _, c := range r.Fake.Calls() {
		if strings.Contains(c.Line(), "lxc-suggest-seat") || strings.Contains(c.Line(), "--host") || strings.Contains(c.Line(), "--seat") {
			t.Fatalf("a host or a seat was passed on: %q", c.Line())
		}
	}
}

func TestTheCheckoutIsReadFromTheHostSideNeverByEnteringTheContainer(t *testing.T) {
	r := newRig(t)
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	want := []string{"checkouts container-a agent -", "probe container-a agent /srv/agent/one"}
	if !slices.Equal(r.lxc.reads, want) {
		t.Fatalf("reads %v, want %v", r.lxc.reads, want)
	}
	for _, s := range r.lxc.scriptsRun {
		if s != theScript {
			t.Fatalf("the reader ran %q, not the one declare.py gave", s)
		}
	}
	for _, c := range r.Fake.Calls() {
		if strings.Contains(c.Line(), "lxc-attach") {
			t.Fatalf("the container was entered: %q", c.Line())
		}
	}
}

func TestTheDerivedHandleIsShownInTheSummaryBeforeTheYes(t *testing.T) {
	r := newRig(t)
	r.answers(choose("container-a"), choose("agent"), choose("/srv/agent/one"), choose("worker"), "NO")
	if code := r.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	shown := strings.Join(r.Script.Styled, "\n")
	if !strings.Contains(shown, handle) || !strings.Contains(r.ErrOut.String()+shown, "repo proj, host worker, seat 1") {
		t.Fatalf("the summary before the yes does not show the handle:\n%s\n%s", shown, r.ErrOut.String())
	}
	if last := r.Script.GumLog[len(r.Script.GumLog)-1]; !strings.Contains(last, "gum confirm") {
		t.Fatalf("the yes was not asked last: %q", last)
	}
}

func TestTheDerivationIsGivenWhatWasReadAndTheTeamsMembers(t *testing.T) {
	r := newRig(t)
	r.roster = agentbus.Roster{Members: []agentbus.Member{
		{Handle: "proj.1+worker.lxc"}, {Handle: "proj.2+worker.lxc", Parked: true}, {Handle: "proj.3+local.podman"},
	}}
	r.answers(choose("container-a"), choose("agent"), choose("/srv/agent/one"), choose("worker"), "YES")
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	want := r.DeclarePrefix("lxc-derive") + " --file " + r.Host.HostVars + " --team alpha --container container-a --user agent" +
		" --probe " + probeOutput + " --roster proj.1+worker.lxc=active,proj.2+worker.lxc=parked,proj.3+local.podman=active"
	if got := r.deriveLine(); got != want {
		t.Fatalf("lxc-derive ran as\n%q\nwant\n%q", got, want)
	}
}

func TestACheckoutWithAGitHubRemoteInTheUsersHomeIsSuggested(t *testing.T) {
	r := newRig(t)
	r.OnDeclare("lxc-checkouts", "CHECKOUT\t/srv/agent/one\texample-org/one\nCHECKOUT\t/srv/agent/two\texample-org/two\n")
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	question := r.Script.GumLog[2]
	if !strings.Contains(question, "--value /srv/agent/one") {
		t.Fatalf("the checkout question did not suggest the first: %q", question)
	}
	if hint := strings.Join(r.Script.Styled, "\n"); !strings.Contains(hint, "/srv/agent/two (example-org/two)") {
		t.Fatalf("the others are not named:\n%s", hint)
	}
}

func TestWithNoCheckoutFoundTheCheckoutIsTypedFromTheUsersHome(t *testing.T) {
	r := newRig(t)
	r.OnDeclare("lxc-checkouts", "")
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if question := r.Script.GumLog[2]; !strings.Contains(question, "--value /home/"+"agent/") {
		t.Fatalf("the checkout question was %q", question)
	}
}

func TestACheckoutListThatCannotBeReadIsSaidAndTheCheckoutIsTyped(t *testing.T) {
	r := newRig(t)
	r.lxc.scanErr = errors.New("/srv/agent/x/.git/config is a symbolic link")
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if !strings.Contains(r.ErrOut.String(), "is a symbolic link") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}

func TestACheckoutThatCannotBeReadIsRefusedWithTheReasonAndAskedAgain(t *testing.T) {
	r := newRig(t)
	calls := 0
	r.answers(choose("container-a"), choose("agent"), choose("/srv/agent/bad"), choose("/srv/agent/one"), choose("worker"), "YES")
	r.a.Lister = readerFunc{r.lxc, func(path string) error {
		calls++
		if path == "/srv/agent/bad" {
			return errors.New("/srv/agent/bad is not a git checkout")
		}
		return nil
	}}
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if !strings.Contains(r.ErrOut.String(), "not a git checkout. Try again.") || !strings.Contains(r.addLine(), "--checkout /srv/agent/one") {
		t.Fatalf("stderr %s; add %q", r.ErrOut.String(), r.addLine())
	}
	if calls != 2 {
		t.Fatalf("the probe ran %d times", calls)
	}
}

// readerFunc is the lxc with a probe that fails for some paths.
type readerFunc struct {
	*lxc

	probeFails func(path string) error
}

func (r readerFunc) ReadCheckout(ctx context.Context, script, mode, container, user, path string) (string, error) {
	if mode == "probe" {
		if err := r.probeFails(path); err != nil {
			return "", err
		}
	}
	return r.lxc.ReadCheckout(ctx, script, mode, container, user, path)
}

func TestACheckoutThatCannotBeReadThreeTimesEndsTheRunWithNothingWritten(t *testing.T) {
	r := newRig(t)
	r.lxc.probeErr = errors.New("/srv/agent/bad is not a git checkout")
	r.answers(choose("container-a"), choose("agent"), choose("/srv/agent/bad"), choose("/srv/agent/bad"), choose("/srv/agent/bad"))
	if code := r.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if r.addLine() != "" || len(r.applied) != 0 || !strings.Contains(r.ErrOut.String(), "Giving up after 3 tries; nothing was changed.") {
		t.Fatalf("add %q applied %v: %s", r.addLine(), r.applied, r.ErrOut.String())
	}
}

func TestADerivationDeclarePyRefusesIsSaidAndTheCheckoutAskedAgain(t *testing.T) {
	r := newRig(t)
	refused := true
	r.Fake.OnFunc(strings.Fields(r.DeclarePrefix("lxc-derive")), func(execx.Cmd) (execx.Result, error) {
		if refused {
			refused = false
			return execx.Result{ExitCode: 64, Stderr: []byte("declare: agent_bus_lxc_members gives agent@container-a two hosts\n")}, nil
		}
		return execx.Result{Stdout: []byte(derivedLine)}, nil
	})
	r.answers(choose("container-a"), choose("agent"), choose("/srv/agent/one"), choose("/srv/agent/one"), choose("worker"), "YES")
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if !strings.Contains(r.ErrOut.String(), "two hosts. Try again.") {
		t.Fatalf("stderr %s", r.ErrOut.String())
	}
}

func TestAnUnreadableRosterStopsTheRunBeforeAnythingIsAskedBecauseTheSeatNeedsIt(t *testing.T) {
	r := newRig(t)
	r.rosterE = errors.New("agent-bus list failed")
	r.happy()
	if code := r.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	said := r.ErrOut.String()
	if !strings.Contains(said, "agent-bus list failed") || !strings.Contains(said, "ccy-teams apply alpha") ||
		r.addLine() != "" || len(r.Script.GumLog) != 0 {
		t.Fatalf("stderr %s; add %q; asked %d", said, r.addLine(), len(r.Script.GumLog))
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
		"an entry that leaves its host to be derived": {
			members: "LXCMEMBER\talpha\tcontainer-b\tagent\t-\tcoordinator\tpresent\n",
		},
		"live coordinator": {
			roster: agentbus.Roster{Members: []agentbus.Member{{Handle: "proj.1+local.podman", Role: agentbus.RoleCoordinator}}},
		},
	}
	for name, tc := range cases {
		r := newRig(t)
		r.OnDeclare("lxc-members", tc.members)
		r.roster = tc.roster
		r.answers(choose("container-a"), choose("agent"), choose("/srv/agent/one"))
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

func TestTheUserIsSuggestedFromTheContainerAndAskedAgainWhenRefused(t *testing.T) {
	r := newRig(t)
	r.lxc.users = []string{"agent", "other"}
	r.answers(choose("container-a"), choose("Bad User"), choose("agent"), choose("/srv/agent/one"), choose("worker"), "YES")
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

func TestAUserAlreadyInOneTeamIsAddedToASecond(t *testing.T) {
	r := newRig(t)
	r.OnDeclare("lxc-members", "LXCMEMBER\tbeta\tcontainer-a\tagent\tworker\tworker\tpresent\n")
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if !strings.Contains(r.addLine(), "--user agent --checkout /srv/agent/one") {
		t.Fatalf("add %q", r.addLine())
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
	if len(r.lxc.reads) != 1 || !strings.HasPrefix(r.lxc.reads[0], "checkouts") {
		t.Fatalf("a refused path was read from the container: %v", r.lxc.reads)
	}
}

func TestDecliningTheSummaryWritesNothing(t *testing.T) {
	r := newRig(t)
	r.answers(choose("container-a"), choose("agent"), choose("/srv/agent/one"), choose("worker"), "NO")
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

// minted makes the play's apply add a member to the team, as a real apply would.
func (r *rig) minted(handles ...string) {
	r.a.ApplyTeams = func(_ context.Context, teams ...string) error {
		r.applied = append(r.applied, teams...)
		for _, h := range handles {
			r.roster.Members = append(r.roster.Members, agentbus.Member{Handle: h})
		}
		return nil
	}
}

func TestTheHandleSaidAfterTheApplyIsTheOneThePlayMintedNotThePreview(t *testing.T) {
	r := newRig(t)
	r.happy()
	r.roster = agentbus.Roster{Members: []agentbus.Member{{Handle: handle}, {Handle: "other.1+worker.lxc"}}}
	r.minted("proj.2+worker.lxc")
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	said := r.ErrOut.String()
	if !strings.Contains(said, "is a member of alpha as proj.2+worker.lxc.") || strings.Contains(said, "as "+handle+".") {
		t.Fatalf("the handle said is not the one the play minted:\n%s", said)
	}
}

func TestAParkedHandleTheApplyBroughtBackIsTheOneReported(t *testing.T) {
	r := newRig(t)
	r.happy()
	r.roster = agentbus.Roster{Members: []agentbus.Member{{Handle: "proj.2+worker.lxc", Parked: true}}}
	r.a.ApplyTeams = func(context.Context, ...string) error {
		r.roster.Members[0].Parked = false
		return nil
	}
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if said := r.ErrOut.String(); !strings.Contains(said, "is a member of alpha as proj.2+worker.lxc.") {
		t.Fatalf("the returned handle is not reported:\n%s", said)
	}
}

func TestWhenTheApplyShowsNoNewMemberTheExpectedHandleIsSaidToBeOnlyExpected(t *testing.T) {
	r := newRig(t)
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	said := r.ErrOut.String()
	if strings.Contains(said, "is a member of alpha as") || !strings.Contains(said, "expected handle "+handle) {
		t.Fatalf("an unconfirmed handle is said as a result:\n%s", said)
	}
}

func TestWhenTheRosterCannotBeReadBackTheExpectedHandleIsSaidToBeOnlyExpected(t *testing.T) {
	r := newRig(t)
	r.happy()
	r.a.ApplyTeams = func(context.Context, ...string) error {
		r.rosterE = errors.New("agent-bus list failed")
		return nil
	}
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	said := r.ErrOut.String()
	if strings.Contains(said, "is a member of alpha as") || !strings.Contains(said, "expected handle "+handle) ||
		!strings.Contains(said, "agent-bus list failed") {
		t.Fatalf("a failed read-back is not said:\n%s", said)
	}
}

func TestSeveralNewMembersMeanTheHandleCannotBeTold(t *testing.T) {
	r := newRig(t)
	r.happy()
	r.minted("proj.2+worker.lxc", "proj.3+worker.lxc")
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if said := r.ErrOut.String(); strings.Contains(said, "is a member of alpha as") || !strings.Contains(said, "expected handle "+handle) {
		t.Fatalf("an ambiguous read-back is said as a result:\n%s", said)
	}
}

func TestTheSummaryCallsTheHandleExpectedBecauseThePlayMintsIt(t *testing.T) {
	r := newRig(t)
	r.answers(choose("container-a"), choose("agent"), choose("/srv/agent/one"), choose("worker"), "NO")
	if code := r.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if shown := strings.Join(r.Script.Styled, "\n"); !strings.Contains(shown, "Handle (expected)") {
		t.Fatalf("the summary does not say the handle is expected:\n%s", shown)
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
