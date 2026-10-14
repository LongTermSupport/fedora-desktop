// Package lxcadd is "ccy-teams lxc-add <team>": an agent user of an existing LXC container
// joins a team without anyone editing the host_vars. It lists the host's running containers,
// asks for the user, the checkout, the handle's host word and the role (each answer checked
// by declare.py as it is given), shows a summary and asks once, then writes the
// agent_bus_lxc_members entry through declare.py and applies the team, which runs the play
// that joins the member. What is left to do inside the container is printed at the end.
package lxcadd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/lxcfreeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

const (
	// maxTries is how often a refused answer is asked again before the run ends.
	maxTries = 3

	playGum = "playbooks/imports/play-claude-yolo.yml"

	// readWhy is what sudo is asked for: LXC is rootful on this host.
	readWhy = "LXC here is rootful, so listing the containers and reading who lives in one is done as root."

	roleWorker      = "worker"
	roleCoordinator = "coordinator"
	usageText       = "'lxc-add' takes one team: ccy-teams lxc-add <team>."
)

// Containers is the host's LXC: which containers there are and who lives in a running one.
// *lxcfreeze.Lister is the real one.
type Containers interface {
	Containers(ctx context.Context) (running, stopped []string, err error)
	Users(ctx context.Context, container string) ([]string, error)
}

// stop ends the run with an exit code; whatever the person needs to read has been printed.
type stop struct{ code term.ExitCode }

func (s *stop) Error() string { return fmt.Sprintf("stopped with exit %d", s.code) }

// Adder is "ccy-teams lxc-add". Every dependency is a field so a test can stand in for each.
type Adder struct {
	Declare *declare.Client
	Gum     *ui.Gum
	Lister  Containers
	// Roster is "agent-bus list <team>" as root: the live coordinator is read from it.
	Roster func(ctx context.Context, team string) (agentbus.Roster, error)
	// SudoReady makes root available, saying why it is wanted; false means it has said why not.
	SudoReady  func(ctx context.Context, why string) bool
	ApplyTeams func(ctx context.Context, teams ...string) error
	// NewTerm is a Term of its own for each run: the step counters live in it.
	NewTerm func() *term.Term
	// LookPath finds a tool on PATH.
	LookPath func(name string) (string, error)

	Stdin  io.Reader // must be a terminal: the questions are asked there
	Stdout io.Writer // the CHANGED line declare.py prints

	HostVars string
	Home     string // for the ~ abbreviation

	term *term.Term
}

// New is the Adder over a host and the flows it hands the work to: apply joins the member.
func New(h *host.Host, kit *flow.Kit, o *ops.Ops) *Adder {
	return &Adder{
		Declare: h.Declare, Gum: h.Gum, Lister: &lxcfreeze.Lister{Sudo: h.Sudo, LookPath: h.LookPath},
		Roster: h.Bus.List, SudoReady: kit.SudoReady,
		ApplyTeams: func(ctx context.Context, teams ...string) error { return o.ApplyTeams(ctx, teams) },
		NewTerm:    h.FreshTerm, LookPath: h.LookPath, Stdin: h.Stdin, Stdout: h.Stdout,
		HostVars: h.HostVars, Home: h.Home,
	}
}

// answers is what the questions collect.
type answers struct {
	team, container, user, checkout, host, role string
}

func (a answers) entry() declare.LxcEntry {
	e := declare.LxcEntry{Team: a.team, Container: a.container, User: a.user, Host: a.host, Checkout: a.checkout}
	if a.role == roleCoordinator {
		e.Role = a.role
	}
	return e
}

func (a answers) label() string { return a.user + "@" + a.container }

