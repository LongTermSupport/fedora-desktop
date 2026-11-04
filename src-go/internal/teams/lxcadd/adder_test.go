package lxcadd

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/coordinator"
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
	// oneRow is the checkout found in the user's home as the Checkout question offers it.
	oneRow = "/srv/agent/one   (example-org/one)"
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

	// dirs is what the reader prints for a directory listing, by path; dirsErr its failures.
	dirs    map[string]string
	dirsErr map[string]error
	// hang makes the home search wait until it is stopped, as one too slow to finish would.
	hang bool

	// sessions is what the reader prints for the user's Claude sessions; sessionsErr a failed look.
	sessions    string
	sessionsErr error
}

func (l *lxc) LxcPath(context.Context) (string, error) { return "/var/lib/lxc", nil }

func (l *lxc) Containers(context.Context) ([]string, []string, error) {
	return l.running, l.stopped, l.err
}

func (l *lxc) Users(_ context.Context, container string) ([]string, error) {
	l.readUsersOf = append(l.readUsersOf, container)
	return l.users, l.usersErr
}

func (l *lxc) ReadCheckout(ctx context.Context, script, mode, container, user, path string) (string, error) {
	l.reads = append(l.reads, strings.Join([]string{mode, container, user, path}, " "))
	l.scriptsRun = append(l.scriptsRun, script)
	switch mode {
	case "probe":
		return l.probe, l.probeErr
	case "sessions":
		if l.sessions == "" && l.sessionsErr == nil {
			return `{"sessions": []}`, nil
		}
		return l.sessions, l.sessionsErr
	case "dirs":
		if err := l.dirsErr[path]; err != nil {
			return "", err
		}
		if out, ok := l.dirs[path]; ok {
			return out, nil
		}
		return `{"path": "` + path + `", "exists": false, "git": false, "truncated": false, "dirs": []}`, nil
	}
	if l.hang {
		<-ctx.Done()
		return "", ctx.Err()
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
	// setRoles is every set-role, as "<team> <handle> <role>".
	setRoles []string
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
	r.a.SetRole = func(_ context.Context, team, handle, role string) error {
		r.setRoles = append(r.setRoles, team+" "+handle+" "+role)
		r.events = append(r.events, "set-role")
		return nil
	}
	r.a.ApplyTeams = func(_ context.Context, teams ...string) error {
		r.applied = append(r.applied, teams...)
		r.events = append(r.events, "apply")
		return nil
	}
	w.On(w.DeclarePrefix("declared"), teamtest.DeclaredAlpha, 0)
	w.OnDeclare("lxc-members", "")
	w.OnDeclare("lxc-script", theScript)
	w.OnDeclare("lxc-checkouts", "CHECKOUT\t/srv/agent/one\texample-org/one\t-\n")
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
	r.answers(choose("container-a"), choose("agent"), choose(oneRow), choose("worker"), "YES")
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
	for _, want := range []string{
		"agent@container-a", "agent-bus-claude", "/srv/agent/one", "gh auth login", "container-a", handle,
		"sudo lxc-attach -P /var/lib/lxc -n container-a -- su - agent",
		"set -a; . ~/.config/pingbus/env; set +a; pingbus config check && pingbus history --limit 1",
		"agent-bus-claude refuses to start until",
	} {
		if !strings.Contains(said, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, said)
		}
	}
	if strings.Contains(said, "ccy --teams alpha") {
		t.Fatalf("the client project's ccy is named, which refuses a team value in a container:\n%s", said)
	}
	if !slices.Equal(r.lxc.readUsersOf, []string{"container-a"}) {
		t.Fatalf("users read of %v", r.lxc.readUsersOf)
	}
	if len(r.Script.GumLog) != 5 {
		t.Fatalf("%d questions were asked, want 5:\n%s", len(r.Script.GumLog), strings.Join(r.Script.GumLog, "\n"))
	}
	// Each question names the team it adds to, its step, and the answers so far; the last is
	// the review of what joins which team.
	for i, want := range []string{
		"Teams › alpha › Add LXC member · step 1/4 Container\ninto team alpha",
		"Teams › alpha › Add LXC member · step 2/4 User\ninto team alpha · container container-a",
		"Teams › alpha › Add LXC member · step 3/4 Checkout\ninto team alpha · container container-a · user agent",
		"Teams › alpha › Add LXC member · step 4/4 Role\ninto team alpha · container container-a · user agent · checkout /srv/agent/one",
		"Teams › alpha › Add LXC member › Review\nagent@container-a joins team alpha · role worker",
	} {
		r.Script.HeadingAt(i, want)
	}
}

