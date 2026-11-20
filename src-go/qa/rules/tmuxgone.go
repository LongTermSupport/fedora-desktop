package rules

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// tmuxGoneClassifier is the one function that may spell tmux's no-server and no-session wording.
const tmuxGoneClassifier = "goneAnswer"

// tmuxGoneWords are the fragments of tmux's own answers that a loose match takes for "the server
// or the session is not there"; through lxc-attach, runuser or env the last is also theirs.
var tmuxGoneWords = []string{"no server running", "can't find", "error connecting to", "No such file or directory"}

// TmuxGoneText keeps tmux's "not there" answers to the one classifier in internal/tmuxccy.
var TmuxGoneText = &analysis.Analyzer{
	Name: "project_tmux_gone_text",
	Doc: `project_tmux_gone_text forbids reading tmux's "not there" answer outside tmuxccy's one classifier.

tmux says a server or session is not there in a few exact forms ("no server running on <socket>",
"error connecting to <socket> (No such file or directory)", "can't find session: <name>"), with
exit 1. A loose match on a fragment ("No such file or directory", "can't find") also takes
another program's failure for it: run in an LXC container through lxc-attach, runuser and env,
a missing tmux or a failed attach says "No such file or directory" too, and ccy-teams shutdown
read a guest server it never reached as empty ("no session running", exit 0).

Flagged, in internal/tmuxccy and every package that imports it (the packages that hold a tmux
answer; the tmux program is tmuxccy's alone, project_seam_commands), in files that are not
tests: a string literal containing one of "no server running", "can't find", "error connecting
to" or "No such file or directory", outside tmuxccy's function goneAnswer. A struct tag and an
import path are not flagged.

Not caught: a fragment assembled at run time or split across literals, a match on tmux's text
in a package that neither is nor imports tmuxccy, and a different fragment of the same answers.

How to fix: ask tmuxccy (errors.Is(err, tmuxccy.ErrSessionGone), or List and Panes, which read
no server as empty), whose goneAnswer accepts only tmux's own forms with exit 1.`,
	Run:        perPass(runTmuxGoneText),
	ResultType: noResultType,
}

func runTmuxGoneText(pass *analysis.Pass) {
	if !holdsTmuxAnswers(pass) {
		return
	}
	owner := ownedBy(pass.Pkg.Path(), tmuxOwner)
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if owner && funcName(decl) == tmuxGoneClassifier {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.ImportSpec:
					return false
				case *ast.Field:
					ast.Inspect(n.Type, func(m ast.Node) bool {
						if lit, ok := m.(*ast.BasicLit); ok {
							checkTmuxGoneLiteral(pass, lit)
						}
						return true
					})
					return false
				case *ast.BasicLit:
					checkTmuxGoneLiteral(pass, n)
				}
				return true
			})
		}
	}
}

// holdsTmuxAnswers is whether the package is tmuxccy or imports it.
func holdsTmuxAnswers(pass *analysis.Pass) bool {
	if ownedBy(pass.Pkg.Path(), tmuxOwner) {
		return true
	}
	return slices.ContainsFunc(pass.Pkg.Imports(), func(p *types.Package) bool {
		return ownedBy(p.Path(), tmuxOwner)
	})
}

func checkTmuxGoneLiteral(pass *analysis.Pass, lit *ast.BasicLit) {
	if lit.Kind != token.STRING {
		return
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return
	}
	for _, w := range tmuxGoneWords {
		if strings.Contains(s, w) {
			pass.Reportf(lit.Pos(), "tmux's answer %q matched outside tmuxccy's goneAnswer, which another program's failure "+
				"(lxc-attach, runuser, env) can also say: ask tmuxccy (errors.Is(err, tmuxccy.ErrSessionGone)) (project_tmux_gone_text)", w)
		}
	}
}
