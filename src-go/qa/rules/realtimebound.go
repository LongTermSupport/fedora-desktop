package rules

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// The tree whose tests drive a fake clock, and the package that owns its real one.
const (
	teamchatTree     = "internal/teamchat"
	teamchatClockPkg = "internal/teamchat/clock"
	contextPkg       = "context"
	timePkg          = "time"
	realSleep        = "systemClock.Sleep"
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

// realClocks are teamchat's real clocks outside its clock package: each is the production side
// of a clock seam whose callers hand it the duration and whose tests replace it whole.
var realClocks = []allowance{
	{pkg: "internal/teamchat/admin/wire", fn: realSleep, reason: "the real side of host.Clock and health.Clock"},
	{pkg: "internal/teamchat/matrix", fn: realSleep, reason: "the real side of matrix.Clock, the 429 and send retry waits"},
	{pkg: "internal/teamchat/matrix", fn: "systemTimers.AfterFunc", reason: "the real side of matrix.Timers, a try's time limit"},
	{pkg: "internal/teamchat/matrix/fakehs", fn: "realHold", reason: "the real side of fakehs's Hold, a held sync's timeout"},
	{pkg: "internal/teamchat/member/lock", fn: realSleep, reason: "the real side of lock.Clock, the wait between tries"},
	{pkg: "internal/teamchat/teamlock", fn: realSleep, reason: "the real side of teamlock.Clock, the wait between tries"},
}

// RealTimeBound holds teamchat's bounds to its injected clock.
var RealTimeBound = newRealTimeBound(realClocks)

func newRealTimeBound(allow []allowance) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "project_realtime_bound",
		Doc: `project_realtime_bound flags, in internal/teamchat and below, a bound on real time that
a test can shorten, and a test that passes when real time runs out.

Outside the _test.go files: a call of context.WithTimeout, WithTimeoutCause, WithDeadline or
WithDeadlineCause, or of time.After, AfterFunc, NewTimer, NewTicker, Tick, Sleep or a Timer's or
Ticker's Reset, whose bound is not a compile-time constant (a field, a variable, a parameter, a
function's result; a deadline is never a constant), and any of them taken as a value. The clock
package (internal/teamchat/clock) and the realClocks table of the rules package, the real side
of each clock seam, are not judged.

In the _test.go files: a select case that receives from a real timer (time.After, or the C of a
*time.Timer) and does not fail: its body calls no Fatal, Fatalf, Error, Errorf or FailNow, no
panic, and returns nothing but nil.

teamchat's tests drive a fake clock. A bound on real time that a test can shorten makes the test
shorten it, and the short bound then races the step the test asserts: the L3 probe's 20 ms bound
fired before a probe reached the fake root's hang under load ("1 of 4 probes hung at once",
Plan 00209 N3). A select that passes when a real timer fires asserts that nothing happened in
that time, which under load is a test of nothing. A bound whose length is a constant cannot be
shortened, so it is not judged.

How to fix: bound the work on the injected clock (clock.Clock's AfterFunc or Hold, and
clock.WithBound for a callee that must see the bound), so the test waits until the work is at
the step (BlockUntil, a channel the fake sends on) and only then moves the fake past the bound.
In a test, wait for the event or on the fake clock, and fail when a real timer fires first.`,
		Run: perPass(func(pass *analysis.Pass) {
			runRealTimeBound(pass, allow)
		}),
		ResultType: noResultType,
	}
}

func runRealTimeBound(pass *analysis.Pass, allow []allowance) {
	if !inTree(pass.Pkg.Path(), teamchatTree) || ownedBy(pass.Pkg.Path(), teamchatClockPkg) {
		return
	}
	for _, f := range pass.Files {
		test := isTestFile(pass, f)
		for _, decl := range f.Decls {
			switch {
			case test:
				ast.Inspect(decl, func(n ast.Node) bool {
					if sel, isSelect := n.(*ast.SelectStmt); isSelect {
						reportPassingTimers(pass, sel)
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
		}
		return true
	})
}

// reportPassingTimers flags each case of sel that receives from a real timer and does not fail.
func reportPassingTimers(pass *analysis.Pass, sel *ast.SelectStmt) {
	for _, stmt := range sel.Body.List {
		cc, ok := stmt.(*ast.CommClause)
		if !ok || cc.Comm == nil || !isRealTimer(pass, received(cc.Comm)) || fails(pass, cc.Body) {
			continue
		}
		pass.Reportf(cc.Pos(), "a real timer is the passing case of a select (project_realtime_bound): wait for the event or on the fake clock, and fail when a real timer fires")
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

// isRealTimer is whether ch is time.After's channel or a *time.Timer's C.
func isRealTimer(pass *analysis.Pass, ch ast.Expr) bool {
	switch ch := ch.(type) {
	case *ast.CallExpr:
		return isPkgFunc(calledFunc(pass, ch), timePkg, "After")
	case *ast.SelectorExpr:
		if ch.Sel.Name != "C" {
			return false
		}
		ptr, ok := types.Unalias(pass.TypesInfo.TypeOf(ch.X)).(*types.Pointer)
		if !ok {
			return false
		}
		named, ok := types.Unalias(ptr.Elem()).(*types.Named)
		return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == timePkg && named.Obj().Name() == "Timer"
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

// inTree is whether the package path (the test variants of it included) is tree or below it,
// matched on whole path elements so the fixtures and the real module share the scope.
func inTree(pkgPath, tree string) bool {
	path := pkgPath
	if i := strings.IndexByte(path, ' '); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimSuffix(path, "_test")
	return ownedBy(path, tree) || strings.HasPrefix(path, tree+"/") || strings.Contains(path, "/"+tree+"/")
}
