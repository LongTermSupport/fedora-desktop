package rules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

const (
	contextPkg = "context"
	timePkg    = "time"
)

// realTimeBound is a function that bounds work on real time, and which of its arguments is
// the bound: a duration, or for a deadline a time.Time, which is never a constant.
type realTimeBound struct {
	pkg, recv, name string // recv is "" for a package-level function
	arg             int
	deadline        bool
}

// realTimeBounds are every way the standard library bounds work on real time.
var realTimeBounds = []realTimeBound{
	{pkg: contextPkg, name: "WithTimeout", arg: 1},
	{pkg: contextPkg, name: "WithTimeoutCause", arg: 1},
	{pkg: contextPkg, name: "WithDeadline", arg: 1, deadline: true},
	{pkg: contextPkg, name: "WithDeadlineCause", arg: 1, deadline: true},
	{pkg: timePkg, name: "After", arg: 0},
	{pkg: timePkg, name: "AfterFunc", arg: 0},
	{pkg: timePkg, name: "NewTimer", arg: 0},
	{pkg: timePkg, name: "NewTicker", arg: 0},
	{pkg: timePkg, name: "Tick", arg: 0},
	{pkg: timePkg, name: "Sleep", arg: 0},
	{pkg: timePkg, recv: "Timer", name: "Reset", arg: 0},
	{pkg: timePkg, recv: "Ticker", name: "Reset", arg: 0},
}

// testFailures are the testing methods that fail the test they are called on.
var testFailures = map[string]bool{"Fatal": true, "Fatalf": true, "Error": true, "Errorf": true, "FailNow": true}

// RealTimeBound holds every bound a test can shorten to an injected clock.
var RealTimeBound = newRealTimeBound(realTimeAllowances())

func newRealTimeBound(allow []allowance) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "project_realtime_bound",
		Doc: `project_realtime_bound flags a bound on real time that a test can shorten, and a test
that passes when real time runs out.

Outside the _test.go files: a call of context.WithTimeout, WithTimeoutCause, WithDeadline or
WithDeadlineCause, or of time.After, AfterFunc, NewTimer, NewTicker, Tick, Sleep or a Timer's or
Ticker's Reset, whose bound is not a compile-time constant (a field, a variable, a parameter, a
function's result; a deadline is never a constant), any of them taken as a value, and
os/exec.Cmd's WaitDelay assigned or keyed from a value set at run time. The
functions in the rules package's realClocks table (the real side of a clock seam, whose tests
replace it whole) and processBounds table (a bound on a real child process that no test
shortens) are not judged; each row has its reason.

In the _test.go files: a select case that receives from a real timer (time.After, time.Tick,
or the C of a *time.Timer or *time.Ticker) and does not fail: its body calls no Fatal, Fatalf,
Error, Errorf or FailNow, no panic, and returns nothing but nil. In the select that ends the body
of a bare for loop (no condition, init or post), a timer case that is empty or a bare continue
is the poll's gap and is not judged. And any time.AfterFunc, called or taken as a value.

A bound on real time that a test can shorten makes the test shorten it, and the short bound then
races the step the test asserts: an L3 probe's 20 ms bound fired before the probe reached a fake
root's hang under load ("1 of 4 probes hung at once", Plan 00209 N3). A select that passes when a
real timer fires asserts that nothing happened in that time, which under load is a test of
nothing. A bound whose length is a constant cannot be shortened, so it is not judged.

How to fix: bound the work on an injected clock (the package's clock seam: an AfterFunc, a Sleep
or a Hold the test replaces), so the test waits until the work is at the step and only then moves
the fake past the bound. A bound that only ever wraps a real child process gets a processBounds
row saying so. In a test, wait for the event or on the fake clock, and fail when a real timer
fires first.`,
		Run: perPass(func(pass *analysis.Pass) {
			runRealTimeBound(pass, allow)
		}),
		ResultType: noResultType,
	}
}

func runRealTimeBound(pass *analysis.Pass, allow []allowance) {
	for _, f := range pass.Files {
		test := isTestFile(pass, f)
		for _, decl := range f.Decls {
			switch {
			case test:
				polls := map[*ast.SelectStmt]bool{}
				ast.Inspect(decl, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.ForStmt:
						markPoll(n, polls)
					case *ast.SelectStmt:
						reportPassingTimers(pass, n, polls[n])
					case *ast.Ident:
						reportTimedStep(pass, n)
					}
					return true
				})
			case funcName(decl) == "" || !allowedIn(pass, funcName(decl), allow):
				reportShortenableBounds(pass, decl)
			}
		}
	}
}

