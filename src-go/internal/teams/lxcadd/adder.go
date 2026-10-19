// Package lxcadd is "ccy-teams lxc-add <team>": an agent user of an existing LXC container
// joins a team without anyone editing the host_vars. It lists the host's running containers,
// asks for the user, the checkout and the role (each answer checked by declare.py as it is
// given), shows a summary with the member's handle and asks once, then writes the
// agent_bus_lxc_members entry through declare.py and applies the team, which runs the play
// that joins the member. What is left to do inside the container is printed at the end.
//
// The handle's repo, host and seat are never asked: they are derived from the checkout as ccy
// derives them for a podman seat (helpers/agent_bus_lxc/derive.py is the one rule). The checkout
// is read from this host's side, as root, through the container's LXC config: the host reads
// plain files, runs no git, follows no link, and enters nothing.
package lxcadd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/lxcfreeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/playlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/coordinator"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/lxcrecord"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui/pathinput"
)

const (
	// maxTries is how often a refused answer is asked again before the run ends.
	maxTries = 3

	playGum = "playbooks/imports/play-claude-yolo.yml"

	// readWhy is what sudo is asked for: LXC is rootful on this host.
	readWhy = "LXC here is rootful, so listing the containers and reading who lives in one is done as root."

	// rootReader is the declare.py script that reads a container's checkout from the host.
	rootReader = "root-read"

	// readTimeout bounds one read of the container's tree; the home search stops itself sooner.
	readTimeout = 20 * time.Second

	roleWorker      = "worker"
	roleCoordinator = "coordinator"
	usageText       = "'lxc-add' takes one team: ccy-teams lxc-add <team>."
)

// Containers is the host's LXC: which containers there are and who lives in a running one.
// *lxcfreeze.Lister is the real one.
type Containers interface {
	Containers(ctx context.Context) (running, stopped []string, err error)
	Users(ctx context.Context, container string) ([]string, error)
	// LxcPath is where root's LXC keeps its containers.
	LxcPath(ctx context.Context) (string, error)
	// ReadCheckout runs the host-side reader script as root (see lxcfreeze.Lister.ReadCheckout).
	ReadCheckout(ctx context.Context, script, mode, container, user, path string) (string, error)
}

// stop ends the run with an exit code; whatever the person needs to read has been printed.
type stop struct{ code term.ExitCode }

func (s *stop) Error() string { return fmt.Sprintf("stopped with exit %d", s.code) }

// Adder is "ccy-teams lxc-add". Every dependency is a field so a test can stand in for each.
type Adder struct {
	Declare *declare.Client
	Gum     *ui.Gum
	// Paths is the line the checkout's path is typed in, Tab completing it.
	Paths  PathAsker
	Lister Containers
	// ReadTimeout bounds each read of the container's tree: the home search, a directory listing.
	ReadTimeout time.Duration
	// Roster is "agent-bus list <team>" as root: the live coordinator is read from it.
	Roster func(ctx context.Context, team string) (agentbus.Roster, error)
	// SetRole is "agent-bus set-role" as root: a takeover makes a live coordinator a worker.
	SetRole func(ctx context.Context, team, handle, role string) error
	// SudoReady makes root available, saying why it is wanted; false means it has said why not.
	SudoReady  func(ctx context.Context, why string) bool
	ApplyTeams func(ctx context.Context, teams ...string) error
	// Locked is ops.Locked: fn in one hold of the play lock, waited for first. The member is
	// declared and the team applied in it, so a wait that does not end in the lock adds nothing.
	Locked func(ctx context.Context, purpose string, fn func(context.Context) error) error
	// NewTerm is a Term of its own for each run: the step counters live in it.
	NewTerm func() *term.Term
	// LookPath finds a tool on PATH.
	LookPath func(name string) (string, error)

	Stdin  io.Reader // must be a terminal: the questions are asked there
	Stdout io.Writer // the CHANGED line declare.py prints

	HostVars string
	Home     string // for the ~ abbreviation
	// AppliedDir holds the applied stamps and the LXC member record the play writes.
	AppliedDir string

	term *term.Term
	// head is the heading of the question being asked, and asked the steps so far.
	head  ui.Heading
	asked int
	// script is the host-side reader, fetched from declare.py once per run; roster is the
	// team's members as declare.py reads them, for the seat.
	script, roster string
	// before is the team as it was when the questions began: what the apply is read back against.
	before agentbus.Roster
	// lxcPath is where root's LXC keeps its containers, for the lxc-attach hints.
	lxcPath string
}

