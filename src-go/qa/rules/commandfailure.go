package rules

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// CommandFailureDropped forbids keeping only whether a command failed: its error must be carried.
var CommandFailureDropped = &analysis.Analyzer{
	Name: "project_command_failure_dropped",
	Doc: `project_command_failure_dropped flags a command run through the seam (a function or
method of internal/execx or qa/internal/proc that takes a Cmd) whose error is only tested,
never carried.

The error of a run is the cause of its failure: an exit code and stderr (execx.ExitError), a
start or stdin-feed failure, a refusal. Code that only asks err == nil, err != nil or
errors.Is(err, sentinel) and then reports a fixed guess has thrown the cause away: Plan 00191's
--token-usage turned every curl failure into status 000, printed "could not reach the API" for
a failure that may have been anything, and cached it for 15 minutes (2026-10-29).

Carrying is any other use of the error before the variable is next assigned: returning it,
passing it to a call (fmt.Errorf, errors.Join, errors.As, a logger, append), calling a method
on it (err.Error()), or storing it. A run whose call is a statement, or whose error goes to the
blank identifier, carries nothing. Uses are counted in source order within the function, so an
error a later call reuses the variable for is not this one. Test files and the seam packages
themselves are not judged.

Not caught: an error carried somewhere it is never shown (stored and dropped), or carried and
then contradicted by a guessed message beside it; a cause read from Result.ExitCode alone.

How to fix: put the error itself, or what errors.As finds in it (the exit code, stderr), in
the message or the record the caller sees, or return it.`,
	Run:        perPass(runCommandFailureDropped),
	ResultType: noResultType,
}

const failureDroppedMsg = "a command's failure is only tested, never carried: put its error (exit code, stderr) in what the caller sees, or return it"

func runCommandFailureDropped(pass *analysis.Pass) {
	if ownedByAny(pass.Pkg.Path(), execOwners) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
				checkFailureDropped(pass, fd.Body)
			}
		}
	}
}

// checkFailureDropped reports each run of a command in body whose error is not carried.
func checkFailureDropped(pass *analysis.Pass, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ExprStmt:
			if call, ok := n.X.(*ast.CallExpr); ok && runsCommand(pass, call) {
				pass.Reportf(call.Pos(), failureDroppedMsg)
			}
		case *ast.AssignStmt:
			checkRunAssign(pass, body, n)
		}
		return true
	})
}

// checkRunAssign reports a command run on the right of as whose error is not carried.
func checkRunAssign(pass *analysis.Pass, body *ast.BlockStmt, as *ast.AssignStmt) {
	if len(as.Rhs) != 1 {
		return
	}
	call, ok := as.Rhs[0].(*ast.CallExpr)
	if !ok || !runsCommand(pass, call) || len(as.Lhs) == 0 {
		return
	}
	id, ok := as.Lhs[len(as.Lhs)-1].(*ast.Ident)
	if !ok {
		return
	}
	if id.Name == "_" {
		pass.Reportf(call.Pos(), failureDroppedMsg)
		return
	}
	v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok || !carried(pass, body, v, as.End()) {
		pass.Reportf(call.Pos(), failureDroppedMsg)
	}
}

// runsCommand is whether call is to a function of a seam package that takes a Cmd and returns
// an error last.
func runsCommand(pass *analysis.Pass, call *ast.CallExpr) bool {
	f := calledFunc(pass, call)
	if f == nil || f.Pkg() == nil || !ownedByAny(f.Pkg().Path(), execOwners) {
		return false
	}
	sig, ok := f.Type().(*types.Signature)
	if !ok || sig.Results().Len() == 0 || !isErrorType(sig.Results().At(sig.Results().Len()-1).Type()) {
		return false
	}
	for p := range sig.Params().Variables() {
		if isCmdType(p.Type()) {
			return true
		}
	}
	return false
}

func isErrorType(t types.Type) bool {
	return types.Identical(t, types.Universe.Lookup("error").Type())
}

// carried is whether v, assigned at from, is used other than tested before it is next assigned.
// A use inside a branch errors.Is(v, ...) chose carries only that one failure, so it is not
// counted.
func carried(pass *analysis.Pass, body *ast.BlockStmt, v *types.Var, from token.Pos) bool {
	until := nextAssign(pass, body, v, from)
	tests, sentinel := testsOf(pass, body, v)
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if ok && id.Pos() > from && id.Pos() < until && pass.TypesInfo.Uses[id] == v && !tests[id] && !within(id.Pos(), sentinel) {
			found = true
		}
		return !found
	})
	return found
}

// testsOf is every use of v in body that only tests it, and the branches errors.Is(v, ...)
// chose.
func testsOf(pass *analysis.Pass, body *ast.BlockStmt, v *types.Var) (map[*ast.Ident]bool, []ast.Node) {
	tests := map[*ast.Ident]bool{}
	var sentinel []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BinaryExpr:
			if n.Op == token.EQL || n.Op == token.NEQ {
				markIdent(pass, n.X, v, tests)
				markIdent(pass, n.Y, v, tests)
			}
		case *ast.CallExpr:
			if isErrorsIsOf(pass, n, v) {
				markIdent(pass, n.Args[0], v, tests)
			}
		case *ast.IfStmt:
			if isErrorsIsCond(pass, n.Cond, v) {
				sentinel = append(sentinel, n.Body)
			}
		case *ast.CaseClause:
			if slices.ContainsFunc(n.List, func(e ast.Expr) bool { return isErrorsIsCond(pass, e, v) }) {
				sentinel = append(sentinel, n)
			}
		}
		return true
	})
	return tests, sentinel
}

// isErrorsIsCond is whether e, a condition, is errors.Is with v as its first argument.
func isErrorsIsCond(pass *analysis.Pass, e ast.Expr, v *types.Var) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	return ok && isErrorsIsOf(pass, call, v)
}

// isErrorsIsOf is whether call is errors.Is with v as its first argument.
func isErrorsIsOf(pass *analysis.Pass, call *ast.CallExpr, v *types.Var) bool {
	if f := calledFunc(pass, call); f == nil || !isPkgFunc(f, "errors", "Is") || len(call.Args) == 0 {
		return false
	}
	id, ok := ast.Unparen(call.Args[0]).(*ast.Ident)
	return ok && pass.TypesInfo.Uses[id] == v
}

// markIdent records e as a test of v when it is v itself.
func markIdent(pass *analysis.Pass, e ast.Expr, v *types.Var, tests map[*ast.Ident]bool) {
	if id, ok := ast.Unparen(e).(*ast.Ident); ok && pass.TypesInfo.Uses[id] == v {
		tests[id] = true
	}
}

// nextAssign is the position of the first assignment to v after from, or the end of body.
func nextAssign(pass *analysis.Pass, body *ast.BlockStmt, v *types.Var, from token.Pos) token.Pos {
	until := body.End()
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || as.Pos() <= from || as.Pos() >= until {
			return true
		}
		for _, lhs := range as.Lhs {
			if id, isIdent := lhs.(*ast.Ident); isIdent && pass.TypesInfo.ObjectOf(id) == v {
				until = as.Pos()
			}
		}
		return true
	})
	return until
}