// reportShortenableBounds flags each real-time bound in decl whose bound is set at run time,
// and each taken as a value.
func reportShortenableBounds(pass *analysis.Pass, decl ast.Decl) {
	called := map[*ast.Ident]bool{}
	ast.Inspect(decl, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			b, id, ok := realTimeCall(pass, n.Fun)
			if !ok {
				return true
			}
			called[id] = true
			if b.arg < len(n.Args) && (b.deadline || pass.TypesInfo.Types[n.Args[b.arg]].Value == nil) {
				pass.Reportf(n.Pos(), "%s on real time with a bound set at run time (project_realtime_bound): bound it on the injected clock", b.String())
			}
		case *ast.Ident:
			if b, ok := realTimeObj(pass.TypesInfo.Uses[n]); ok && !called[n] {
				pass.Reportf(n.Pos(), "%s taken as a value, its bound unseen (project_realtime_bound): bound it on the injected clock", b.String())
			}
		default:
			reportWaitDelays(pass, n)
		}
		return true
	})
}

// reportWaitDelays flags an assignment of exec.Cmd's WaitDelay, or a keyed literal of it, whose
// value is set at run time.
func reportWaitDelays(pass *analysis.Pass, n ast.Node) {
	switch n := n.(type) {
	case *ast.AssignStmt:
		for i, lhs := range n.Lhs {
			if sel, isSel := lhs.(*ast.SelectorExpr); isSel && i < len(n.Rhs) && isWaitDelayField(pass, sel) {
				reportWaitDelay(pass, n.Rhs[i])
			}
		}
	case *ast.KeyValueExpr:
		if key, isIdent := n.Key.(*ast.Ident); isIdent && isWaitDelay(pass.TypesInfo.Uses[key]) {
			reportWaitDelay(pass, n.Value)
		}
	}
}

func reportWaitDelay(pass *analysis.Pass, value ast.Expr) {
	if pass.TypesInfo.Types[value].Value == nil {
		pass.Reportf(value.Pos(), "os/exec.Cmd.WaitDelay on real time with a bound set at run time (project_realtime_bound): bound it on the injected clock")
	}
}

// markPoll records the select that ends the body of a bare for loop (no condition, no init, no
// post): such a loop leaves only through a case of that select, or a statement before it.
func markPoll(loop *ast.ForStmt, polls map[*ast.SelectStmt]bool) {
	if loop.Init != nil || loop.Cond != nil || loop.Post != nil || len(loop.Body.List) == 0 {
		return
	}
	if sel, ok := loop.Body.List[len(loop.Body.List)-1].(*ast.SelectStmt); ok {
		polls[sel] = true
	}
}

// reportPassingTimers flags each case of sel that receives from a real timer and does not fail.
// In the select that ends a bare for loop, a timer case that does nothing or only continues is
// the poll's gap: the loop goes round, and leaves only on another case or a check before it.
func reportPassingTimers(pass *analysis.Pass, sel *ast.SelectStmt, poll bool) {
	for _, stmt := range sel.Body.List {
		cc, ok := stmt.(*ast.CommClause)
		if !ok || cc.Comm == nil || !isRealTimer(pass, received(cc.Comm)) || fails(pass, cc.Body) || (poll && onlyGoesRound(cc.Body)) {
			continue
		}
		pass.Reportf(cc.Pos(), "a real timer is the passing case of a select (project_realtime_bound): wait for the event or on the fake clock, and fail when a real timer fires")
	}
}

// reportTimedStep flags time.AfterFunc in a test, called or taken as a value: a real timer that
// acts on its own (a cancel, a close, a write) stands in for the step it should wait for.
func reportTimedStep(pass *analysis.Pass, id *ast.Ident) {
	if b, ok := realTimeObj(pass.TypesInfo.Uses[id]); ok && b.recv == "" && b.pkg == timePkg && b.name == "AfterFunc" {
		pass.Reportf(id.Pos(), "time.AfterFunc in a test acts on real time (project_realtime_bound): act when the step is reached, or on the fake clock")
	}
}

