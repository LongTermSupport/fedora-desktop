// Package ops is what ccy-teams does to the machine: apply a team (the play), take one down
// (remove), back one up and restore it (the installer), and install Element and write the
// teams' profiles (the Element play). Each is the flow a command line runs, asking its
// questions on the terminal and answering the exit code that command would.
package ops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/config"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// FlagAll is apply's "every team out of date or not built".
const FlagAll = "--all"

// errOpFailed is a flow that failed after saying why: the caller prints nothing more.
var errOpFailed = errors.New("failed, and said why")

// Ops is the flows that change what runs on this machine: apply and remove (the play),
// backup and restore (the installer). Every dependency is a field, so a test supplies a
// scripted runner and buffers and nothing here touches the real system.
type Ops struct {
	Cfg     config.Config
	Runner  execx.Runner
	Declare *declare.Client
	Sudo    *sudo.Sudo
	Bus     *agentbus.Client // its Root is Sudo: the installer calls
	Gum     *ui.Gum
	Picker  *ui.Picker
	Term    *term.Term
	Stdin   io.Reader
	Stdout  io.Writer

	// IsTerminal is the one terminal rule of these flows: a question is asked only when
	// Stdin and Term.Err are both terminals (stdout may be piped; the questions are drawn
	// on the terminal through stdin and stderr).
	IsTerminal func(v any) bool

	// Refresh is collect_teams + read_status: the applied state of every team declared or
	// built. It reports its own failures and returns an error to stop on.
	Refresh func(ctx context.Context) (map[string]declare.Status, error)

	NeedCheckout func() error                      // reports its own refusal
	LookPath     func(name string) (string, error) // the PATH lookup of a tool
	FileExists   func(path string) bool
	Width        func(ctx context.Context) int // the terminal's columns; nil or small means 80

	// Changed is told when a flow that may have changed a team ran (even one that failed
	// part way), so what was read of the machine and kept is read again.
	Changed func()
}

// ElementLauncher is the desktop file the Element play writes for a team.
func (o *Ops) ElementLauncher(team string) string {
	return filepath.Join(o.Cfg.Home, ".local/share/applications", "agent-bus-"+team+"-element.desktop")
}