func TestAClaudeSessionAlreadyRunningIsToldToBeStartedAgainToJoin(t *testing.T) {
	r := newRig(t)
	r.happy()
	const started = 1792144800
	r.lxc.sessions = `{"sessions": [1792144800]}`
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	want := "A Claude session for agent is already running in container-a (started " +
		time.Unix(started, 0).Format(startedLayout) + "). " +
		"End it and start it again with agent-bus-claude, as agent in /srv/agent/one, to join alpha."
	if said := r.ErrOut.String(); !strings.Contains(said, want) {
		t.Fatalf("stderr lacks %q:\n%s", want, said)
	}
	if !slices.Contains(r.lxc.reads, "sessions container-a agent -") {
		t.Fatalf("sessions not looked for from the host's side: %v", r.lxc.reads)
	}
}

func TestASessionListThatIsNotWhatTheReaderPrintsIsSaidAsAFailedLook(t *testing.T) {
	r := newRig(t)
	r.happy()
	r.lxc.sessions = `{"sessions": ["soon"]}`
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if said := r.ErrOut.String(); !strings.Contains(said, "could not be checked") {
		t.Fatalf("stderr:\n%s", said)
	}
}

func TestWithNoSessionRunningItSaysHowToStartOne(t *testing.T) {
	r := newRig(t)
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if said := r.ErrOut.String(); !strings.Contains(said, "No Claude session for agent is running in container-a. Start one: as agent in /srv/agent/one, agent-bus-claude") {
		t.Fatalf("stderr:\n%s", said)
	}
}

