package rules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// SessionKind forbids asking whether a board row is a seat session by comparing its kind with
// KindCcy.
var SessionKind = &analysis.Analyzer{
	Name: "project_session_kind",
	Doc: `project_session_kind flags an == or != comparison with the teams board's KindCcy
(internal/teams/board, matched through types.Info) outside Kind.IsSession, which defines it.

A checkout's seat is held by a ccy container (KIND ccy) or by a cc session on the desktop (KIND
cc, project_row_kind). The flows that act on a seat's session asked Kind == KindCcy, so a cc
seat was treated as no seat at all: thirteen such checks were converted by hand, and ccy-teams
--graph, written after them, lost a host seat's session line the same way.

Not caught: a switch whose case lists KindCcy, and a comparison with the string "ccy" rather
than the constant. Test files are not judged.

How to fix: r.Kind.IsSession(), or compare with a kind that is not a session's.`,
	Run:        perPass(runSessionKind),
	ResultType: noResultType,
}

func runSessionKind(pass *analysis.Pass) {
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "IsSession" && fn.Recv != nil {
				continue
			}
			ast.Inspect(d, func(n ast.Node) bool {
				if be, ok := n.(*ast.BinaryExpr); ok && isSessionComparison(pass, be) {
					pass.Reportf(be.Pos(), "a seat session is a ccy or a cc one: ask Kind.IsSession, not == KindCcy (project_session_kind)")
				}
				return true
			})
		}
	}
}

// isSessionComparison is whether be compares something with the board's KindCcy.
func isSessionComparison(pass *analysis.Pass, be *ast.BinaryExpr) bool {
	if be.Op != token.EQL && be.Op != token.NEQ {
		return false
	}
	return isKindCcy(pass, be.X) || isKindCcy(pass, be.Y)
}

// isKindCcy is whether e names the board's KindCcy constant.
func isKindCcy(pass *analysis.Pass, e ast.Expr) bool {
	var id *ast.Ident
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		id = e
	case *ast.SelectorExpr:
		id = e.Sel
	default:
		return false
	}
	c, ok := pass.TypesInfo.Uses[id].(*types.Const)
	return ok && c.Name() == "KindCcy" && c.Pkg() != nil && ownedBy(c.Pkg().Path(), boardPkg)
}