// New is the Adder over a host and the flows it hands the work to: apply joins the member.
func New(h *host.Host, kit *flow.Kit, o *ops.Ops) *Adder {
	return &Adder{
		Declare: h.Declare, Gum: h.Gum, Paths: &pathinput.Asker{In: h.Stdin, Out: h.Stderr}, ReadTimeout: readTimeout, Lister: &lxcfreeze.Lister{Sudo: h.Sudo, LookPath: h.LookPath},
		Roster: h.Bus.List, SetRole: h.Bus.SetRole, SudoReady: kit.SudoReady,
		ApplyTeams: func(ctx context.Context, teams ...string) error { return o.ApplyTeams(ctx, teams) },
		Locked:     o.Locked,
		NewTerm:    h.FreshTerm, LookPath: h.LookPath, Stdin: h.Stdin, Stdout: h.Stdout,
		HostVars: h.HostVars, Home: h.Home, AppliedDir: h.AppliedDir,
	}
}

// answers is what the questions collect.
type answers struct {
	team, container, user, checkout, role string
	// takeover is the coordinator this member replaces, when the person chose to; empty
	// otherwise.
	takeover holding
	// derived is the handle the checkout gives, shown before the yes and never written: the
	// entry carries neither a host nor a seat, so the play derives them again.
	derived declare.Derived
}

func (a answers) entry() declare.LxcEntry {
	e := declare.LxcEntry{Team: a.team, Container: a.container, User: a.user, Checkout: a.checkout}
	if a.role == roleCoordinator {
		e.Role = a.role
	}
	return e
}

func (a answers) label() string { return a.user + "@" + a.container }

// addCrumb is the crumb of every question that adds an LXC member.
const addCrumb = "Add LXC member"

// context is the answers so far on one line, for the heading: the team, then what is chosen.
func (a answers) context() string {
	parts := []string{"into team " + a.team}
	if a.container != "" {
		parts = append(parts, "container "+a.container)
	}
	if a.user != "" {
		parts = append(parts, "user "+a.user)
	}
	if a.checkout != "" {
		parts = append(parts, "checkout "+a.checkout)
	}
	return strings.Join(parts, " · ")
}

// where2 names the container and user beside a handle, so the owner never has to look them up.
func (a answers) where2() string { return "(container " + a.container + ", user " + a.user + ")" }

// Run is ccy-teams lxc-add with the arguments after "lxc-add". It returns the exit code:
// 0 done, 1 refused or failed, 64 usage, 130 cancelled, or declare.py's own refusal code.
func (a *Adder) Run(ctx context.Context, args []string) term.ExitCode {
	a.term = a.NewTerm()
	a.asked = 0
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return a.usage(usageText)
	}
	err := a.add(ctx, args[0])
	if err == nil {
		return term.ExitOK
	}
	if s, ok := errors.AsType[*stop](err); ok {
		return s.code
	}
	if de, ok := errors.AsType[*declare.Error](err); ok {
		a.term.Printf("%s\n", de.Error())
		return term.ExitCode(de.Code)
	}
	a.term.PrintError(err.Error())
	return term.ExitFailure
}

func (a *Adder) usage(msg string) term.ExitCode {
	a.term.Printf("ccy-teams: %s See ccy-teams --help.\n", msg)
	return term.ExitUsage
}

func (a *Adder) failed(msg string) error {
	a.term.PrintError(msg)
	return &stop{term.ExitFailure}
}

