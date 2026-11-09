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
	FactTypes:  []analysis.Fact{new(commandErrFact)},
}

const failureDroppedMsg = "a command's failure is only tested, never carried (project_command_failure_dropped): put its error (exit code, stderr) in what the caller sees, or return it"

// commandErrFact marks a function whose error is a command's failure: it returns the error of
// a command it ran, directly or through another such function (systemd.State runs systemctl).
type commandErrFact struct{}

func (*commandErrFact) AFact() {}

func (*commandErrFact) String() string { return "returns a command's failure" }

func runCommandFailureDropped(pass *analysis.Pass) {
	if ownedByAny(pass.Pkg.Path(), execOwners) {
		return
	}
	markWrappers(pass)
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

// markWrappers exports commandErrFact for each function of the package that returns a
// command's failure, to a fixed point so a wrapper of a wrapper is marked too.
func markWrappers(pass *analysis.Pass) {
	for changed := true; changed; {
		changed = false
		for _, f := range pass.Files {
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
				if !ok || !errorLast(fn) || pass.ImportObjectFact(fn, new(commandErrFact)) || !returnsCommandErr(pass, fd.Body) {
					continue
				}
				pass.ExportObjectFact(fn, new(commandErrFact))
				changed = true
			}
		}
	}
}

// returnsCommandErr is whether body returns a command's failure: a run's call, or a variable
// a run's error went into.
func returnsCommandErr(pass *analysis.Pass, body *ast.BlockStmt) bool {
	errVars := map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && len(as.Rhs) == 1 && len(as.Lhs) > 0 {
			if call, isCall := as.Rhs[0].(*ast.CallExpr); isCall && runsCommand(pass, call) {
				if id, isIdent := as.Lhs[len(as.Lhs)-1].(*ast.Ident); isIdent {
					errVars[pass.TypesInfo.ObjectOf(id)] = true
				}
			}
		}
		return true
	})
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
		if ret, ok := n.(*ast.ReturnStmt); ok {
			for _, e := range ret.Results {
				found = found || holdsCommandErr(pass, e, errVars)
			}
		}
		return !found
	})
	return found
}

// holdsCommandErr is whether e is a run's call or names one of errVars.
func holdsCommandErr(pass *analysis.Pass, e ast.Expr, errVars map[types.Object]bool) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			found = found || runsCommand(pass, n)
		case *ast.Ident:
			found = found || errVars[pass.TypesInfo.Uses[n]]
		}
		return !found
	})
	return found
}

// runsCommand is whether call runs a command and returns its failure last: a function of a
// seam package that takes a Cmd, or one commandErrFact marks.
func runsCommand(pass *analysis.Pass, call *ast.CallExpr) bool {
	f := calledFunc(pass, call)
	if f == nil || f.Pkg() == nil || !errorLast(f) {
		return false
	}
	if !ownedByAny(f.Pkg().Path(), execOwners) {
		return pass.ImportObjectFact(f, new(commandErrFact))
	}
	sig, ok := f.Type().(*types.Signature)
	if !ok {
		return false
	}
	for p := range sig.Params().Variables() {
		if isCmdType(p.Type()) {
			return true
		}
	}
	return false
}

// errorLast is whether f's last result is an error.
func errorLast(f *types.Func) bool {
	sig, ok := f.Type().(*types.Signature)
	return ok && sig.Results().Len() > 0 && isErrorType(sig.Results().At(sig.Results().Len()-1).Type())
}

func isErrorType(t types.Type) bool {
	return types.Identical(t, types.Universe.Lookup("error").Type())
}

// carried is whether v, assigned at from, is used other than tested before it is next assigned,
// or on the right of that assignment, which is read before v is replaced (err = errors.Join(err,
// ...)). A use inside a branch errors.Is(v, ...) chose carries only that one failure, so it is
// not counted.
func carried(pass *analysis.Pass, body *ast.BlockStmt, v *types.Var, from token.Pos) bool {
	next := nextAssign(pass, body, v, from)
	until := body.End()
	if next != nil {
		until = next.Pos()
	}
	tests, sentinel := testsOf(pass, body, v)
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if ok && id.Pos() > from && (id.Pos() < until || onTheRight(next, id.Pos())) &&
			pass.TypesInfo.Uses[id] == v && !tests[id] && !within(id.Pos(), sentinel) {
			found = true
		}
		return !found
	})
	return found
}

// onTheRight is whether pos is in the right-hand side of as.
func onTheRight(as *ast.AssignStmt, pos token.Pos) bool {
	return as != nil && slices.ContainsFunc(as.Rhs, func(e ast.Expr) bool { return pos >= e.Pos() && pos < e.End() })
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

// nextAssign is the first assignment to v after from that every path past it takes, or nil:
// one in a branch that does not hold from (an if's body, the other branch of an else) may not
// run, so the failure may still be what v holds after it.
func nextAssign(pass *analysis.Pass, body *ast.BlockStmt, v *types.Var, from token.Pos) *ast.AssignStmt {
	until := body.End()
	var next *ast.AssignStmt
	var blocks []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			blocks = blocks[:len(blocks)-1]
			return true
		}
		blocks = append(blocks, n)
		as, ok := n.(*ast.AssignStmt)
		if !ok || as.Pos() <= from || as.Pos() >= until || !innermostHolds(blocks, from) {
			return true
		}
		for _, lhs := range as.Lhs {
			if id, isIdent := lhs.(*ast.Ident); isIdent && pass.TypesInfo.ObjectOf(id) == v {
				until, next = as.Pos(), as
			}
		}
		return true
	})
	return next
}

// innermostHolds is whether the innermost block or case of path (the nodes from body down to
// an assignment) holds pos.
func innermostHolds(path []ast.Node, pos token.Pos) bool {
	for _, n := range slices.Backward(path) {
		switch n.(type) {
		case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
			return n.Pos() <= pos && pos < n.End()
		}
	}
	return false
}
