package rules

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

const (
	// sharedDueLimit is the constant that bounds the reports join; the rule applies to a package
	// that declares it, and to the value it has there.
	sharedDueLimit = "reportsJoinLimit"
	// sharedDueSettle is the test helper that waits for the join to have ended.
	sharedDueSettle = "reportsSettled"
)

// SharedDue forbids a test naming a due the reports join's bound shares before the join has ended.
var SharedDue = &analysis.Analyzer{
	Name: "project_shared_due",
	Doc: `project_shared_due flags, in a _test.go file of a package that declares the constant
reportsJoinLimit (teamchat's teamsync), a call of a fake clock's AwaitDue or Armed whose due is a
constant equal to it, unless the test function calls reportsSettled earlier in its text. A fake
clock is a type whose method set has Advance(time.Duration). The due is judged by its value, so
longPoll, reportsJoinLimit, 30*time.Second and 60*time.Second/2 are all the same one; a
function literal in the test is part of it, and a helper that names the due is judged on its own.

The team's reports join bounds itself by the injected clock for as long as the long-poll is held,
so while the join is in flight two waiters are due the same time. AwaitDue is answered by either:
a test that waits for the long-poll's hold can be told that the join's bound is armed, advance
the clock, and cancel the join in flight (a transient refusal, then an extra retry wait the test
did not plan for) while the hold is not yet armed. The test then waits for a timer that was
armed at the new time and never fires (TestARestoreUnderALongPoll and TestQS2... hung to the
package's 30-minute timeout in CI; locally the join had ended before the test asked). The
clock itself refuses only a due that two waiters share at the moment it is asked
(clock.ErrAmbiguousDue), so a join that has not been armed yet is not seen.

reportsSettled(t, tm, rooms) waits for the reports room's state and for the try that put it
there to have ended, so the join's bound is stopped and the due is the long-poll's alone.

Not caught: a due that is not a constant (a variable, a call), or a constant that equals the
limit only by coincidence of a different intent; a call in a non-test file (a harness helper);
a settle in a helper under another name; a join that is armed again after the settle (a second
AwaitDue after an outage is judged settled by the first call); a fake whose advance is spelled
otherwise; a package that bounds its join by a value it does not name reportsJoinLimit.

How to fix: call reportsSettled before the AwaitDue, or wait on a due the join does not share.`,
	Run:        perPass(runSharedDue),
	ResultType: noResultType,
}

func runSharedDue(pass *analysis.Pass) {
	limit, ok := pass.Pkg.Scope().Lookup(sharedDueLimit).(*types.Const)
	if !ok {
		return
	}
	for _, f := range pass.Files {
		if !isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if fd, isFunc := decl.(*ast.FuncDecl); isFunc && fd.Body != nil {
				judgeSharedDue(pass, fd, limit)
			}
		}
	}
}

// judgeSharedDue reports each call in fd that names the shared due before fd's first call of
// the settle helper, or when it never calls it.
func judgeSharedDue(pass *analysis.Pass, fd *ast.FuncDecl, limit *types.Const) {
	settled := token.NoPos
	var asks []*ast.CallExpr
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		if isPkgFunc(calledFunc(pass, call), pass.Pkg.Path(), sharedDueSettle) && (settled == token.NoPos || call.Pos() < settled) {
			settled = call.Pos()
		}
		if namesSharedDue(pass, call, limit) {
			asks = append(asks, call)
		}
		return true
	})
	for _, call := range asks {
		if settled == token.NoPos || call.Pos() < settled {
			pass.Reportf(call.Pos(), "names a due the reports join's bound shares (project_shared_due): the join may still be armed at %s, so AwaitDue or Armed can be answered by it; call %s first, or wait on a due the join does not share", sharedDueLimit, sharedDueSettle)
		}
	}
}

// namesSharedDue is whether call is a fake clock's AwaitDue or Armed with a due equal to limit.
func namesSharedDue(pass *analysis.Pass, call *ast.CallExpr, limit *types.Const) bool {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return false
	}
	fn := fakeClockMethod(pass, sel)
	if fn == nil || (fn.Name() != "AwaitDue" && fn.Name() != "Armed") {
		return false
	}
	for _, arg := range call.Args {
		tv := pass.TypesInfo.Types[arg]
		if tv.Value != nil && isDuration(tv.Type) && constant.Compare(constant.ToInt(tv.Value), token.EQL, constant.ToInt(limit.Val())) {
			return true
		}
	}
	return false
}