func (a *Adder) cancelled() error {
	a.term.Printf("Cancelled; nothing was changed.\n")
	return &stop{term.ExitCancelled}
}

// gumErr is a gum question's error: leaving it is the run's cancellation.
func (a *Adder) gumErr(err error) error {
	if errors.Is(err, ui.ErrCancelled) {
		return a.cancelled()
	}
	return err
}

func (a *Adder) add(ctx context.Context, team string) error {
	if err := a.prepare(ctx, team); err != nil {
		return err
	}
	running, stopped, err := a.listContainers(ctx)
	if err != nil {
		return err
	}
	if a.lxcPath, err = a.Lister.LxcPath(ctx); err != nil {
		return a.failed(err.Error())
	}
	if a.script, err = a.Declare.LxcScript(ctx, rootReader); err != nil {
		return err
	}
	roster, err := a.teamRoster(ctx, team)
	if err != nil {
		return err
	}
	declared, err := a.Declare.LxcMembers(ctx)
	if err != nil {
		return err
	}
	held := coordinatorOf(team, roster, declared)
	ans := answers{team: team, role: roleWorker}
	a.term.Steps = 4
	if err = a.ask(ctx, &ans, running, held); err != nil {
		return err
	}
	a.sayStopped(team, declared, stopped)
	if err = a.summary(ctx, ans); err != nil {
		return err
	}
	return a.write(ctx, ans)
}

// prepare is what the questions need before the first is asked: a terminal, gum, and a team
// that is declared present.
func (a *Adder) prepare(ctx context.Context, team string) error {
	if !a.term.IsTerminal(a.Stdin) || !a.term.IsTerminal(a.term.Err) {
		return &stop{a.usage("'lxc-add' asks its questions on a terminal.")}
	}
	if _, err := a.LookPath("gum"); err != nil {
		return a.failed("gum is not installed, and ccy-teams needs it here. " + playGum + " installs it; run that play and try again.")
	}
	teams, err := a.Declare.Declared(ctx)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(teams, func(t declare.Team) bool { return t.Name == team && t.Present }) {
		return &stop{a.usage("team " + team + " is not declared present in " + term.AbbrevHome(a.HostVars, a.Home) +
			"; ccy-teams create makes a team.")}
	}
	return nil
}

// listContainers is the running and the stopped containers, asking for root first. Only a
// running one is offered: the play reaches into it with lxc-attach.
func (a *Adder) listContainers(ctx context.Context) ([]string, []string, error) {
	if !a.SudoReady(ctx, readWhy) {
		return nil, nil, a.failed("so the containers cannot be listed, and nothing was changed.")
	}
	running, stopped, err := a.Lister.Containers(ctx)
	if err != nil {
		return nil, nil, a.failed(err.Error())
	}
	if len(running) == 0 {
		msg := "there is no running LXC container on this host to add."
		if len(stopped) > 0 {
			msg += " Stopped, so not offered: " + strings.Join(stopped, ", ") + ". Start one, then run this again."
		}
		return nil, nil, a.failed(msg)
	}
	if len(stopped) > 0 {
		a.term.Printf("Not offered, as they are not running: %s.\n", strings.Join(stopped, ", "))
	}
	return running, stopped, nil
}

// sayStopped names the team's declared present members whose containers are stopped, so the
// owner knows before the yes that the play holds them back (skipped when joined, deferred when
// not) and that they do not stop this add (SPEC-lxc-stopped-members.md L11). Nothing is started.
func (a *Adder) sayStopped(team string, declared []declare.LxcMember, stopped []string) {
	var named []string
	for _, m := range declared {
		if m.Team == team && m.State == "present" && slices.Contains(stopped, m.Container) {
			named = append(named, m.User+"@"+m.Container)
		}
	}
	if len(named) == 0 {
		return
	}
	a.term.Printf("Declared in %s, container not running: %s. The play skips a member already joined and defers "+
		"one not yet joined; neither blocks this add. Nothing is started.\n", team, strings.Join(named, ", "))
}

