package rules

import (
	"go/ast"
	"go/constant"
	"go/token"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// tmuxOwner is the package that builds tmux argv.
const tmuxOwner = "internal/tmuxccy"

// paneCommands take a pane target: "=name:" for the session's current pane. The bare "=name"
// is refused by real tmux for these ("can't find pane", fedora-desktop#69).
var paneCommands = []string{"capture-pane", "send-keys", "pipe-pane", "select-pane", "paste-buffer", "display-message"}

// sessionCommands take a session target: "=name" (exact match), without the colon.
var sessionCommands = []string{"kill-session", "has-session", "attach-session", "list-clients", "rename-session", "switch-client"}

// TmuxTargets holds the tmux target forms of the one package that builds tmux argv.
var TmuxTargets = &analysis.Analyzer{
	Name: "project_tmux_targets",
	Doc: `project_tmux_targets checks the -t target of each tmux command built in internal/tmuxccy.

A pane command (capture-pane, send-keys, ...) must target paneTarget(name), which is "=name:";
a session command (kill-session, list-clients, ...) must target "=" + name with no colon.
Real tmux refuses the bare "=name" for a pane command, which fakes had hidden
(fedora-desktop#69). The commands are classified in the paneCommands and sessionCommands
tables; a -t beside a command in neither is flagged so the table is extended, and a function
that mixes the two kinds is flagged so each target can be judged.

Judged per function: the command words found in the function against the -t target in the
same argument list. Not caught: a target built in another function and passed in, and a tmux
argv assembled from a variable. scripts/test-ccy-tmux-targets.bash runs the real forms against
a real tmux.

How to fix: use paneTarget(name) for a pane command, "=" + name for a session command.`,
	Run:        perPass(runTmuxTargets),
	ResultType: noResultType,
}

func runTmuxTargets(pass *analysis.Pass) {
	if !ownedBy(pass.Pkg.Path(), tmuxOwner) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
				checkTmuxFunc(pass, fd)
			}
		}
	}
}

func checkTmuxFunc(pass *analysis.Pass, fd *ast.FuncDecl) {
	var panes, sessions bool
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok {
			if s, isConst := constString(pass, e); isConst {
				panes = panes || slices.Contains(paneCommands, s)
				sessions = sessions || slices.Contains(sessionCommands, s)
			}
		}
		return true
	})
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			checkTargets(pass, n.Args, panes, sessions)
		case *ast.CompositeLit:
			checkTargets(pass, n.Elts, panes, sessions)
		}
		return true
	})
}

func constString(pass *analysis.Pass, e ast.Expr) (string, bool) {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}

// checkTargets judges the expression after each "-t" in one argument list.
func checkTargets(pass *analysis.Pass, list []ast.Expr, panes, sessions bool) {
	for i := 0; i+1 < len(list); i++ {
		if s, ok := constString(pass, list[i]); !ok || s != "-t" {
			continue
		}
		target := list[i+1]
		switch {
		case panes && sessions:
			pass.Reportf(target.Pos(), "this function builds both pane and session tmux commands: split it so each -t target can be judged")
		case panes:
			if !isPaneTarget(pass, target) {
				pass.Reportf(target.Pos(), "a pane command needs the target paneTarget(name), which is =name: (tmux refuses the bare =name for panes, fedora-desktop#69)")
			}
		case sessions:
			if !isSessionTarget(pass, target) {
				pass.Reportf(target.Pos(), "a session command needs the target \"=\" + name, without a trailing colon")
			}
		default:
			pass.Reportf(target.Pos(), "-t beside a tmux command that is in neither the paneCommands nor the sessionCommands table: classify the command")
		}
	}
}

func isPaneTarget(pass *analysis.Pass, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	fn := calledFunc(pass, call)
	return fn != nil && fn.Name() == "paneTarget" && fn.Pkg() != nil && ownedBy(fn.Pkg().Path(), tmuxOwner)
}

func isSessionTarget(pass *analysis.Pass, e ast.Expr) bool {
	if !strings.HasPrefix(textOf(pass, e).text, "=") {
		return false
	}
	last := ast.Unparen(e)
	for {
		b, ok := last.(*ast.BinaryExpr)
		if !ok || b.Op != token.ADD {
			break
		}
		last = ast.Unparen(b.Y)
	}
	s, isConst := constString(pass, last)
	return !isConst || !strings.HasSuffix(s, ":")
}
