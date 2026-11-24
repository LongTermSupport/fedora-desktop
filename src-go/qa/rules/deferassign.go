package rules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// DeferAssignLost forbids a deferred function literal that assigns to a variable of its
// enclosing function that is not a named result: nothing reads it after the deferred call, so
// the value (a Close error joined into err) is lost.
var DeferAssignLost = &analysis.Analyzer{
	Name: "project_defer_assign_lost",
	Doc: `project_defer_assign_lost flags a deferred function literal (defer func() { ... }())
that assigns to, or increments, a variable declared in its enclosing function (a local or a
parameter) that is not one of that function's named results.

A deferred call runs after the return values are set. Only a named result is read after it,
so an assignment to any other variable of the function is dead: the classic form,
defer func() { err = errors.Join(err, f.Close()) }() in a function whose results are unnamed,
drops the Close error and the function reports success (teamchat migrate/v1 verifyDir, Plan
00178).

Not flagged: a named result of the enclosing function; a variable declared inside the deferred
literal; a field or a pointer's target (b.err = ..., *p = ...); a variable of a function further
out, which that function can still read; a deferred named function.

How to fix: name the function's results, so the deferred assignment is what is returned; or
handle the value inside the deferred function.`,
	Run:        perPass(runDeferAssignLost),
	ResultType: noResultType,
}

func runDeferAssignLost(pass *analysis.Pass) {
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if fn.Body != nil {
					checkDefersOf(pass, fn, fn.Type, fn.Body)
				}
			case *ast.FuncLit:
				checkDefersOf(pass, fn, fn.Type, fn.Body)
			}
			return true
		})
	}
}

// checkDefersOf reports each deferred literal directly in fn's body (not in a nested literal,
// which is judged as its own function) that assigns a variable of fn other than a named result.
func checkDefersOf(pass *analysis.Pass, fn ast.Node, typ *ast.FuncType, body *ast.BlockStmt) {
	named := map[types.Object]bool{}
	if typ.Results != nil {
		for _, field := range typ.Results.List {
			for _, id := range field.Names {
				named[pass.TypesInfo.Defs[id]] = true
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.DeferStmt:
			if lit, isLit := ast.Unparen(x.Call.Fun).(*ast.FuncLit); isLit {
				reportLostAssigns(pass, fn, lit, named)
			}
			return false
		}
		return true
	})
}

// reportLostAssigns reports each assignment or increment in the deferred literal lit whose
// target is a variable declared in fn, outside lit, and not one of fn's named results.
func reportLostAssigns(pass *analysis.Pass, fn ast.Node, lit *ast.FuncLit, named map[types.Object]bool) {
	report := func(e ast.Expr) {
		id, isIdent := ast.Unparen(e).(*ast.Ident)
		if !isIdent {
			return
		}
		v, isVar := pass.TypesInfo.Uses[id].(*types.Var)
		if !isVar || named[v] || !inside(v.Pos(), fn) || inside(v.Pos(), lit) {
			return
		}
		pass.Reportf(id.Pos(), "a deferred function assigns %s, which is not a named result (project_defer_assign_lost): the value is lost at the return; name the function's results", id.Name)
	}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if x.Tok != token.DEFINE {
				for _, lhs := range x.Lhs {
					report(lhs)
				}
			}
		case *ast.IncDecStmt:
			report(x.X)
		}
		return true
	})
}

// inside is whether pos lies inside node.
func inside(pos token.Pos, node ast.Node) bool {
	return pos >= node.Pos() && pos < node.End()
}