// deferred is why the last apply deferred the new member, when its record says so (its
// container stopped between the question and the play); "" when it was not deferred.
func (a *Adder) deferred(ans answers) (string, error) {
	rec, found, err := lxcrecord.Read(a.AppliedDir, ans.team)
	if err != nil || !found {
		return "", err
	}
	item, ok := rec.Find(ans.container, ans.user)
	if !ok || item.Status != lxcrecord.StatusDeferred {
		return "", nil
	}
	return item.Why, nil
}

// holding says who holds the team's coordinator role, from the declaration and from the live
// team: a declared LXC member (its entry), or a member of the live team (its roster line).
type holding struct {
	holder   string // empty: the team has none
	declared *declare.LxcMember
	member   agentbus.Member
}

// named is the holder as the takeover names it.
func (h holding) named() coordinator.Holder {
	running := coordinator.Unknown
	if h.declared == nil {
		running = coordinator.FromSeats(nil)(h.member)
	}
	return coordinator.Holder{Handle: h.holder, Running: running}
}

// teamRoster is the running team's members, which both the role and the seat depend on: the
// seat is the lowest number no active member of the same repo and host holds. A team whose
// members cannot be read stops the run, naming the way on.
func (a *Adder) teamRoster(ctx context.Context, team string) (agentbus.Roster, error) {
	roster, err := a.Roster(ctx, team)
	if err != nil {
		return agentbus.Roster{}, a.failed("the members of " + team + " could not be read (" + err.Error() + "), and the seat " +
			"is chosen among theirs. If " + team + " has not been applied yet, ccy-teams apply " + team + " first; nothing was changed.")
	}
	parts := make([]string, 0, len(roster.Members))
	for _, m := range roster.Members {
		state := "active"
		if m.Parked {
			state = "parked"
		}
		parts = append(parts, m.Handle+"="+state)
	}
	a.roster = strings.Join(parts, ",")
	a.before = agentbus.Roster{Members: slices.Clone(roster.Members)}
	return roster, nil
}

// coordinatorOf reads the declared LXC members first (a present coordinator there is the one
// the play will set), then the live roster.
func coordinatorOf(team string, roster agentbus.Roster, declared []declare.LxcMember) holding {
	for _, m := range declared {
		if m.Team == team && m.State == "present" && m.Role == agentbus.RoleCoordinator {
			return holding{holder: m.User + "@" + m.Container, declared: &m}
		}
	}
	if m, ok := roster.Coordinator(); ok {
		return holding{holder: m.Handle, member: m}
	}
	return holding{}
}

func (a *Adder) ask(ctx context.Context, ans *answers, running []string, held holding) error {
	if err := a.askContainer(ctx, ans, running); err != nil {
		return err
	}
	if err := a.askUser(ctx, ans); err != nil {
		return err
	}
	if err := a.askCheckout(ctx, ans); err != nil {
		return err
	}
	return a.askRole(ctx, ans, held)
}

// step starts the next question: its numbered title and hint on the terminal, and the heading
// its prompt carries (the team, the step, and the answers so far).
func (a *Adder) step(ctx context.Context, ans *answers, title, hint string) error {
	a.asked++
	a.head = flow.Here(ans.team, addCrumb).AtStep(a.asked, a.term.Steps, title).About(ans.context())
	a.term.Step(title)
	return a.Gum.Explain(ctx, hint, 5)
}

func (a *Adder) askContainer(ctx context.Context, ans *answers, running []string) error {
	if err := a.step(ctx, ans, "Container", "The LXC container whose agent joins "+ans.team+". It must already exist and be running; this never makes one."); err != nil {
		return err
	}
	choice, err := a.Gum.Choose(ctx, a.head, "Container", running[0], running)
	if err != nil {
		return a.gumErr(err)
	}
	ans.container = choice
	a.term.Answered(choice)
	return nil
}

