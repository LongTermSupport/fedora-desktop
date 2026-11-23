package rules

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const containerPidsMsg = "container run without ccy's limit set: missing %s; build the argv with ccylimits.Limits.Run (project_container_pids_limit)"

// limitFlags are ccy's limit set, each of which a container run must carry: the slice that caps
// every ccy container together, the pids limit, and the hard and soft memory caps.
var limitFlags = []string{"--cgroup-parent", "--pids-limit", "--memory", "--memory-reservation"}

// containerVerbs are the verbs that start (or make, to start later) a container.
var containerVerbs = map[string]bool{"run": true, "create": true}

// containerPrefixes are the words an argv may hold before the verb: the engine, and docker's
// management command.
var containerPrefixes = map[string]bool{"podman": true, "docker": true, "container": true}

// containerRunFlags are the flags, each by its own name, that make a list starting with a
// container verb a container run rather than another program's "run".
var containerRunFlags = map[string]bool{
	"--rm": true, "-i": true, "-t": true, "-d": true, "--interactive": true, "--tty": true,
	"--detach": true, "--name": true, "--entrypoint": true, "--volume": true, "--mount": true,
	"--network": true, "--net": true, "--env": true, "--env-file": true, "--userns": true,
	"--user": true, "--workdir": true, "--hostname": true, "--security-opt": true,
	"--cap-add": true, "--cap-drop": true, "--privileged": true, "--device": true,
	"--init": true, "--read-only": true, "--tmpfs": true, "--label": true, "--replace": true,
	"--publish": true, "--memory": true, "--cpus": true,
}

// ContainerPidsLimit forbids a container run without ccy's whole limit set.
var ContainerPidsLimit = &analysis.Analyzer{
	Name: "project_container_pids_limit",
	Doc: `project_container_pids_limit forbids starting a container without ccy's whole limit set.

Every container ccy or ccc starts carries four flags: --cgroup-parent (the agents.slice user
unit, which caps all of them together), --pids-limit, --memory and --memory-reservation.
Without --pids-limit the engine decides: podman gives 2048 threads, docker none, and a fork
bomb inside a Claude Code session climbed to podman's 2048 in about ten seconds. Without the
memory caps and the slice, the containers together took the host's memory and the desktop
with it. The Go counterpart of the Semgrep rule ccy-container-default-pids-limit
(CLAUDE/QA.md), which reads only bash.

Flagged: an argument list, a []string literal or the arguments of one call, whose first
constant, after any leading non-constant arguments (a context, a program chosen at run time)
and any "podman", "docker" or "container", is "run" or "create", and which holds a container
flag but not all four limit flags; the message names the missing ones. Flags are read by
name: "-dit" is -d, -i and -t, "--rm=true" is --rm, "--memory=8g" is --memory. The container
flags: --rm, -i, -t, -d, --interactive,
--tty, --detach, --name, --entrypoint, --volume, --mount, --network, --net, --env,
--env-file, --userns, --user, --workdir, --hostname, --security-opt, --cap-add, --cap-drop,
--privileged, --device, --init, --read-only, --tmpfs, --label, --replace, --publish,
--memory, --cpus.

Not caught: a run whose only flags are short ones outside that list (-v, -e, -p, -w, -u, which
other programs' "run" verbs also take); a run assembled by append from several lists where no
literal holding a container flag starts with the verb; a --pids-limit placed after the image,
where it is the contained program's argument and not the engine's; and a limit that turns the
bound off (-1, 0, max) or another slice, as the values are not read.

How to fix: build the argv with ccylimits.Limits.Run, which starts with "run", "--rm" and the
whole set as the ccy launcher settles it.`,
	Run:        perPass(runContainerPidsLimit),
	ResultType: noResultType,
}

func runContainerPidsLimit(pass *analysis.Pass) {
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				checkContainerRun(pass, n, n.Elts)
			case *ast.CallExpr:
				checkContainerRun(pass, n, n.Args)
			}
			return true
		})
	}
}

// containerVerbAt returns the index of list's container verb, or -1 when list is not one.
func containerVerbAt(pass *analysis.Pass, list []ast.Expr) int {
	i := 0
	for i < len(list) {
		if _, ok := constString(pass, list[i]); ok {
			break
		}
		i++
	}
	for i < len(list) {
		s, _ := constString(pass, list[i])
		if !containerPrefixes[s] {
			break
		}
		i++
	}
	if i == len(list) {
		return -1
	}
	if s, _ := constString(pass, list[i]); !containerVerbs[s] {
		return -1
	}
	return i
}

// flagNames returns the flag names one argument carries: "--rm=true" is --rm, "-dit" is -d,
// -i and -t, and anything not a flag carries none.
func flagNames(arg string) []string {
	switch {
	case strings.HasPrefix(arg, "--"):
		name, _, _ := strings.Cut(arg, "=")
		return []string{name}
	case strings.HasPrefix(arg, "-") && len(arg) > 1 && !strings.Contains(arg, "="):
		names := make([]string, 0, len(arg)-1)
		for _, r := range arg[1:] {
			names = append(names, "-"+string(r))
		}
		return names
	}
	return nil
}

// checkContainerRun reports list when it is a container run without the whole limit set.
func checkContainerRun(pass *analysis.Pass, at ast.Node, list []ast.Expr) {
	verb := containerVerbAt(pass, list)
	if verb < 0 {
		return
	}
	run := false
	seen := map[string]bool{}
	for _, e := range list[verb+1:] {
		s, ok := constString(pass, e)
		if !ok {
			continue
		}
		for _, name := range flagNames(s) {
			seen[name] = true
			run = run || containerRunFlags[name]
		}
	}
	if !run {
		return
	}
	var missing []string
	for _, flag := range limitFlags {
		if !seen[flag] {
			missing = append(missing, flag)
		}
	}
	if len(missing) > 0 {
		pass.Reportf(at.Pos(), containerPidsMsg, strings.Join(missing, ", "))
	}
}
