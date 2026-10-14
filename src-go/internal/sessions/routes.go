package sessions

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// kindCancelled is the signal that withdraws a warning.
const kindCancelled = "reboot-cancelled"

// projectInfo is one live project: its directory, its live sessions in listing order, and
// where its daemon is reached when it has a ccy session with a container.
type projectInfo struct {
	Dir       string
	Sessions  []string
	Engine    string
	Container string
}

// route is how a project's daemon CLI is run, and the project root it is told.
type route struct {
	cmd  []string
	root string
	via  string
}

func daemonCLI(dir string) string { return dir + "/.claude/hooks-daemon/bin/hooks-daemon" }

// route reaches a project's daemon where its venv is. For a project with a ccy session that is
// inside the container: most projects run only ccy, and their daemon has no venv on the host
// at all. A project with only cc sessions is reached on the host. A signal is a file in the
// project's own tree, which the container and the host share, so one delivery per project
// reaches every session in it, ccy and cc alike.
func (p projectInfo) route() route {
	if p.Container != "" {
		return route{
			cmd:  []string{p.Engine, "exec", p.Container, daemonCLI(containerWorkspace)},
			root: containerWorkspace,
			via:  " (in container " + p.Container + ")",
		}
	}
	return route{cmd: []string{daemonCLI(p.Dir)}, root: p.Dir}
}

// run is the route's command with args after it.
func (r route) run(args ...string) execx.Cmd {
	return execx.Cmd{Name: r.cmd[0], Args: slices.Concat(r.cmd[1:], args)}
}

// sessionContainers is the engine and container found in each session's process tree, from
// the live pane and process tables. A failed read is reported and returned: "could not look"
// must not read as "no container".
func (a *App) sessionContainers(ctx context.Context) ([]sessprobe.Owner, error) {
	panes, err := a.Tmux.Panes(ctx)
	if err != nil {
		a.failErr(err)
		return nil, errReported
	}
	table, err := sessprobe.ProcessTable(ctx, a.Runner)
	if err != nil {
		a.fail("the process table could not be read, so no session can be matched to its container: " + strings.TrimSpace(table))
		return nil, errReported
	}
	procs, err := sessprobe.ParseProcs(table)
	if err != nil {
		a.failErr(err)
		return nil, errReported
	}
	return sessprobe.SessionContainers(panes, procs), nil
}

// groupProjects splits the live sessions into projects (each directory once, in order of first
// appearance) and the ccy sessions that have no container: those are at ccy's startup prompts,
// or their container has exited, so there is no agent in them to warn.
func groupProjects(sessions []tmuxccy.Session, owners []sessprobe.Owner) ([]projectInfo, []string) {
	containers := map[string]sessprobe.Owner{}
	for _, o := range owners {
		if o.HasContainer() {
			containers[o.Session] = o
		}
	}
	var projects []projectInfo
	var idle []string
	for _, s := range sessions {
		if s.Name == "" || s.Path == "" {
			continue
		}
		owner, hasContainer := containers[s.Name]
		if strings.HasPrefix(s.Name, "ccy-") && !hasContainer {
			idle = append(idle, "  "+s.Name+"  in "+s.Path)
			continue
		}
		i := slices.IndexFunc(projects, func(p projectInfo) bool { return p.Dir == s.Path })
		if i < 0 {
			projects = append(projects, projectInfo{Dir: s.Path})
			i = len(projects) - 1
		}
		projects[i].Sessions = append(projects[i].Sessions, s.Name)
		// A project with a ccy session is reached in the first such session's container.
		if hasContainer && projects[i].Container == "" {
			projects[i].Engine, projects[i].Container = owner.Engine, owner.Container
		}
	}
	return projects, idle
}

// brokenProject is a project whose CLI exists but cannot run, with what it said.
type brokenProject struct {
	project projectInfo
	said    string
}

// checkProjects finds every reason a live session could not be given the signal BEFORE any is:
// a session list that cannot be read, a project with no hooks-daemon CLI, and (for a warning)
// a CLI that cannot run where its session is reached. Every session that cannot be warned is
// listed with what to do about it, not just the first. The reports go to stderr; the projects
// to signal come back.
func (a *App) checkProjects(ctx context.Context, kind string) ([]projectInfo, error) {
	// Read whole first: a listing that failed would otherwise read as "no sessions", after
	// which a reboot would proceed with every session unwarned.
	sessions, err := a.Tmux.List(ctx)
	if err != nil {
		a.failErr(err)
		a.fail("the live session list could not be read, so nothing is signalled and nothing may reboot.")
		return nil, errReported
	}
	if len(sessions) == 0 {
		return nil, nil
	}
	owners, err := a.sessionContainers(ctx)
	if err != nil {
		a.fail("the live sessions could not be matched to their containers, so nothing is signalled and nothing may reboot.")
		return nil, errReported
	}
	projects, idle := groupProjects(sessions, owners)
	if len(idle) > 0 {
		a.sayf("Left out: no container was found in these ccy sessions' process trees (still at ccy's startup prompts, or the container has exited), so there is no agent in them to warn:\n%s\n",
			strings.Join(idle, "\n"))
	}
	missing, broken := a.probeProjects(ctx, kind, projects)
	if len(missing) == 0 && len(broken) == 0 {
		return projects, nil
	}
	a.reportRefusals(missing, broken)
	return nil, errReported
}