func (a *Adder) askUser(ctx context.Context, ans *answers) error {
	users, err := a.Lister.Users(ctx, ans.container)
	suggestion, hint := "", "The login user the agent runs as in the container, which must already exist."
	switch {
	case err != nil:
		a.term.Printf("%s\n", err.Error())
	case len(users) > 0:
		suggestion = users[0]
		hint += " It has: " + strings.Join(users, ", ") + "."
	}
	if err = a.step(ctx, ans, "User", hint); err != nil {
		return err
	}
	user, err := a.askValid(ctx, func(ctx context.Context, answer string) (bool, error) {
		if answer == "" {
			a.problem("name the user", true)
			return false, nil
		}
		return a.checked(a.Declare.LxcCheck(ctx, declare.LxcEntry{Team: ans.team, Container: ans.container, User: answer}))
	}, "User in "+ans.container, "e.g. agent", suggestion)
	if err != nil {
		return err
	}
	ans.user = user
	a.term.Answered(user)
	return nil
}

// checkoutOK judges a typed checkout: its form, then the checkout itself, read from the host's side
// and turned into the handle it gives. Whatever cannot be read or derived is said and the path asked again.
func (a *Adder) checkoutOK(ctx context.Context, ans *answers, path string) (bool, error) {
	if ok, err := a.checked(a.Declare.LxcCheck(ctx, declare.LxcEntry{Checkout: path})); !ok || err != nil {
		return false, err
	}
	probe, err := a.Lister.ReadCheckout(ctx, a.script, "probe", ans.container, ans.user, path)
	if err != nil {
		a.problem(err.Error(), true)
		return false, nil
	}
	who := declare.LxcEntry{Team: ans.team, Container: ans.container, User: ans.user}
	derived, err := a.Declare.LxcDerive(ctx, who, probe, a.roster)
	if ok, cerr := a.checked(err); !ok || cerr != nil {
		return false, cerr
	}
	ans.derived = derived
	return true, nil
}

// askRole asks the role in one of the two coordinator paths (package coordinator): worker, the
// ordinary choice, first; then the coordinator's role offered plainly on a team with none, or,
// on a team that has one, the takeover as its own choice, which asks again, on No. The role is
// never changed without a question.
func (a *Adder) askRole(ctx context.Context, ans *answers, held holding) error {
	duties := "A worker does the work it is given. The coordinator takes the humans' requests, dispatches them to the workers and is copied on what workers tell humans; "
	coord := coordinator.NoneChoice(ans.team)
	if held.holder == "" {
		duties += ans.team + " has none yet."
	} else {
		duties += ans.team + "'s coordinator is " + held.named().Named() + ", and a team has one."
		coord = coordinator.TakeoverChoice(held.named())
	}
	if err := a.step(ctx, ans, "Role", duties); err != nil {
		return err
	}
	role, err := a.Gum.Choose(ctx, a.head, "Role", roleWorker, []string{roleWorker, coord})
	if err != nil {
		return a.gumErr(err)
	}
	if role != coord {
		a.term.Answered(roleWorker)
		return nil
	}
	if held.holder != "" {
		yes, err := a.Gum.Confirm(ctx, a.head, coordinator.OfferTakeover(ans.team, ans.label(), held.named()), false)
		if err != nil && !errors.Is(err, ui.ErrCancelled) { // Esc is No (Fable's N3)
			return err
		}
		if !yes {
			a.term.Printf("%s stays %s's coordinator; %s joins as a worker.\n", held.holder, ans.team, ans.label())
			a.term.Answered(roleWorker)
			return nil
		}
		ans.takeover = held
	}
	ans.role = roleCoordinator
	a.term.Answered(roleCoordinator)
	return nil
}

// checker accepts an answer, or says what is wrong with it and refuses it. An error is a
// failure to check at all, which ends the run.
type checker func(ctx context.Context, answer string) (bool, error)

