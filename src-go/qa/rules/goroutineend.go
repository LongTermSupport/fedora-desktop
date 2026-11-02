package rules

import (
	"go/ast"
	"go/constant"
	"go/token"
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
send and nothing in the body returns before it; or when every return of its own directly
follows a close, a Done or a send on a channel made buffered in this package (a variable set
once, by make with a constant capacity above 0, so the one send cannot block), and it cannot
fall off its end without one (its last statement is one, or a for with no condition and no
break or goto in it): such a body ends only through a signal that cannot block; or when its
body is one call of a function that signals its end. Flagged: every other go statement, a call
of another package's function or of a function value included (its end cannot be seen here).

A goroutine that signals no end cannot be waited for, so nothing proves it stopped: when its
stop is missed on some path it leaks for the life of the process, holding what it references.
The link's control reader (go l.read()) was stopped by l.quit, which only the path past the
launch closed; every link that ended earlier left its reader blocked on a send nobody would
receive (about 145 of them in one test binary).

Not flagged: sync.WaitGroup.Go and errgroup's Go (joined by Wait), time.AfterFunc and
context.AfterFunc callbacks, and goroutines started in the _test.go files of a test package
(the internal or the external one) whose own files hold a TestMain calling a leakcheck
package's Tests (the binary then fails when a test leaves one running).
Not caught: an end signalled and never awaited, or awaited on only some paths of the owner.

How to fix: give the goroutine an owner that stops it and waits for it on every path: start
it with a WaitGroup's Go, or defer close(done) in it and receive from done in the owner's
teardown, deferred before any return that follows the start. In a test, the package's
TestMain can run leakcheck.Tests instead.`,
	Run:        perPass(runGoroutineEnd),
	ResultType: noResultType,
}

// goEnds judges the go statements of one package.
type goEnds struct {
	pass   *analysis.Pass
	bodies map[*types.Func]*ast.BlockStmt
	// buffered is each variable set once, by make(chan T, n) with a constant n above 0.
	buffered map[types.Object]bool
}

func runGoroutineEnd(pass *analysis.Pass) {
	g := goEnds{pass: pass, bodies: map[*types.Func]*ast.BlockStmt{}, buffered: bufferedChans(pass)}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				if fn, isFunc := pass.TypesInfo.Defs[fd.Name].(*types.Func); isFunc {
					g.bodies[fn] = fd.Body
				}
			}
		}
	}
	checked := runsLeakcheck(pass)
	for _, f := range pass.Files {
		if checked && isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if gs, ok := n.(*ast.GoStmt); ok && !g.callSignalsEnd(gs.Call, map[*types.Func]bool{}) {
				pass.Reportf(gs.Pos(), "goroutine signals no end, so no owner can wait for it: defer close(done) or wg.Done in it, or start it with a WaitGroup's Go, and wait for it")
			}
			return true
		})
	}
}

// runsLeakcheck is whether a _test.go file of the package has a TestMain that calls Tests of a
// package named leakcheck.
func runsLeakcheck(pass *analysis.Pass) bool {
	for _, f := range pass.Files {
		if !isTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Name.Name != "TestMain" || fd.Body == nil {
				continue
			}
			found := false
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if call, isCall := n.(*ast.CallExpr); isCall {
					fn := calledFunc(pass, call)
					found = found || (fn != nil && fn.Name() == "Tests" && fn.Pkg() != nil && fn.Pkg().Name() == "leakcheck")
				}
				return !found
			})
			if found {
				return true
			}
		}
	}
	return false
}

// bufferedChans is every variable of the package set once, by make(chan T, n) with a constant
// n above 0; a variable assigned anywhere else, or whose address is taken, is not one.
func bufferedChans(pass *analysis.Pass) map[types.Object]bool {
	c := chanSets{pass: pass, made: map[types.Object]bool{}, assigned: map[types.Object]int{}}
	for _, f := range pass.Files {
		ast.Inspect(f, c.visit)
	}
	for obj := range c.made {
		if c.assigned[obj] != 1 {
			delete(c.made, obj)
		}
	}
	return c.made
}

// chanSets counts the assignments of each variable and notes those made buffered.
type chanSets struct {
	pass     *analysis.Pass
	made     map[types.Object]bool
	assigned map[types.Object]int
}

func (c chanSets) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.AssignStmt:
		for i, lhs := range n.Lhs {
			if id, ok := lhs.(*ast.Ident); ok {
				c.note(id, valueAt(n.Rhs, len(n.Lhs), i))
			}
		}
	case *ast.ValueSpec:
		for i, id := range n.Names {
			c.note(id, valueAt(n.Values, len(n.Names), i))
		}
	case *ast.UnaryExpr:
		if id, ok := n.X.(*ast.Ident); ok && n.Op == token.AND {
			c.note(id, nil)
		}
	}
	return true
}

func (c chanSets) note(id *ast.Ident, value ast.Expr) {
	obj := c.pass.TypesInfo.ObjectOf(id)
	if obj == nil {
		return
	}
	c.assigned[obj]++
	if value != nil && isBufferedMake(c.pass, value) {
		c.made[obj] = true
	}
}

// valueAt is the value assigned to the i-th of n names, or nil when one call assigns them all.
func valueAt(values []ast.Expr, n, i int) ast.Expr {
	if len(values) != n {
		return nil
	}
	return values[i]
}

// isBufferedMake is whether e is make(chan T, n) with a constant n above 0.
func isBufferedMake(pass *analysis.Pass, e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 2 {
		return false
	}
	id, isIdent := call.Fun.(*ast.Ident)
	if !isIdent {
		return false
	}
	if b, isBuiltin := pass.TypesInfo.Uses[id].(*types.Builtin); !isBuiltin || b.Name() != "make" {
		return false
	}
	if _, isChan := pass.TypesInfo.TypeOf(call.Args[0]).Underlying().(*types.Chan); !isChan {
		return false
	}
	tv, known := pass.TypesInfo.Types[call.Args[1]]
	if !known || tv.Value == nil {
		return false
	}
	n, exact := constant.Int64Val(tv.Value)
	return exact && n > 0
}

// callSignalsEnd is whether the goroutine call runs signals its end; seen stops a recursion.
func (g goEnds) callSignalsEnd(call *ast.CallExpr, seen map[*types.Func]bool) bool {
	if lit, ok := call.Fun.(*ast.FuncLit); ok {
		return g.bodySignalsEnd(lit.Body, seen)
	}
	fn := calledFunc(g.pass, call)
	if fn == nil {
		return false
	}
	fn = fn.Origin()
	body, ok := g.bodies[fn]
	if !ok || seen[fn] {
		return false
	}
	seen[fn] = true
	return g.bodySignalsEnd(body, seen)
}

func (g goEnds) bodySignalsEnd(body *ast.BlockStmt, seen map[*types.Func]bool) bool {
	for _, s := range body.List {
		if d, ok := s.(*ast.DeferStmt); ok && deferSignalsEnd(g.pass, d.Call) {
			return true
		}
	}
	if len(body.List) == 0 {
		return false
	}
	last := body.List[len(body.List)-1]
	if isEndStmt(g.pass, last) && !returnsEarly(body) {
		return true
	}
	if g.signalsOnEveryReturn(body) {
		return true
	}
	if len(body.List) == 1 {
		if es, ok := last.(*ast.ExprStmt); ok {
			if call, isCall := es.X.(*ast.CallExpr); isCall {
				return g.callSignalsEnd(call, seen)
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

// signalsOnEveryReturn is whether every return of body's own directly follows a signal that
// cannot block (a close, a Done, a send on a buffered channel) and body cannot fall off its end
// without one: its last statement is one, or a for with no condition and no break in it.
func (g goEnds) signalsOnEveryReturn(body *ast.BlockStmt) bool {
	last := body.List[len(body.List)-1]
	if f, ok := last.(*ast.ForStmt); !g.isUnblockingEnd(last) && (!ok || f.Cond != nil || breaks(f.Body)) {
		return false
	}
	returns, signalled := 0, 0
	ast.Inspect(body, func(n ast.Node) bool {
		var list []ast.Stmt
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			returns++
		case *ast.BlockStmt:
			list = n.List
		case *ast.CaseClause:
			list = n.Body
		case *ast.CommClause:
			list = n.Body
		}
		for i, s := range list {
			if _, isReturn := s.(*ast.ReturnStmt); isReturn && i > 0 && g.isUnblockingEnd(list[i-1]) {
				signalled++
			}
		}
		return true
	})
	return returns == signalled
}

// isUnblockingEnd is whether s is a close, a Done, or a send on a buffered channel.
func (g goEnds) isUnblockingEnd(s ast.Stmt) bool {
	if send, ok := s.(*ast.SendStmt); ok {
		id, isIdent := send.Chan.(*ast.Ident)
		return isIdent && g.buffered[g.pass.TypesInfo.ObjectOf(id)]
	}
	return isEndStmt(g.pass, s)
}

// breaks is whether body holds a break of its own (not a nested function literal's); a break
// of an inner switch, select or loop counts too, so a loop the rule cannot judge is flagged.
func breaks(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.BranchStmt:
			found = found || n.Tok == token.BREAK || n.Tok == token.GOTO
		}
		return !found
	})
	return found
}
