package rules

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const containerPidsMsg = "container run with the engine's default pids limit: pass --pids-limit, settled as the ccy launcher settles it (project_container_pids_limit)"

// containerRunFlags are the flags that make a list starting with "run" a container run.
var containerRunFlags = map[string]bool{
	"--rm": true, "-it": true, "-i": true, "-t": true, "--interactive": true, "--tty": true,
	"-d": true, "--detach": true,
}

// ContainerPidsLimit forbids a container run with no --pids-limit.
var ContainerPidsLimit = &analysis.Analyzer{
	Name: "project_container_pids_limit",
	Doc: `project_container_pids_limit forbids starting a container with the engine's default pids limit.

Without --pids-limit the engine decides: podman gives 2048 threads, docker none. A fork bomb
inside a Claude Code session climbed to podman's 2048 in about ten seconds; under docker it
runs on into the user slice's own task limit, which the desktop shares. The Go twin of the
Semgrep rule ccy-container-default-pids-limit (CLAUDE/QA.md), which reads only bash.

Flagged: an argument list, a []string literal or the arguments of one call, whose first
constant is "run" and which holds a container-run flag (--rm, -it, -i, -t, --interactive,
--tty, -d, --detach) but no --pids-limit (or --pids-limit=N).

Not caught: a run whose arguments are appended from several lists with "run" in none of the
literals that hold a run flag, or a limit given as a value that turns it off (-1, 0, max).

How to fix: pass "--pids-limit" and the limit the ccy launcher settles (engine.PidsLimit).`,
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

// checkContainerRun reports list when it is a container run with no pids limit.
func checkContainerRun(pass *analysis.Pass, at ast.Node, list []ast.Expr) {
	if len(list) == 0 {
		return
	}
	if first, ok := constString(pass, list[0]); !ok || first != "run" {
		return
	}
	run := false
	for _, e := range list[1:] {
		s, ok := constString(pass, e)
		if !ok {
			continue
		}
		if s == "--pids-limit" || strings.HasPrefix(s, "--pids-limit=") {
			return
		}
		run = run || containerRunFlags[s]
	}
	if run {
		pass.Reportf(at.Pos(), containerPidsMsg)
	}
}