// askValid asks until check accepts the answer, maxTries at most. A refused answer comes back
// as the next try's value, as typed.
func (a *Adder) askValid(ctx context.Context, check checker, header, placeholder, value string) (string, error) {
	for range maxTries {
		answer, err := a.Gum.Input(ctx, a.head, header, placeholder, value)
		if err != nil {
			return "", a.gumErr(err)
		}
		good, err := check(ctx, answer)
		if err != nil {
			return "", err
		}
		if good {
			return answer, nil
		}
		value = answer
	}
	a.term.Printf("Giving up after %d tries; nothing was changed.\n", maxTries)
	return "", &stop{term.ExitFailure}
}

// problem says why an answer was refused, as a person would.
func (a *Adder) problem(msg string, tryAgain bool) {
	msg = strings.TrimPrefix(msg, "declare: ")
	if tryAgain {
		a.term.Printf("  %s. Try again.\n", msg)
		return
	}
	a.term.Printf("  %s.\n", msg)
}

// checked is the outcome of a declare.py check: accepted, refused (said, and false), or a
// failure to run it (an error).
func (a *Adder) checked(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	de, ok := errors.AsType[*declare.Error](err)
	if !ok {
		return false, err
	}
	a.problem(de.Error(), true)
	return false, nil
}

// summary checks the whole entry as one (a second coordinator, a duplicate), shows what will
// be written, and asks the one yes that covers writing it and applying the team.
func (a *Adder) summary(ctx context.Context, ans answers) error {
	check := ans.entry()
	if ans.takeover.declared != nil {
		// The declared coordinator is made a worker first; until then a second one is refused.
		check.Role = ""
	}
	if _, err := a.Declare.LxcAddDryRun(ctx, check); err != nil {
		if de, ok := errors.AsType[*declare.Error](err); ok {
			a.term.Printf("%s\nRefused; nothing was changed.\n", de.Error())
			return &stop{term.ExitCode(de.Code)}
		}
		return err
	}
	body := strings.Join([]string{
		line("Container", ans.container),
		line("User", ans.user),
		line("Checkout", ans.checkout),
		line("Handle (expected)", ans.derived.Handle+" "+ans.where2()),
		line("Role", roleLine(ans)),
	}, "\n")
	a.term.Printf("\n")
	if err := a.Gum.Box(ctx, false, ans.label()+" joins "+ans.team, "", body); err != nil {
		return err
	}
	a.term.Printf("The expected handle is derived from the checkout as ccy derives it: repo %s, host %s, seat %s. "+
		"The play makes the final choice and the result is reported after it.\n",
		ans.derived.Repo, ans.derived.Host, ans.derived.Seat)
	question := "Add " + ans.label() + " to " + ans.team + "? (saved to " + term.AbbrevHome(a.HostVars, a.Home) +
		", then " + ans.team + " is applied: sudo asks for your password)"
	head := flow.Here(ans.team, addCrumb, "Review").About(ans.label() + " joins team " + ans.team + " · role " + ans.role)
	yes, err := a.Gum.Confirm(ctx, head, question, true)
	if err != nil {
		return a.gumErr(err)
	}
	if !yes {
		a.term.Printf("Nothing was added.\n")
		return &stop{term.ExitFailure}
	}
	return nil
}

func line(label, value string) string { return fmt.Sprintf("%-18s %s", label, value) }

// roleLine is the summary's role: a takeover names who becomes a worker.
func roleLine(ans answers) string {
	if ans.takeover.holder != "" {
		return ans.role + " (" + ans.takeover.holder + " stops being " + ans.team + "'s coordinator and becomes a worker)"
	}
	return ans.role
}