// received is the channel a select case receives from, nil for a send.
func received(comm ast.Stmt) ast.Expr {
	var x ast.Expr
	switch s := comm.(type) {
	case *ast.ExprStmt:
		x = s.X
	case *ast.AssignStmt:
		if len(s.Rhs) == 1 {
			x = s.Rhs[0]
		}
	}
	if u, ok := ast.Unparen(x).(*ast.UnaryExpr); ok && u.Op == token.ARROW {
		return ast.Unparen(u.X)
	}
	return nil
}

// onlyGoesRound is whether a case body is empty or a bare continue.
func onlyGoesRound(body []ast.Stmt) bool {
	if len(body) == 0 {
		return true
	}
	br, ok := body[0].(*ast.BranchStmt)
	return len(body) == 1 && ok && br.Tok == token.CONTINUE && br.Label == nil
}

// isRealTimer is whether ch is time.After's or time.Tick's channel, or a *time.Timer's or
// *time.Ticker's C.
func isRealTimer(pass *analysis.Pass, ch ast.Expr) bool {
	switch ch := ch.(type) {
	case *ast.CallExpr:
		f := calledFunc(pass, ch)
		return isPkgFunc(f, timePkg, "After") || isPkgFunc(f, timePkg, "Tick")
	case *ast.SelectorExpr:
		if ch.Sel.Name != "C" {
			return false
		}
		ptr, ok := types.Unalias(pass.TypesInfo.TypeOf(ch.X)).(*types.Pointer)
		if !ok {
			return false
		}
		named, ok := types.Unalias(ptr.Elem()).(*types.Named)
		return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == timePkg &&
			(named.Obj().Name() == "Timer" || named.Obj().Name() == "Ticker")
	}
	return false
}

// fails is whether body fails the test: a testing failure, a panic, or a return of something
// other than nil.
func fails(pass *analysis.Pass, body []ast.Stmt) bool {
	failed := false
	for _, stmt := range body {
		ast.Inspect(stmt, func(n ast.Node) bool {
			if _, isLit := n.(*ast.FuncLit); isLit {
				return false
			}
			failed = failed || failsHere(pass, n)
			return !failed
		})
	}
	return failed
}

// failsHere is whether n itself fails the test.
func failsHere(pass *analysis.Pass, n ast.Node) bool {
	switch n := n.(type) {
	case *ast.CallExpr:
		if f := calledFunc(pass, n); f != nil && f.Pkg() != nil && f.Pkg().Path() == "testing" && testFailures[f.Name()] {
			return true
		}
		id, isIdent := ast.Unparen(n.Fun).(*ast.Ident)
		if !isIdent {
			return false
		}
		b, isBuiltin := pass.TypesInfo.Uses[id].(*types.Builtin)
		return isBuiltin && b.Name() == "panic"
	case *ast.ReturnStmt:
		return len(n.Results) > 0 && !isNil(pass, n.Results[len(n.Results)-1])
	}
	return false
}

func isNil(pass *analysis.Pass, e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	_, isNil := pass.TypesInfo.Uses[id].(*types.Nil)
	return isNil
}

// realTimeCall is the real-time bound fun names, and its identifier.
func realTimeCall(pass *analysis.Pass, fun ast.Expr) (realTimeBound, *ast.Ident, bool) {
	var id *ast.Ident
	switch fn := ast.Unparen(fun).(type) {
	case *ast.SelectorExpr:
		id = fn.Sel
	case *ast.Ident:
		id = fn
	default:
		return realTimeBound{}, nil, false
	}
	b, ok := realTimeObj(pass.TypesInfo.Uses[id])
	return b, id, ok
}

// realTimeObj is the real-time bound obj is, if it is one.
func realTimeObj(obj types.Object) (realTimeBound, bool) {
	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() == nil {
		return realTimeBound{}, false
	}
	recv := ""
	if sig, isSig := fn.Type().(*types.Signature); isSig && sig.Recv() != nil {
		t := sig.Recv().Type()
		if ptr, isPtr := t.(*types.Pointer); isPtr {
			t = ptr.Elem()
		}
		named, isNamed := types.Unalias(t).(*types.Named)
		if !isNamed {
			return realTimeBound{}, false
		}
		recv = named.Obj().Name()
	}
	for _, b := range realTimeBounds {
		if b.pkg == fn.Pkg().Path() && b.recv == recv && b.name == fn.Name() {
			return b, true
		}
	}
	return realTimeBound{}, false
}

func (b realTimeBound) String() string {
	if b.recv != "" {
		return b.pkg + "." + b.recv + "." + b.name
	}
	return b.pkg + "." + b.name
}
