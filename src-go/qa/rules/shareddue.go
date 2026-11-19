package rules

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	// sharedDueLimit is the constant that bounds the reports join; the rule applies to a package
	// that declares it, and to the value it has there.
	sharedDueLimit = "reportsJoinLimit"
	// sharedDueSettle is the test helper that waits for the join to have ended.
	sharedDueSettle = "reportsSettled"
)

// SharedDue forbids a test naming a due that two of its package's limits share, by which the
// fake clock can answer for the wrong timer.
var SharedDue = &analysis.Analyzer{
	Name: "project_shared_due",
	Doc: `project_shared_due flags, in a _test.go file, a call of a fake clock's AwaitDue or Armed
whose due is a constant equal to the value of two or more package-level time.Duration constants
the package under test declares in its non-test files (for an external test package, the package
it tests). A fake clock is a type whose method set has Advance(time.Duration). The due is judged
by its value, so RequestLimit, 10*time.Second and 20*time.Second/2 are all the same one; a
function literal in the test is part of it, and a helper that names the due is judged on its own.

Two limits with one value arm timers due the same time from the instant each is armed. AwaitDue
is answered by whichever is armed: a test that means the second can be told the first is armed,
advance the clock between the first's stop and the second's arming, and then wait for a timer
armed at the new time, which never fires. The clock refuses only a due two waiters share at the
moment it is asked (clock.ErrAmbiguousDue), so timers armed one after the other are not seen.
The originating forms: teamsync's reports join, bounded by the long-poll's 30 s
(TestARestoreUnderALongPoll hung in CI), and tlsterm's handshake and first-request bounds, both
10 s (TestT10ASilentClientIsClosedAtRequestLimit failed under a loaded -race -shuffle run).

In teamsync, reportsSettled(t, tm, rooms) called earlier in the test excuses the reports join's
due: it waits for the join's try to end, so the due is the long-poll's alone.

Not caught: a due that is not a constant (a variable, a call); a limit that is a variable, or a
constant of another type; a timer armed with a literal duration the package does not name; a call
in a non-test file (a harness helper); a fake whose advance is spelled otherwise.

How to fix: name the timer by the function that arms it, clock.(*Fake).AwaitDueFrom(ctx, d, fn),
which waits for a waiter due d armed in fn; in teamsync, call reportsSettled first.`,
	Run:        perPass(runSharedDue),
	ResultType: noResultType,
}

func runSharedDue(pass *analysis.Pass) {
	limits := sharedLimits(pass)
	// The reports join's bound shares its due with the long-poll's hold whether or not the hold
	// is a constant of the package, so its value is always a shared due.
	var join *types.Const
	if c, isConst := pass.Pkg.Scope().Lookup(sharedDueLimit).(*types.Const); isConst {
		join = c
		key := constant.ToInt(c.Val()).ExactString()
		if _, shared := limits[key]; !shared {
			limits[key] = []string{sharedDueLimit, "the long-poll's hold"}
		}
	}
	if len(limits) == 0 {
		return
	}
	for _, f := range pass.Files {
		if !isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if fd, isFunc := decl.(*ast.FuncDecl); isFunc && fd.Body != nil {
				judgeSharedDue(pass, fd, limits, join)
			}
		}
	}
}

// sharedLimits is each value two or more package-level Duration constants of the package under
// test share, with their names: the package's own non-test declarations, or for an external test
// package those of the package it tests.
func sharedLimits(pass *analysis.Pass) map[string][]string {
	pkgs := []*types.Package{pass.Pkg}
	if under, ok := strings.CutSuffix(pass.Pkg.Path(), "_test"); ok {
		for _, imp := range pass.Pkg.Imports() {
			if imp.Path() == under {
				pkgs = append(pkgs, imp)
			}
		}
	}
	byValue := map[string][]string{}
	for _, pkg := range pkgs {
		scope := pkg.Scope()
		for _, name := range scope.Names() {
			c, isConst := scope.Lookup(name).(*types.Const)
			if !isConst || !isDuration(c.Type()) || strings.HasSuffix(pass.Fset.Position(c.Pos()).Filename, "_test.go") {
				continue
			}
			key := constant.ToInt(c.Val()).ExactString()
			byValue[key] = append(byValue[key], name)
		}
	}
	for key, names := range byValue {
		if len(names) < 2 {
			delete(byValue, key)
		}
	}
	return byValue
}

// judgeSharedDue reports each call in fd that names a shared due; the reports join's due is
// excused from fd's first call of the settle helper on.
func judgeSharedDue(pass *analysis.Pass, fd *ast.FuncDecl, limits map[string][]string, join *types.Const) {
	settled := token.NoPos
	type ask struct {
		call  *ast.CallExpr
		value string
	}
	var asks []ask
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		if isPkgFunc(calledFunc(pass, call), pass.Pkg.Path(), sharedDueSettle) && (settled == token.NoPos || call.Pos() < settled) {
			settled = call.Pos()
		}
		if value, ok := namedDue(pass, call); ok {
			if _, shared := limits[value]; shared {
				asks = append(asks, ask{call, value})
			}
		}
		return true
	})
	for _, a := range asks {
		isJoin := join != nil && constant.ToInt(join.Val()).ExactString() == a.value
		if isJoin && settled != token.NoPos && settled < a.call.Pos() {
			continue
		}
		names := limits[a.value]
		slices.Sort(names)
		pass.Reportf(a.call.Pos(), "names a due the package's limits %s share (project_shared_due): AwaitDue or Armed can be answered by another limit's timer; name the timer by the function that arms it (AwaitDueFrom)%s", strings.Join(names, ", "), settleHint(isJoin))
	}
}

func settleHint(isJoin bool) string {
	if isJoin {
		return ", or call " + sharedDueSettle + " first"
	}
	return ""
}

// namedDue is the constant due of a fake clock's AwaitDue or Armed call, as an exact integer.
func namedDue(pass *analysis.Pass, call *ast.CallExpr) (string, bool) {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return "", false
	}
	fn := fakeClockMethod(pass, sel)
	if fn == nil || (fn.Name() != "AwaitDue" && fn.Name() != "Armed") {
		return "", false
	}
	for _, arg := range call.Args {
		tv := pass.TypesInfo.Types[arg]
		if tv.Value != nil && isDuration(tv.Type) {
			return constant.ToInt(tv.Value).ExactString(), true
		}
	}
	return "", false
}