// demote is a takeover's first step: the current coordinator made a worker, and how to put it
// back. With no takeover it does nothing.
func (a *Adder) demote(ctx context.Context, ans answers) (func(context.Context), error) {
	held := ans.takeover
	switch {
	case held.holder == "":
		return func(context.Context) {}, nil
	case held.declared != nil:
		if _, err := a.Declare.LxcSetRole(ctx, *held.declared, roleWorker); err != nil {
			return nil, err
		}
		return func(ctx context.Context) {
			if _, err := a.Declare.LxcSetRole(ctx, *held.declared, roleCoordinator); err != nil {
				a.term.PrintError(held.holder + " was made a worker in the host_vars and could not be put back (" + err.Error() +
					"): ccy-teams lxc-add " + ans.team + " again, or its entry's role, makes a coordinator")
			}
		}, nil
	}
	if !a.SudoReady(ctx, "A role is changed as root, by agent-bus.") {
		return nil, a.failed("so " + held.holder + " stays the coordinator, and nothing was changed.")
	}
	if err := a.SetRole(ctx, ans.team, held.holder, roleWorker); err != nil {
		return nil, a.failed(held.holder + " could not be made a worker (" + err.Error() + "); nothing was changed.")
	}
	return func(ctx context.Context) {
		if err := a.SetRole(ctx, ans.team, held.holder, roleCoordinator); err != nil {
			a.term.PrintError(held.holder + " was made a worker and could not be made the coordinator again (" + err.Error() +
				"): sudo agent-bus set-role " + ans.team + " " + held.holder + " --role=coordinator")
		}
	}, nil
}

// joined says the handle the play gave the member, read back from the team after the apply: the one
// active handle of the derived repo and host that was not active before. The summary's handle is only
// a preview (it cannot see bundles or the entries still to be derived), so when the read-back cannot
// name exactly one it is said to be the expected handle, never the result.
func (a *Adder) joined(ctx context.Context, ans answers) string {
	d := ans.derived
	unsure := func(why string) string {
		return ans.label() + " is joined to " + ans.team + ", but " + why + ": the expected handle " + d.Handle +
			" is the summary's preview, so check the handle in ccy-teams --list."
	}
	roster, err := a.Roster(ctx, ans.team)
	if err != nil {
		return unsure("the team's members could not be read back (" + err.Error() + ")")
	}
	was := map[string]bool{}
	for _, m := range a.before.Members {
		was[m.Handle] = !m.Parked
	}
	var fresh []string
	for _, m := range roster.Members {
		if !m.Parked && !was[m.Handle] && strings.HasPrefix(m.Handle, d.Repo+".") && strings.HasSuffix(m.Handle, "+"+d.Host+".lxc") {
			fresh = append(fresh, m.Handle)
		}
	}
	if len(fresh) != 1 {
		return unsure(fmt.Sprintf("%d new active members of %s on %s were found", len(fresh), d.Repo, d.Host))
	}
	return ans.label() + " is a member of " + ans.team + " as " + fresh[0] + " " + ans.where2() + "."
}

// write declares the member and applies the team in one hold of the play lock, and says what is
// left to do in the container.
func (a *Adder) write(ctx context.Context, ans answers) error {
	err := a.Locked(ctx, "add "+ans.label()+" to team "+ans.team, func(ctx context.Context) error {
		return a.declareAndApply(ctx, ans)
	})
	if errors.Is(err, playlock.ErrNotTaken) {
		a.term.PrintError("nothing was changed: " + ans.label() + " is not added to " + ans.team +
			", as the play lock was not free (above). Once the other play run has finished, run ccy-teams lxc-add " + ans.team + " again.")
		return &stop{ops.LockExit(err)}
	}
	if err != nil {
		return err
	}
	why, err := a.deferred(ans)
	if err != nil {
		return a.failed(err.Error())
	}
	if why != "" {
		a.term.Say(ans.label() + " is declared in " + ans.team + " but deferred, not joined: " + why)
		return nil
	}
	a.term.Say(a.joined(ctx, ans))
	a.term.Say(a.session(ctx, ans))
	a.term.Printf("Inside the container (%s):\n", a.attach(ans))
	a.term.Printf("  Agent    in %s: %s (the kit's launcher), or your project's launcher if it supports it "+
		"(ccy --teams, with no value)\n", ans.checkout, launcher)
	a.term.Printf("  Check    set -a; . ~/.config/pingbus/env; set +a; pingbus config check && pingbus history --limit 1"+
		"   (reads %s's room live, as this member)\n", ans.team)
	a.term.Printf("  GitHub   the play checked that %s is logged in to GitHub there, and agent-bus-claude refuses to start "+
		"until it is; if it is ever logged out: gh auth login --hostname github.com, as %s.\n", ans.user, ans.user)
	return nil
}

