package rules

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const containerPidsMsg = "container run with the engine's default pids limit: pass --pids-limit, settled as the ccy launcher settles it (project_container_pids_limit)"

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

// ContainerPidsLimit forbids a container run with no --pids-limit.
var ContainerPidsLimit = &analysis.Analyzer{
	Name: "project_container_pids_limit",
	Doc: `project_container_pids_limit forbids starting a container with the engine's default pids limit.

Without --pids-limit the engine decides: podman gives 2048 threads, docker none. A fork bomb
inside a Claude Code session climbed to podman's 2048 in about ten seconds; under docker it
runs on into the user slice's own task limit, which the desktop shares. The Go counterpart of
the Semgrep rule ccy-container-default-pids-limit (CLAUDE/QA.md), which reads only bash.

Flagged: an argument list, a []string literal or the arguments of one call, whose first
constant, after any leading non-constant arguments (a context, a program chosen at run time)
and any "podman", "docker" or "container", is "run" or "create", and which holds a container
flag but no --pids-limit. Flags are read by name: "-dit" is -d, -i and -t, "--rm=true" is
--rm, "--pids-limit=N" is the limit. The container flags: --rm, -i, -t, -d, --interactive,
--tty, --detach, --name, --entrypoint, --volume, --mount, --network, --net, --env,
--env-file, --userns, --user, --workdir, --hostname, --security-opt, --cap-add, --cap-drop,
--privileged, --device, --init, --read-only, --tmpfs, --label, --replace, --publish,
--memory, --cpus.

Not caught: a run whose only flags are short ones outside that list (-v, -e, -p, -w, -u, which
other programs' "run" verbs also take); a run assembled by append from several lists where no
literal holding a container flag starts with the verb; a --pids-limit placed after the image,
where it is the contained program's argument and not the engine's; and a limit that turns the
bound off (-1, 0, max), as the value is not read.

How to fix: pass "--pids-limit" and the limit the ccy launcher settles
(engine.PidsLimitDefault, or the settled CCY_PIDS_LIMIT).`,
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

// checkContainerRun reports list when it is a container run with no pids limit.
func checkContainerRun(pass *analysis.Pass, at ast.Node, list []ast.Expr) {
	verb := containerVerbAt(pass, list)
	if verb < 0 {
		return
	}
	run := false
	for _, e := range list[verb+1:] {
		s, ok := constString(pass, e)
		if !ok {
			continue
		}
		for _, name := range flagNames(s) {
			if name == "--pids-limit" {
				return
			}
			run = run || containerRunFlags[name]
		}
	}
	if run {
		pass.Reportf(at.Pos(), containerPidsMsg)
	}
}
