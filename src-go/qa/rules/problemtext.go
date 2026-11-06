package rules

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// problemPackage is teamchat's status package, matched as a path suffix so the fixtures and the
// real module share it.
const problemPackage = "internal/teamchat/status"

// problemTrees are what the rule judges: teamchat's packages and its two commands.
var problemTrees = transportTrees

const problemFix = " (project_problem_error_text): the record refuses a problem over 512 bytes or with a control character, and then the whole team's record is not written; name the cause as a fixed class (a sentinel, matrix.Class, a cause string the failing step returns)"

// ProblemErrorText keeps an error's own text out of a status problem.
var ProblemErrorText = &analysis.Analyzer{
	Name: "project_problem_error_text",
	Doc: `project_problem_error_text flags, in internal/teamchat, cmd/teamchat and cmd/teamchat-admin and
below, an error's own text in the What or the Fix of a status.Problem: in a literal of the type
(by field name or by position, a pointer literal too) and in the two arguments of
status.NewProblem, an expression that holds

- a call of Error() on an error that is not a package-level variable (a sentinel's text is
  fixed, and may be carried);
- an argument of fmt.Sprint, Sprintf or Sprintln that is an error, other than such a sentinel.

Test files are judged as the code is.

A status record is written only when every problem is one line of at most 512 bytes
(status.OneLine), and the health tick writes the team's whole record at once: an error's text,
which may hold a path, a wrapped chain or a remote server's words, can pass that bound on one
failure and lose the team's record for as long as the failure lasts (Plan 00246 Task 8.8, the
leaf renewal's first draft).

Not caught: an error's text put in a variable first and the variable put in the problem; an error
formatted by another function than fmt's three.

How to fix: name the cause as a fixed class: a sentinel's text, matrix.Class(err), or a cause
string the failing step returns beside its error.`,
	Run:        perPass(runProblemErrorText),
	ResultType: noResultType,
}

func runProblemErrorText(pass *analysis.Pass) {
	if !slices.ContainsFunc(problemTrees, func(tree string) bool { return inTree(pass.Pkg.Path(), tree) }) {
		return
	}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			for _, v := range problemTexts(pass, n) {
				if at := errorText(pass, v); at != nil {
					pass.Reportf(at.Pos(), "an error's own text in a status problem%s", problemFix)
				}
			}
			return true
		})
	}
}

// problemTexts are the expressions n gives a problem's What and Fix: the values of a
// status.Problem literal, and the arguments of status.NewProblem.
func problemTexts(pass *analysis.Pass, n ast.Node) []ast.Expr {
	switch n := n.(type) {
	case *ast.CompositeLit:
		if !isProblemType(pass.TypesInfo.TypeOf(n)) {
			return nil
		}
		out := make([]ast.Expr, 0, len(n.Elts))
		for _, e := range n.Elts {
			if kv, ok := e.(*ast.KeyValueExpr); ok {
				out = append(out, kv.Value)
				continue
			}
			out = append(out, e)
		}
		return out
	case *ast.CallExpr:
		if fn := calledFunc(pass, n); fn != nil && fn.Name() == "NewProblem" && isProblemPackage(fn.Pkg()) {
			return n.Args
		}
	}
	return nil
}

// errorText is the first node in e that carries an error's own text, nil for none.
func errorText(pass *analysis.Pass, e ast.Expr) ast.Node {
	var found ast.Node
	ast.Inspect(e, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if found != nil || !ok {
			return found == nil
		}
		if sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr); isSel && isErrorMethod(sel.Sel.Name) && len(call.Args) == 0 &&
			isError(pass.TypesInfo.TypeOf(sel.X)) && !isSentinel(pass, sel.X) {
			found = call
			return false
		}
		if fn := calledFunc(pass, call); fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == "fmt" && strings.HasPrefix(fn.Name(), "Sprint") {
			for _, arg := range call.Args {
				if isError(pass.TypesInfo.TypeOf(arg)) && !isSentinel(pass, arg) {
					found = arg
					return false
				}
			}
		}
		return true
	})
	return found
}

// isProblemType is whether t is status.Problem.
func isProblemType(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Name() == "Problem" && isProblemPackage(named.Obj().Pkg())
}

func isProblemPackage(p *types.Package) bool {
	return p != nil && (p.Path() == problemPackage || strings.HasSuffix(p.Path(), "/"+problemPackage))
}

// errorInterface is the built-in error, nil if the universe has none.
func errorInterface() *types.Interface {
	iface, ok := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !ok || iface.NumMethods() != 1 {
		return nil
	}
	return iface
}

// isError is whether t is an error.
func isError(t types.Type) bool {
	iface := errorInterface()
	return iface != nil && t != nil && types.Implements(t, iface)
}

// isErrorMethod is whether name is the one method of error.
func isErrorMethod(name string) bool {
	iface := errorInterface()
	return iface != nil && iface.Method(0).Name() == name
}

// isSentinel is whether e names a package-level variable, whose text is fixed.
func isSentinel(pass *analysis.Pass, e ast.Expr) bool {
	var id *ast.Ident
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		id = e
	case *ast.SelectorExpr:
		id = e.Sel
	default:
		return false
	}
	v, ok := pass.TypesInfo.Uses[id].(*types.Var)
	return ok && v.Pkg() != nil && v.Parent() == v.Pkg().Scope()
}