// declareAndApply declares the member and applies the team. A takeover first makes the current
// coordinator a worker (its entry, or its live role), and puts it back when the new entry cannot
// be written.
func (a *Adder) declareAndApply(ctx context.Context, ans answers) error {
	undo, err := a.demote(ctx, ans)
	if err != nil {
		return err
	}
	change, err := a.Declare.LxcAdd(ctx, ans.entry())
	if err != nil {
		undo(ctx)
		return err
	}
	marker := "UNCHANGED"
	if change.Changed {
		marker = "CHANGED"
	}
	if _, err = fmt.Fprintf(a.Stdout, "%s\t%s\n", marker, change.Text); err != nil {
		return fmt.Errorf("write the result: %w", err)
	}
	if err = a.ApplyTeams(ctx, ans.team); err != nil {
		if ans.takeover.holder != "" {
			a.term.Printf("%s is a worker now, so %s has no coordinator until ccy-teams apply %s succeeds.\n",
				ans.takeover.holder, ans.team, ans.team)
		}
		return a.failed(ans.label() + " is declared in " + term.AbbrevHome(a.HostVars, a.Home) + " but not joined. " +
			"If the cause above is about gh, log in inside the container as " + ans.user + ":\n" +
			"  " + a.attach(ans) + "     (then gh auth login, and exit)\n" +
			"Once the cause is fixed, ccy-teams apply " + ans.team + " joins it.")
	}
	return nil
}

// attach is the command that opens a login shell as the member user in its container.
func (a *Adder) attach(ans answers) string {
	return "sudo lxc-attach -P " + a.lxcPath + " -n " + ans.container + " -- su - " + ans.user
}

// launcher is the command the kit installs in every member container to start a session on the
// bus (README.lxc). A client project's own ccy inside the container is not ours to name.
const launcher = "agent-bus-claude"

// startedLayout is how a session's start is shown.
const startedLayout = "Mon 2 Jan 15:04"

// session says whether a Claude session of the member is already running in the container: one
// started before the add has not read the new team, so it must be started again. The processes
// are read from the host's side, as the checkout is, never by entering the container.
func (a *Adder) session(ctx context.Context, ans answers) string {
	start := launcher + ", as " + ans.user + " in " + ans.checkout
	started, err := a.sessions(ctx, ans)
	switch {
	case err != nil:
		return "Whether a Claude session for " + ans.user + " is running in " + ans.container + " could not be checked (" +
			err.Error() + "). A session started before this add must be ended and started again with " + start +
			", to join " + ans.team + "."
	case len(started) > 0:
		return "A Claude session for " + ans.user + " is already running in " + ans.container + " (started " +
			strings.Join(started, ", ") + "). End it and start it again with " + start + ", to join " + ans.team + "."
	}
	return "No Claude session for " + ans.user + " is running in " + ans.container + ". Start one: as " + ans.user +
		" in " + ans.checkout + ", " + launcher + "."
}

// sessions are when each Claude session of the member started, as the reader prints them.
func (a *Adder) sessions(ctx context.Context, ans answers) ([]string, error) {
	bounded, cancel := context.WithTimeout(ctx, a.ReadTimeout)
	defer cancel()
	out, err := a.Lister.ReadCheckout(bounded, a.script, "sessions", ans.container, ans.user, "-")
	if err != nil {
		return nil, err
	}
	var read struct {
		Sessions []int64 `json:"sessions"`
	}
	dec := json.NewDecoder(strings.NewReader(out))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&read); err != nil || read.Sessions == nil {
		return nil, fmt.Errorf("the session list is not one the reader prints (%q)", out)
	}
	shown := make([]string, 0, len(read.Sessions))
	for _, at := range read.Sessions {
		shown = append(shown, time.Unix(at, 0).Format(startedLayout))
	}
	return shown, nil
}