// probeProjects sorts the projects whose daemon cannot be used into those with no CLI and those
// whose CLI cannot run. An executable CLI can still be unable to run, as the host's is for a
// project whose daemon has no venv there, so a warning first asks each route for `signal
// --help`, which changes nothing but must start the daemon's python. A withdrawal skips this: it
// reaches every project it can.
func (a *App) probeProjects(ctx context.Context, kind string, projects []projectInfo) ([]projectInfo, []brokenProject) {
	var missing []projectInfo
	var broken []brokenProject
	for _, p := range projects {
		if !a.Executable(daemonCLI(p.Dir)) {
			missing = append(missing, p)
			continue
		}
		if kind == kindCancelled {
			continue
		}
		out, err := execx.Combined(ctx, a.Runner, p.route().run("signal", "--help"))
		if err != nil {
			if strings.TrimSpace(out) == "" {
				out = err.Error()
			}
			broken = append(broken, brokenProject{project: p, said: out})
		}
	}
	return missing, broken
}

// reportRefusals names every session that cannot be warned, and what to do about each.
func (a *App) reportRefusals(missing []projectInfo, broken []brokenProject) {
	if len(missing) > 0 {
		a.fail("these live sessions cannot be warned: their project has no hooks-daemon CLI.")
		for _, p := range missing {
			for _, name := range p.Sessions {
				a.sayf("  %s  in %s  (expected %s)\n", name, p.Dir, daemonCLI(p.Dir))
			}
		}
	}
	if len(broken) > 0 {
		a.fail("these live sessions cannot be warned: their project's hooks-daemon CLI cannot run.")
		for _, b := range broken {
			via := b.project.route().via
			for _, name := range b.project.Sessions {
				a.sayf("  %s  in %s%s: its hooks-daemon CLI cannot run:\n", name, b.project.Dir, via)
			}
			for line := range strings.SplitSeq(strings.TrimRight(b.said, "\n"), "\n") {
				a.sayf("      %s\n", line)
			}
		}
	}
	a.sayf("For each session above, either end it (run ccy-sessions and press Ctrl-X on it), or install or repair the daemon in its project (the hooks-daemon skill, 'install'). Nothing was signalled.\n")
}

// minutesFlag is " --minutes N" for a warning, empty for a withdrawal.
func minutesFlag(minutes int) string {
	if minutes <= 0 {
		return ""
	}
	return " --minutes " + strconv.Itoa(minutes)
}

// deliverSignal invokes the daemon CLI of each project once and returns how many it reached.
// A warning stops at the first project that refuses, so the caller can withdraw it. A
// withdrawal carries on past a refusal: every project it reaches is one fewer left waiting for
// a reboot that is not coming. A dry run says what it would do and signals nothing. The CLI's
// own stdout goes to stderr: this tool's stdout is its payload.
func (a *App) deliverSignal(ctx context.Context, projects []projectInfo, kind string, minutes int, dryRun bool) (int, error) {
	if len(projects) == 0 {
		a.sayf("No CCY or cc sessions need warning; nothing to signal.\n")
		return 0, nil
	}
	args := []string{"signal", kind}
	if minutes > 0 {
		args = append(args, "--minutes", strconv.Itoa(minutes))
	}
	args = append(args, "--all-sessions")
	failed := false
	for _, p := range projects {
		r := p.route()
		if dryRun {
			a.sayf("would signal %s%s to %s%s\n", kind, minutesFlag(minutes), p.Dir, r.via)
			continue
		}
		cmd := r.run(slices.Concat(args, []string{"--project-root", r.root})...)
		cmd.Stdout, cmd.Stderr = a.Stderr, a.Stderr
		if _, err := a.Runner.Run(ctx, cmd); err != nil {
			a.fail("signalling " + kind + " to " + p.Dir + r.via + " failed.")
			if kind != kindCancelled {
				return 0, errReported
			}
			failed = true
			continue
		}
		sent := ""
		if minutes > 0 {
			sent = " (" + strconv.Itoa(minutes) + " min)"
		}
		a.sayf("signalled %s%s to %s%s\n", kind, sent, p.Dir, r.via)
	}
	if failed {
		return 0, errReported
	}
	return len(projects), nil
}

// signalProjects checks every live project, then signals each once. Every project is checked
// BEFORE any is signalled, so a refusal leaves no project half-warned about a reboot that is
// then not going to happen.
func (a *App) signalProjects(ctx context.Context, kind string, minutes int, dryRun bool) (int, error) {
	projects, err := a.checkProjects(ctx, kind)
	if err != nil {
		return 0, err
	}
	return a.deliverSignal(ctx, projects, kind, minutes, dryRun)
}
