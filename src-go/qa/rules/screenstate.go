package rules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// screenPkg is the package whose State says what Claude's input box shows.
const screenPkg = "internal/screen"

// ScreenStateCompared forbids judging what Claude's input box shows by == or != against one
// screen.State.
var ScreenStateCompared = &analysis.Analyzer{
	Name: "project_screen_state_compared",
	Doc: `project_screen_state_compared flags an == or != comparison whose operand is a
screen.State (internal/screen, matched through types.Info), outside test files, inside package
screen too.

A pane is read into one of a few states, but Claude Code draws more screens than the reader
knows: a line held for review ("Removed 1 invisible character · review and press Enter to
send"), a redraw, a dialog, the external editor. A verdict asked as state != Typed (or
== Ready, its else being the verdict) puts every other state, any the reader does not know
included, on one side. tmuxccy.Submit's check had that shape (state != Typed took every other
screen, one with no box drawn included, as taken) when a /compact stayed held in the box
(fedora-desktop#288); how that line got past it is not established.

Not caught: a State converted to a string and compared, a State looked up in a slice, and a
verdict on a screen kept in another type (restore.State). No instance of the first two exists. A switch over State is held by the exhaustive linter, which
refuses a missing member and a default clause.

How to fix: switch over the State naming every member, so each consumer decides each state, and
a member added to screen is a compile error at every consumer until it is decided there.`,
	Run:        perPass(runScreenStateCompared),
	ResultType: noResultType,
}

func runScreenStateCompared(pass *analysis.Pass) {
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			be, ok := n.(*ast.BinaryExpr)
			if !ok || (be.Op != token.EQL && be.Op != token.NEQ) {
				return true
			}
			if isScreenState(pass, be.X) || isScreenState(pass, be.Y) {
				pass.Reportf(be.Pos(), "a screen state judged by == or != puts every other state, one the reader does not know included, on the other side (fedora-desktop#288): switch over every screen.State (project_screen_state_compared)")
			}
			return true
		})
	}
}

// isScreenState is whether e's type is internal/screen's State.
func isScreenState(pass *analysis.Pass, e ast.Expr) bool {
	named, ok := pass.TypesInfo.TypeOf(e).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Name() == "State" && obj.Pkg() != nil && ownedBy(obj.Pkg().Path(), screenPkg)
}
