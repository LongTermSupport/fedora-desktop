package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// GoroutineEnd forbids a goroutine that signals no end, which no owner can wait for.
var GoroutineEnd = &analysis.Analyzer{
	Name: "project_goroutine_end",
	Doc: `project_goroutine_end flags a go statement whose goroutine signals no end.

A goroutine signals its end when its body (a function literal, or a function or method of the
same package) defers close(ch), a Done() with no result (sync.WaitGroup.Done), or a function
literal that closes, sends or calls Done; or when its last statement is such a close, Done or
send and nothing in the body returns before it; or when its body is one call of a function
that signals its end. Flagged: every other go statement outside a _test.go file, a call of
another package's function or of a function value included (its end cannot be seen here).

A goroutine that signals no end cannot be waited for, so nothing proves it stopped: when its
stop is missed on some path it leaks for the life of the process, holding what it references.
The link's control reader (go l.read()) was stopped by l.quit, which only the path past the
launch closed; every link that ended earlier left its reader blocked on a send nobody would
receive (about 145 of them in one test binary).

Not flagged: sync.WaitGroup.Go and errgroup's Go (joined by Wait), time.AfterFunc and
context.AfterFunc callbacks, and goroutines started in tests (a test binary's leak check
answers for those).
Not caught: an end signalled and never awaited, or awaited on only some paths of the owner.

How to fix: give the goroutine an owner that stops it and waits for it on every path: start
it with a WaitGroup's Go, or defer close(done) in it and receive from done in the owner's
teardown, deferred before any return that follows the start.`,
	Run:        perPass(runGoroutineEnd),
	ResultType: noResultType,
}

func runGoroutineEnd(pass *analysis.Pass) {
	bodies := map[*types.Func]*ast.BlockStmt{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				if fn, isFunc := pass.TypesInfo.Defs[fd.Name].(*types.Func); isFunc {
					bodies[fn] = fd.Body
				}
			}
		}
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if g, ok := n.(*ast.GoStmt); ok && !callSignalsEnd(pass, bodies, g.Call, map[*types.Func]bool{}) {
				pass.Reportf(g.Pos(), "goroutine signals no end, so no owner can wait for it: defer close(done) or wg.Done in it, or start it with a WaitGroup's Go, and wait for it")
			}
			return true
		})
	}
}

// callSignalsEnd is whether the goroutine call runs signals its end; seen stops a recursion.
func callSignalsEnd(pass *analysis.Pass, bodies map[*types.Func]*ast.BlockStmt, call *ast.CallExpr, seen map[*types.Func]bool) bool {
	if lit, ok := call.Fun.(*ast.FuncLit); ok {
		return bodySignalsEnd(pass, bodies, lit.Body, seen)
	}
	fn := calledFunc(pass, call)
	if fn == nil {
		return false
	}
	fn = fn.Origin()
	body, ok := bodies[fn]
	if !ok || seen[fn] {
		return false
	}
	seen[fn] = true
	return bodySignalsEnd(pass, bodies, body, seen)
}

func bodySignalsEnd(pass *analysis.Pass, bodies map[*types.Func]*ast.BlockStmt, body *ast.BlockStmt, seen map[*types.Func]bool) bool {
	for _, s := range body.List {
		if d, ok := s.(*ast.DeferStmt); ok && deferSignalsEnd(pass, d.Call) {
			return true
		}
	}
	if len(body.List) == 0 {
		return false
	}
	last := body.List[len(body.List)-1]
	if isEndStmt(pass, last) && !returnsEarly(body) {
		return true
	}
	if len(body.List) == 1 {
		if es, ok := last.(*ast.ExprStmt); ok {
			if call, isCall := es.X.(*ast.CallExpr); isCall {
				return callSignalsEnd(pass, bodies, call, seen)
			}
		}
	}
	return false
}

// deferSignalsEnd is whether a deferred call is an end signal, or a function literal holding one.
func deferSignalsEnd(pass *analysis.Pass, call *ast.CallExpr) bool {
	lit, ok := call.Fun.(*ast.FuncLit)
	if !ok {
		return isEndCall(pass, call)
	}
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.SendStmt:
			found = true
		case *ast.CallExpr:
			found = found || isEndCall(pass, n)
		}
		return !found
	})
	return found
}

// isEndStmt is whether s is a send, a close or a Done.
func isEndStmt(pass *analysis.Pass, s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.SendStmt:
		return true
	case *ast.ExprStmt:
		call, ok := s.X.(*ast.CallExpr)
		return ok && isEndCall(pass, call)
	}
	return false
}

// isEndCall is whether call is the builtin close or a Done method with no parameter and no
// result (a WaitGroup's; a context's Done returns its channel and is not one).
func isEndCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		b, ok := pass.TypesInfo.Uses[fn].(*types.Builtin)
		return ok && b.Name() == "close"
	case *ast.SelectorExpr:
		m, ok := pass.TypesInfo.Uses[fn.Sel].(*types.Func)
		if !ok || m.Name() != "Done" {
			return false
		}
		sig, isSig := m.Type().(*types.Signature)
		return isSig && sig.Recv() != nil && sig.Params().Len() == 0 && sig.Results().Len() == 0
	}
	return false
}

// returnsEarly is whether body holds a return of its own (not a nested function literal's).
func returnsEarly(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			found = true
		}
		return !found
	})
	return found
}