func TestASessionLookThatFailsIsSaidWithWhatToDoEitherWay(t *testing.T) {
	r := newRig(t)
	r.happy()
	r.lxc.sessionsErr = errors.New("the processes of agent@container-a could not be read: ps: not found")
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	said := r.ErrOut.String()
	if !strings.Contains(said, "ps: not found") || !strings.Contains(said, "started before this add must be ended and started again") {
		t.Fatalf("stderr:\n%s", said)
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
	want := []string{"checkouts container-a agent -", "probe container-a agent /srv/agent/one", "sessions container-a agent -"}
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
	r.answers(choose("container-a"), choose("agent"), choose(oneRow), choose("worker"), "NO")
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
	r.answers(choose("container-a"), choose("agent"), choose(oneRow), choose("worker"), "YES")
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	want := r.DeclarePrefix("lxc-derive") + " --file " + r.Host.HostVars + " --team alpha --container container-a --user agent" +
		" --probe " + probeOutput + " --roster proj.1+worker.lxc=active,proj.2+worker.lxc=parked,proj.3+local.podman=active"
	if got := r.deriveLine(); got != want {
		t.Fatalf("lxc-derive ran as\n%q\nwant\n%q", got, want)
	}
}

func TestTheCheckoutsInTheUsersHomeAreOfferedAsChoicesCcyProjectsFirstWithATypedPath(t *testing.T) {
	r := newRig(t)
	r.OnDeclare("lxc-checkouts", "CHECKOUT\t/srv/agent/two\texample-org/two\tccy\nCHECKOUT\t/srv/agent/one\texample-org/one\t-\n")
	r.happy()
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	question := r.Script.GumLog[2]
	want := "gum choose --header Checkout in container-a --selected /srv/agent/two   (example-org/two, a ccy project) " +
		"/srv/agent/two   (example-org/two, a ccy project) " + oneRow + " Type a path…"
	if question != want {
		t.Fatalf("the checkout question was\n%q\nwant\n%q", question, want)
	}
	r.Script.HeadingAt(2, "Teams › alpha › Add LXC member · step 3/4 Checkout\ninto team alpha · container container-a · user agent")
}

func TestACheckoutListThatCannotBeReadIsSaidAndTheCheckoutIsTyped(t *testing.T) {
	r := newRig(t)
	r.lxc.scanErr = errors.New("/srv/agent/x/.git/config is a symbolic link")
	r.lxc.dirs = map[string]string{home: homeListing}
	r.answers(choose("container-a"), choose("agent"), choose("worker"), "YES")
	r.Script.Fzf = []string{"|TYPED:/srv/agent/one"}
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	said := r.ErrOut.String()
	if !strings.Contains(said, "could not be listed: /srv/agent/x/.git/config is a symbolic link") ||
		!strings.Contains(r.addLine(), "--checkout /srv/agent/one") {
		t.Fatalf("stderr %s; add %q", said, r.addLine())
	}
}

func TestACheckoutThatCannotBeReadIsRefusedWithTheReasonAndAskedAgain(t *testing.T) {
	r := newRig(t)
	calls := 0
	r.lxc.dirs = map[string]string{home: homeListing}
	r.answers(choose("container-a"), choose("agent"), choose(typePath), choose(oneRow), choose("worker"), "YES")
	r.Script.Fzf = []string{"|TYPED:/srv/agent/bad"}
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
	r.lxc.probeErr = errors.New("/srv/agent/one is not a git checkout")
	r.answers(choose("container-a"), choose("agent"), choose(oneRow), choose(oneRow), choose(oneRow))
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
	r.answers(choose("container-a"), choose("agent"), choose(oneRow), choose(oneRow), choose("worker"), "YES")
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

// The two coordinator paths (Plan 00203 SPEC-hot-control.md H25): a team with no coordinator
// is offered one plainly; a team that has one offers the takeover as its own choice, which
// names the coordinator and asks again, on No. The role is never changed silently.
func TestTheCoordinatorIsOfferedPlainlyWhenTheTeamHasNone(t *testing.T) {
	cases := map[string]string{
		"none": "",
		"coordinator declared absent does not count": "LXCMEMBER\talpha\tcontainer-b\tagent\tworker\tcoordinator\tabsent\n",
		"another team's coordinator does not count":  "LXCMEMBER\tbeta\tcontainer-b\tagent\tworker\tcoordinator\tpresent\n",
	}
	for name, members := range cases {
		r := newRig(t)
		r.OnDeclare("lxc-members", members)
		r.answers(choose("container-a"), choose("agent"), choose(oneRow), choose(coordinator.NoneChoice("alpha")), "YES")
		if code := r.run("alpha"); code != term.ExitOK {
			t.Fatalf("%s: exit %d: %s", name, code, r.ErrOut.String())
		}
		if q := r.roleQuestion(); !strings.Contains(q, "--selected worker worker "+coordinator.NoneChoice("alpha")) {
			t.Fatalf("%s: role question %q", name, q)
		}
		if !strings.Contains(r.addLine(), "--role coordinator") {
			t.Fatalf("%s: lxc-add %q", name, r.addLine())
		}
	}
}

// roleHeading is the heading every Role question carries, the takeover offer included.
const roleHeading = "Teams › alpha › Add LXC member · step 4/4 Role\ninto team alpha · container container-a · user agent · checkout /srv/agent/one"

// roleQuestion is the gum call that asked the role.
func (r *rig) roleQuestion() string {
	for _, line := range r.Script.GumLog {
		if strings.HasPrefix(line, "gum choose --header Role ") {
			return line
		}
	}
	return ""
}

func (r *rig) lxcRoleLine() string {
	for _, c := range r.Fake.Calls() {
		if strings.Contains(c.Line(), "helpers.agent_bus.declare lxc-role ") {
			return c.Line()
		}
	}
	return ""
}

const declaredCoordinator = "LXCMEMBER\talpha\tcontainer-b\tagent\tworker\tcoordinator\tpresent\n"

func TestOnATeamWithACoordinatorWorkerIsTheOrdinaryChoiceAndNothingChangesSilently(t *testing.T) {
	takeover := coordinator.TakeoverChoice(coordinator.Holder{Handle: "agent@container-b", Running: coordinator.Unknown})
	for name, answers := range map[string][]string{
		"worker":             {choose("worker"), "YES"},
		"takeover, then no":  {choose(takeover), "NO", "YES"},
		"takeover, then Esc": {choose(takeover), "CANCEL", "YES"},
	} {
		r := newRig(t)
		r.OnDeclare("lxc-members", declaredCoordinator)
		r.answers(append([]string{choose("container-a"), choose("agent"), choose(oneRow)}, answers...)...)
		if code := r.run("alpha"); code != term.ExitOK {
			t.Fatalf("%s: exit %d: %s", name, code, r.ErrOut.String())
		}
		if q := r.roleQuestion(); !strings.Contains(q, "--selected worker worker "+takeover) {
			t.Fatalf("%s: role question %q", name, q)
		}
		if strings.Contains(r.addLine(), "--role coordinator") || r.lxcRoleLine() != "" || len(r.setRoles) != 0 {
			t.Fatalf("%s: a role changed: add %q, lxc-role %q, set-role %v", name, r.addLine(), r.lxcRoleLine(), r.setRoles)
		}
	}
}

func TestTheTakeoverOfADeclaredCoordinatorMakesItsEntryAWorkerFirst(t *testing.T) {
	r := newRig(t)
	r.OnDeclare("lxc-members", declaredCoordinator)
	r.OnDeclare("lxc-role", "CHANGED\tLXC member agent@container-b in team alpha is now worker, in "+r.Host.HostVars+"\n")
	h := coordinator.Holder{Handle: "agent@container-b", Running: coordinator.Unknown}
	r.answers(choose("container-a"), choose("agent"), choose(oneRow), choose(coordinator.TakeoverChoice(h)), "YES", "YES")
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	confirm := "gum confirm --default=false " + coordinator.OfferTakeover("alpha", "agent@container-a", h)
	r.Script.HeadingAt(3, roleHeading)
	r.Script.HeadingAt(4, roleHeading)
	if !slices.Contains(r.Script.GumLog, confirm) {
		t.Fatalf("no takeover question %q in %q", confirm, r.Script.GumLog)
	}
	if !strings.HasSuffix(r.lxcRoleLine(), "lxc-role --file "+r.Host.HostVars+" --team alpha --container container-b --user agent --role worker") {
		t.Fatalf("lxc-role %q", r.lxcRoleLine())
	}
	calls := r.Fake.Calls()
	at := func(want string) int {
		return slices.IndexFunc(calls, func(c execx.Cmd) bool {
			return strings.Contains(c.Line(), want) && !strings.HasSuffix(c.Line(), "--dry-run")
		})
	}
	if role, add := at("declare lxc-role "), at("declare lxc-add "); role < 0 || add < role || !strings.Contains(r.addLine(), "--role coordinator") {
		t.Fatalf("the old coordinator is made a worker before the new one is added: %d %d %q", role, add, r.addLine())
	}
}

func TestTheTakeoverOfALiveCoordinatorDemotesItBeforeTheEntryIsWritten(t *testing.T) {
	r := newRig(t)
	r.roster = agentbus.Roster{Members: []agentbus.Member{{Handle: "proj.1+local.podman", Role: agentbus.RoleCoordinator}}}
	h := coordinator.Holder{Handle: "proj.1+local.podman", Running: coordinator.Unknown}
	r.answers(choose("container-a"), choose("agent"), choose(oneRow), choose(coordinator.TakeoverChoice(h)), "YES", "YES")
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if !slices.Equal(r.setRoles, []string{"alpha proj.1+local.podman worker"}) {
		t.Fatalf("set-role %v", r.setRoles)
	}
	if !strings.Contains(r.addLine(), "--role coordinator") || !slices.Equal(r.events, []string{"set-role", "apply"}) {
		t.Fatalf("add %q events %v", r.addLine(), r.events)
	}
}

func TestATakeoverWhoseEntryCannotBeWrittenPutsTheCoordinatorBack(t *testing.T) {
	r := newRig(t)
	r.roster = agentbus.Roster{Members: []agentbus.Member{{Handle: "proj.1+local.podman", Role: agentbus.RoleCoordinator}}}
	r.Fake.OnFunc(strings.Fields(r.DeclarePrefix("lxc-add")), func(c execx.Cmd) (execx.Result, error) {
		if slices.Contains(c.Args, "--dry-run") {
			return execx.Result{}, nil
		}
		return execx.Result{ExitCode: 70}, &execx.ExitError{Name: "python3", Code: 70, Stderr: "declare: refused"}
	})
	h := coordinator.Holder{Handle: "proj.1+local.podman", Running: coordinator.Unknown}
	r.answers(choose("container-a"), choose("agent"), choose(oneRow), choose(coordinator.TakeoverChoice(h)), "YES", "YES")
	if code := r.run("alpha"); code == term.ExitOK {
		t.Fatalf("a failed write succeeded: %s", r.ErrOut.String())
	}
	if !slices.Equal(r.setRoles, []string{"alpha proj.1+local.podman worker", "alpha proj.1+local.podman coordinator"}) {
		t.Fatalf("set-role %v", r.setRoles)
	}
}

func TestTheUserIsSuggestedFromTheContainerAndAskedAgainWhenRefused(t *testing.T) {
	r := newRig(t)
	r.lxc.users = []string{"agent", "other"}
	r.answers(choose("container-a"), choose("Bad User"), choose("agent"), choose(oneRow), choose("worker"), "YES")
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
	r.answers(choose("container-a"), choose("agent"), choose(oneRow), choose(oneRow), choose(oneRow))
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
	r.answers(choose("container-a"), choose("agent"), choose(oneRow), choose("worker"), "NO")
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
	for _, want := range []string{"is declared in", "ccy-teams apply alpha", "gh auth login", "lxc-attach -P /var/lib/lxc -n container-a", "agent"} {
		if !strings.Contains(said, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, said)
		}
	}
	if strings.Contains(said, "agent-bus-claude") || slices.Contains(r.lxc.reads, "sessions container-a agent -") {
		t.Fatalf("the sessions step is offered before the member is applied:\n%s", said)
	}
}

// The member is declared and the team applied in one hold of the play lock.
func TestTheMemberIsDeclaredAndAppliedInOneHoldOfThePlayLock(t *testing.T) {
	r := newRig(t)
	r.happy()
	heldAtAdd := false
	r.Fake.OnFunc(strings.Fields(r.DeclarePrefix("lxc-add")), func(c execx.Cmd) (execx.Result, error) {
		if !slices.Contains(c.Args, "--dry-run") {
			heldAtAdd = r.PlayLockHeld(t)
		}
		return execx.Result{Stdout: []byte("CHANGED\tLXC member agent@container-a in team alpha\n")}, nil
	})
	if code := r.run("alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if !heldAtAdd || r.PlayLockHeld(t) {
		t.Fatalf("held at the add %v, held after %v", heldAtAdd, r.PlayLockHeld(t))
	}
}

// The owner's defect: an add while another play run holds the lock. The wait is a screen naming
// the holder, and stopping it declares nothing and applies nothing.
func TestStoppingTheWaitForThePlayLockAddsNothing(t *testing.T) {
	r := newRig(t)
	r.happy()
	r.HoldPlayLock(t)
	r.Script.Fzf = []string{"|EXIT"}
	if code := r.run("alpha"); code != term.ExitCancelled {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if r.addLine() != "" || len(r.applied) != 0 || len(r.setRoles) != 0 {
		t.Fatalf("add %q, applied %v, set-role %v", r.addLine(), r.applied, r.setRoles)
	}
	if want := "as the play lock was not free (above). Once the other play run has finished, run ccy-teams lxc-add alpha again."; !strings.Contains(r.ErrOut.String(), want) {
		t.Fatalf("stderr lacks %q:\n%s", want, r.ErrOut.String())
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
	r.a.SetRole = func(_ context.Context, team, handle, role string) error {
		r.setRoles = append(r.setRoles, team+" "+handle+" "+role)
		r.events = append(r.events, "set-role")
		return nil
	}
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
	if !strings.Contains(said, "is a member of alpha as proj.2+worker.lxc (container container-a, user agent).") ||
		strings.Contains(said, "as "+handle+" ") {
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
	if said := r.ErrOut.String(); !strings.Contains(said, "is a member of alpha as proj.2+worker.lxc (container container-a, user agent).") {
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
	r.answers(choose("container-a"), choose("agent"), choose(oneRow), choose("worker"), "NO")
	if code := r.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if shown := strings.Join(r.Script.Styled, "\n"); !strings.Contains(shown, "Handle (expected)") ||
		!strings.Contains(shown, handle+" (container container-a, user agent)") {
		t.Fatalf("the summary does not say the handle is expected, with its container and user:\n%s", shown)
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

// Fable's N2: an apply that fails after a live takeover says the old coordinator is a worker now.
func TestAnApplyThatFailsAfterATakeoverSaysTheOldCoordinatorIsAWorker(t *testing.T) {
	r := newRig(t)
	r.roster = agentbus.Roster{Members: []agentbus.Member{{Handle: "proj.1+local.podman", Role: agentbus.RoleCoordinator}}}
	r.a.ApplyTeams = func(context.Context, ...string) error { return errors.New("play failed") }
	h := coordinator.Holder{Handle: "proj.1+local.podman", Running: coordinator.Unknown}
	r.answers(choose("container-a"), choose("agent"), choose(oneRow), choose(coordinator.TakeoverChoice(h)), "YES", "YES")
	if code := r.run("alpha"); code != term.ExitFailure {
		t.Fatalf("exit %d: %s", code, r.ErrOut.String())
	}
	if want := "proj.1+local.podman is a worker now, so alpha has no coordinator until ccy-teams apply alpha succeeds."; !strings.Contains(r.ErrOut.String(), want) {
		t.Fatalf("stderr lacks %q:\n%s", want, r.ErrOut.String())
	}
}