// New is the Ops of a host. refresh reads the applied state of every team (the board's
// Statuses); changed, when not nil, is told whenever a flow may have changed a team.
func New(h *host.Host, refresh func(context.Context) (map[string]declare.Status, error), changed func()) *Ops {
	return &Ops{
		Cfg: h.Config, Runner: h.Runner, Declare: h.Declare, Sudo: h.Sudo, Bus: h.Bus, Gum: h.Gum, Picker: h.Picker,
		Term: h.FreshTerm(), Stdin: h.Stdin, Stdout: h.Stdout, IsTerminal: h.Term.IsTerminal,
		Refresh: refresh, NeedCheckout: h.NeedCheckout, LookPath: h.LookPath, FileExists: exists, Changed: changed,
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// RunElementPlay is play-agent-bus-element.yml, then each named team's launcher must be there.
func (o *Ops) RunElementPlay(ctx context.Context, teams []string) error {
	names := strings.Join(teams, " ")
	o.Term.Say("Running play-agent-bus-element.yml: it installs Element Desktop (sudo) and writes the Element profile for " + names + ".")
	if err := o.checkPlay(o.Cfg.ElementPlay); err != nil {
		o.Term.PrintError(err.Error())
		return errOpFailed
	}
	_, err := o.Runner.Run(ctx, execx.Cmd{
		Name: o.Cfg.ElementPlay, Stdin: o.Stdin, Stdout: o.Stdout, Stderr: o.Term.Err,
	})
	if err != nil {
		o.Term.PrintError("play-agent-bus-element.yml failed (above), so " + names + " has no Element profile yet.")
		return errOpFailed
	}
	for _, team := range teams {
		if !o.FileExists(o.ElementLauncher(team)) {
			o.Term.PrintError("play-agent-bus-element.yml finished, but " + team + "'s Element launcher is not there.")
			return errOpFailed
		}
	}
	return nil
}

// ApplyTeams runs the play for those teams, then reads each one's stamp back: done only
// when every one is up to date (its homeserver started, its room and people made). A team
// with an Element profile declared has it written by its own play, here too. A failure has
// already been said.
func (o *Ops) ApplyTeams(ctx context.Context, teams []string) error {
	defer o.changed()
	if err := o.runPlay(ctx, teams); err != nil {
		o.Term.PrintError(fmt.Sprintf("play-agent-bus.yml failed (exit %d, above).", playExit(err)))
		return errOpFailed
	}
	status, err := o.Refresh(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", errOpFailed, err)
	}
	var behind []string
	for _, team := range teams {
		if status[team].State != declare.ApplyCurrent {
			behind = append(behind, team+" ("+status[team].Word()+")")
		}
	}
	if len(behind) > 0 {
		o.Term.PrintError("the play finished, but these teams are still not as declared: " + strings.Join(behind, " ") + ".")
		return errOpFailed
	}
	watched, err := o.elementTeams(ctx, teams)
	if err != nil {
		o.childExit(err)
		return errOpFailed
	}
	if len(watched) > 0 && o.RunElementPlay(ctx, watched) != nil {
		o.Term.PrintError("the homeserver side is applied, but the Element profile is not; once the cause above is fixed, ccy-teams apply " +
			strings.Join(teams, " ") + " runs both again.")
		return errOpFailed
	}
	o.Term.Say("Applied: " + strings.Join(teams, " ") + ".")
	return nil
}

// Apply is "ccy-teams apply <team> | --all": bring a team, or every team out of date or not
// built, to what the host_vars declare. Never a team declared absent or no longer declared:
// taking a team down is remove's, one team at a time.
func (o *Ops) Apply(ctx context.Context, args []string) term.ExitCode {
	defer o.changed()
	if err := o.NeedCheckout(); err != nil {
		return term.ExitFailure
	}
	if len(args) != 1 {
		return o.usageError("'apply' takes one team, or --all.")
	}
	status, err := o.Refresh(ctx)
	if err != nil {
		return term.ExitFailure
	}
	var targets []string
	if args[0] == FlagAll {
		all, code, done := o.allTargets(status)
		if done {
			return code
		}
		targets = all
	} else {
		team := args[0]
		s := status[team]
		switch s.State {
		case declare.ApplyOutdated, declare.ApplyUnbuilt, declare.ApplyCurrent:
			targets = []string{team}
		case declare.ApplyDecide, declare.ApplyRemoved:
			return o.usageError("team " + team + " is not declared present; ccy-teams remove " + team + " takes a team down.")
		case declare.ApplyUnreadable:
			return o.usageError("team " + team + "'s entry cannot be read: " + s.Detail + ".")
		case declare.ApplyAbsent:
			fallthrough
		default:
			return o.usageError("team " + team + " is not declared in " + o.home(o.Cfg.HostVars) + ".")
		}
	}
	if err := o.ApplyTeams(ctx, targets); err != nil {
		o.Term.PrintError("not applied: " + strings.Join(targets, " ") +
			". Their entries stay as declared; once the cause above is fixed, ccy-teams apply runs the play again.")
		return term.ExitFailure
	}
	return term.ExitOK
}

func (o *Ops) changed() {
	if o.Changed != nil {
		o.Changed()
	}
}

// interactive reports whether there is a terminal to ask on.
func (o *Ops) interactive() bool {
	return o.IsTerminal(o.Stdin) && o.IsTerminal(o.Term.Err)
}

// playJSON is the play's extra vars exactly as Python's json.dumps writes them.
func playJSON(teams []string) string {
	quoted := make([]string, 0, len(teams))
	for _, t := range teams {
		quoted = append(quoted, strconv.QuoteToASCII(t))
	}
	return `{"agent_bus_only_teams": [` + strings.Join(quoted, ", ") + `]}`
}

// usageError is the "ccy-teams: ... See ccy-teams --help." refusal.
func (o *Ops) usageError(msg string) term.ExitCode {
	o.Term.Printf("ccy-teams: %s See ccy-teams --help.\n", msg)
	return term.ExitUsage
}

// cancelled is the person leaving a question.
func (o *Ops) cancelled() term.ExitCode {
	o.Term.Printf("Cancelled; nothing was changed.\n")
	return term.ExitCancelled
}

// playExit is the exit status a failed play gave, 1 if it never ran.
func playExit(err error) term.ExitCode {
	if ee, ok := errors.AsType[*execx.ExitError](err); ok {
		return term.ExitCode(ee.Code)
	}
	return term.ExitFailure
}

// home abbreviates the home directory as ~ in a path shown to the person.
func (o *Ops) home(path string) string {
	return term.AbbrevHome(path, o.Cfg.Home)
}

// childExit is the exit status of a failed child: the status it exited with, so a code the
// child chose (64, 77, 78) reaches the caller. A child that could not run at all, or an
// error of ours, is said and is 1. A declare.py failure is said here too, since the client
// captured what declare printed.
func (o *Ops) childExit(err error) term.ExitCode {
	if de, ok := errors.AsType[*declare.Error](err); ok {
		if de.Stderr != "" {
			o.Term.Printf("%s\n", de.Stderr)
		}
		return term.ExitCode(de.Code)
	}
	if ce, ok := errors.AsType[*agentbus.CommandError](err); ok {
		return term.ExitCode(ce.Code)
	}
	if ee, ok := errors.AsType[*execx.ExitError](err); ok {
		return term.ExitCode(ee.Code)
	}
	o.Term.PrintError(err.Error())
	return term.ExitFailure
}

// runPlay is play-agent-bus.yml for those teams alone, run by its own shebang (run.bash's
// single-play mode knows how this machine gives Ansible its sudo password), streamed.
func (o *Ops) runPlay(ctx context.Context, teams []string) error {
	o.Term.Say("Running play-agent-bus.yml for " + strings.Join(teams, " ") +
		": it starts each team's homeserver, and creates its room and its people's accounts.")
	if err := o.checkPlay(o.Cfg.Play); err != nil {
		return err
	}
	_, err := o.Runner.Run(ctx, execx.Cmd{
		Name: o.Cfg.Play, Args: []string{"-e", playJSON(teams)},
		Stdin: o.Stdin, Stdout: o.Stdout, Stderr: o.Term.Err,
	})
	if err != nil {
		return fmt.Errorf("play-agent-bus.yml: %w", err)
	}
	return nil
}

// errPlayOutsideCheckout is a play path that is not the checkout's own.
var errPlayOutsideCheckout = errors.New("refusing to run a play from outside the checkout")

// checkPlay accepts a play only if it is an absolute path inside the checkout, once cleaned
// (so ".." cannot climb out), unless the environment is trusted for tests.
func (o *Ops) checkPlay(play string) error {
	if trustPlays {
		return nil
	}
	rel, err := filepath.Rel(o.Cfg.RootDir, filepath.Clean(play))
	if !filepath.IsAbs(play) || err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("%w: %q is not under %s",
			errPlayOutsideCheckout, play, o.Cfg.RootDir)
	}
	return nil
}

// elementTeams is those of the named teams that have an Element profile declared, in the
// order declare lists them.
func (o *Ops) elementTeams(ctx context.Context, teams []string) ([]string, error) {
	profiles, err := o.Declare.ElementTeams(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range profiles {
		if slices.Contains(teams, p.Team) {
			out = append(out, p.Team)
		}
	}
	return out, nil
}

// allTargets sorts the teams for "apply --all": out of date or unbuilt ones are to be
// applied, those awaiting a decision are named and left, and unreadable ones refuse the
// whole run. done is true when the run ends here, with code.
func (o *Ops) allTargets(status map[string]declare.Status) ([]string, term.ExitCode, bool) {
	var targets []string
	var waiting, broken []string
	for _, team := range slices.Sorted(maps.Keys(status)) {
		s := status[team]
		switch s.State {
		case declare.ApplyOutdated, declare.ApplyUnbuilt:
			targets = append(targets, team)
		case declare.ApplyDecide:
			waiting = append(waiting, team+" ("+s.Word()+")")
		case declare.ApplyUnreadable:
			broken = append(broken, team+": "+s.Detail)
		case declare.ApplyCurrent, declare.ApplyRemoved, declare.ApplyAbsent:
			// Nothing to apply, and nothing to decide.
		}
	}
	if len(broken) > 0 {
		o.Term.PrintError("these teams' entries cannot be read, so nothing was applied: " + strings.Join(broken, " ") + ".")
		return nil, term.ExitFailure, true
	}
	if len(waiting) > 0 {
		o.Term.Printf("Left alone, as they need your decision (ccy-teams remove <team>, or declare them again): %s.\n", strings.Join(waiting, " "))
	}
	if len(targets) == 0 {
		o.Term.Printf("No team is out of date or waiting to be built.\n")
		return nil, term.ExitOK, true
	}
	return targets, term.ExitOK, false
}