// Run is ccy-teams lxc-add with the arguments after "lxc-add". It returns the exit code:
// 0 done, 1 refused or failed, 64 usage, 130 cancelled, or declare.py's own refusal code.
func (a *Adder) Run(ctx context.Context, args []string) term.ExitCode {
	a.term = a.NewTerm()
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
	running, err := a.listContainers(ctx)
	if err != nil {
		return err
	}
	held, err := a.coordinatorOf(ctx, team)
	if err != nil {
		return err
	}
	ans := answers{team: team, role: roleWorker}
	a.term.Steps = 4
	if held.holder == "" {
		a.term.Steps = 5
	}
	if err = a.ask(ctx, &ans, running, held); err != nil {
		return err
	}
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

// listContainers is the running containers, asking for root first. A container that is not
// running cannot join: the play reaches into it with lxc-attach.
func (a *Adder) listContainers(ctx context.Context) ([]string, error) {
	if !a.SudoReady(ctx, readWhy) {
		return nil, a.failed("so the containers cannot be listed, and nothing was changed.")
	}
	running, stopped, err := a.Lister.Containers(ctx)
	if err != nil {
		return nil, a.failed(err.Error())
	}
	if len(running) == 0 {
		msg := "there is no running LXC container on this host to add."
		if len(stopped) > 0 {
			msg += " Stopped, so not offered: " + strings.Join(stopped, ", ") + ". Start one, then run this again."
		}
		return nil, a.failed(msg)
	}
	if len(stopped) > 0 {
		a.term.Printf("Not offered, as they are not running: %s.\n", strings.Join(stopped, ", "))
	}
	return running, nil
}

// coordinator says who holds the team's coordinator role, from the declaration and from the
// live team, and whether the live team could be read.
type coordinator struct {
	holder     string // empty: the team has none
	unreadable string // why the live team could not be read; empty when it was
}

// coordinatorOf reads the declared LXC members first (a present coordinator there is the one
// the play will set), then the live roster. A roster that cannot be read is said later and
// leaves only worker on offer: a second coordinator is refused by declare.py in any case.
func (a *Adder) coordinatorOf(ctx context.Context, team string) (coordinator, error) {
	members, err := a.Declare.LxcMembers(ctx)
	if err != nil {
		return coordinator{}, err
	}
	for _, m := range members {
		if m.Team == team && m.State == "present" && m.Role == agentbus.RoleCoordinator {
			return coordinator{holder: m.User + "@" + m.Container}, nil
		}
	}
	return a.liveCoordinator(ctx, team), nil
}

// liveCoordinator is the coordinator the running team has, if its roster can be read.
func (a *Adder) liveCoordinator(ctx context.Context, team string) coordinator {
	roster, err := a.Roster(ctx, team)
	if err != nil {
		return coordinator{holder: "unknown", unreadable: err.Error()}
	}
	if m, ok := roster.Coordinator(); ok {
		return coordinator{holder: m.Handle}
	}
	return coordinator{}
}

func (a *Adder) ask(ctx context.Context, ans *answers, running []string, held coordinator) error {
	if err := a.askContainer(ctx, ans, running); err != nil {
		return err
	}
	if err := a.askUser(ctx, ans); err != nil {
		return err
	}
	if err := a.askCheckout(ctx, ans); err != nil {
		return err
	}
	if err := a.askHost(ctx, ans); err != nil {
		return err
	}
	return a.askRole(ctx, ans, held)
}

func (a *Adder) step(ctx context.Context, title, hint string) error {
	a.term.Step(title)
	return a.Gum.Explain(ctx, hint, 5)
}

func (a *Adder) askContainer(ctx context.Context, ans *answers, running []string) error {
	if err := a.step(ctx, "Container", "The LXC container whose agent joins "+ans.team+". It must already exist and be running; this never makes one."); err != nil {
		return err
	}
	choice, err := a.Gum.Choose(ctx, "Container", running[0], running)
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
	if err = a.step(ctx, "User", hint); err != nil {
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

func (a *Adder) askCheckout(ctx context.Context, ans *answers) error {
	hint := "Where the agent's repository is checked out, inside the container: an absolute path. " +
		"The play runs pingbus there, as " + ans.user + ", to name the member's handle."
	if err := a.step(ctx, "Checkout", hint); err != nil {
		return err
	}
	path, err := a.askValid(ctx, func(ctx context.Context, answer string) (bool, error) {
		return a.checked(a.Declare.LxcCheck(ctx, declare.LxcEntry{Checkout: answer}))
	}, "Checkout path in "+ans.container, "an absolute path, e.g. /srv/work/project", "/home/"+ans.user+"/")
	if err != nil {
		return err
	}
	ans.checkout = path
	a.term.Answered(path)
	return nil
}

func (a *Adder) askHost(ctx context.Context, ans *answers) error {
	suggestion, err := a.Declare.LxcSuggestHost(ctx, ans.container)
	if err != nil {
		return err
	}
	hint := "The <host> part of the member's handle: a role word such as worker, never a machine's hostname, " +
		"because handles appear in public forge text. Lower-case letters, digits and -."
	if err = a.step(ctx, "Handle host word", hint); err != nil {
		return err
	}
	word, err := a.askValid(ctx, func(ctx context.Context, answer string) (bool, error) {
		return a.checked(a.Declare.LxcCheck(ctx, declare.LxcEntry{Host: answer}))
	}, "Host word", "e.g. worker", suggestion)
	if err != nil {
		return err
	}
	ans.host = word
	a.term.Answered(word)
	return nil
}

// askRole offers coordinator only when the team has none; otherwise the member is a worker and
// the person is told why there is no question.
func (a *Adder) askRole(ctx context.Context, ans *answers, held coordinator) error {
	if held.holder != "" {
		if held.unreadable != "" {
			a.term.Printf("\nRole: worker. Who is in %s could not be read (%s), so a coordinator is not offered.\n",
				ans.team, held.unreadable)
			return nil
		}
		a.term.Printf("\nRole: worker. %s already has a coordinator (%s); a team has one.\n", ans.team, held.holder)
		return nil
	}
	if err := a.step(ctx, "Role", "A worker does the work it is given. The coordinator takes the humans' requests, dispatches them to the workers and is copied on what workers tell humans; "+ans.team+" has none yet."); err != nil {
		return err
	}
	role, err := a.Gum.Choose(ctx, "Role", roleWorker, []string{roleWorker, roleCoordinator})
	if err != nil {
		return a.gumErr(err)
	}
	ans.role = role
	a.term.Answered(role)
	return nil
}

// checker accepts an answer, or says what is wrong with it and refuses it. An error is a
// failure to check at all, which ends the run.
type checker func(ctx context.Context, answer string) (bool, error)

// askValid asks until check accepts the answer, maxTries at most. A refused answer comes back
// as the next try's value, as typed.
func (a *Adder) askValid(ctx context.Context, check checker, header, placeholder, value string) (string, error) {
	for range maxTries {
		answer, err := a.Gum.Input(ctx, header, placeholder, value)
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
	if _, err := a.Declare.LxcAddDryRun(ctx, ans.entry()); err != nil {
		return err
	}
	body := strings.Join([]string{
		line("Container", ans.container),
		line("User", ans.user),
		line("Checkout", ans.checkout),
		line("Host word", ans.host),
		line("Role", ans.role),
	}, "\n")
	a.term.Printf("\n")
	if err := a.Gum.Box(ctx, false, ans.label()+" joins "+ans.team, "", body); err != nil {
		return err
	}
	question := "Add " + ans.label() + " to " + ans.team + "? (saved to " + term.AbbrevHome(a.HostVars, a.Home) +
		", then " + ans.team + " is applied: sudo asks for your password)"
	yes, err := a.Gum.Confirm(ctx, question, true)
	if err != nil {
		return a.gumErr(err)
	}
	if !yes {
		a.term.Printf("Nothing was added.\n")
		return &stop{term.ExitFailure}
	}
	return nil
}

func line(label, value string) string { return fmt.Sprintf("%-10s %s", label, value) }

// write declares the member, applies the team, and says what is left to do in the container.
func (a *Adder) write(ctx context.Context, ans answers) error {
	change, err := a.Declare.LxcAdd(ctx, ans.entry())
	if err != nil {
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
		return a.failed(ans.label() + " is declared in " + term.AbbrevHome(a.HostVars, a.Home) + " but not joined. " +
			"If the cause above is about gh, log in inside the container as " + ans.user + ":\n" +
			"  sudo lxc-attach -n " + ans.container + " -- su - " + ans.user + "     (then gh auth login, and exit)\n" +
			"Once the cause is fixed, ccy-teams apply " + ans.team + " joins it.")
	}
	a.term.Say(ans.label() + " is a member of " + ans.team + ".")
	a.term.Printf("What is left is inside the container:\n"+
		"  Agent    as %s, in %s: ccy --teams %s\n"+
		"  GitHub   the play checked that %s is logged in to GitHub there; if it is ever logged out, gh auth login as %s.\n",
		ans.user, ans.checkout, ans.team, ans.user, ans.user)
	return nil
}
