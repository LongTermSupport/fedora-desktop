package rules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// FakeClockCount forbids judging a fake clock's timers by the number of its armed waiters.
var FakeClockCount = newFakeClockCount(waiterCountAllowances)

func newFakeClockCount(allow []allowance) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "project_fakeclock_count",
		Doc: `project_fakeclock_count flags a test of a fake clock's timers by the number of its armed waiters.

A fake clock is a type whose method set has Advance(time.Duration). Flagged:

- a wait by count: any use (a call or a method value) of its method that takes a
  context.Context and an integer (BlockUntil(ctx, n), whatever it is called), and a call of its
  method that takes nothing and returns an int (Waiters()) in the condition or body of a for
  statement, or in a return of a function literal (until(func() bool { return clk.Waiters() ==
  n })): a poll on the count. Flagged everywhere, the fake's own package included;
- any other use (a call, a method value) of that count method outside the package that
  declares it (its internal and external tests are its own): an assertion (if clk.Waiters() !=
  1), a count kept in a variable, handed to a helper, switched on.

A count says how many timers are armed, not which. The code under test arms, stops and re-arms
its own timers, and any of them changes the count. A test that waits for a count advances too
early when another timer makes it, and the timer it meant is armed afterwards at the new time
and never fires (TestAnOpBeforeThePIDIsNotAnswered hung to the binary's timeout). A test that
asserts a count fails on correct code when another timer is armed beside the one it means
(the TIMEOUT tests' "waiters = 2, want the one TIMEOUT timer" once the delivery loop's timers
were merged in), and passes when the timer it means is missing and another makes up the number.

Not flagged: the fake's own package asserting its own count; an integer method without a
context (StepWall(ms)); a reading that is not an int (WallMS() int64, Armed(d) bool); a count on
a type with no Advance; and an assertion in a function the waiterCountAllowances table names,
whose claim is that nothing at all is armed (a client that must never wait), so every timer
is the one it means. A wait by count is never excused.
Not caught: a fake whose advance is spelled otherwise; a bool query (AwaitDue, Armed) that
names a due time another timer shares.

How to fix: name the timer by when it is due. clock.(*Fake).AwaitDue(ctx, d) waits until a
waiter due d from the fake's present reading is armed, and Armed(d) says whether one is now;
that a timer is not armed is asked of the code that owns it, not of the clock.`,
		Run: perPass(func(pass *analysis.Pass) {
			runFakeClockCount(pass, allow)
		}),
		ResultType: noResultType,
	}
}

func runFakeClockCount(pass *analysis.Pass, allow []allowance) {
	reported := map[token.Pos]bool{}
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			excused := allowedIn(pass, funcName(decl), allow)
			ast.Inspect(decl, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.SelectorExpr:
					reportCountUse(pass, n, excused, reported)
				case *ast.ForStmt:
					reportCountReads(pass, n.Cond, reported)
					reportCountReads(pass, n.Body, reported)
				case *ast.FuncLit:
					reportReturnedCounts(pass, n, reported)
				}
				return true
			})
		}
	}
}

// reportCountUse reports a count wait anywhere, and a count read outside the fake's own
// package unless the enclosing function is excused. The inspection is pre-order, so a poll
// that encloses the read has already reported it as a wait.
func reportCountUse(pass *analysis.Pass, sel *ast.SelectorExpr, excused bool, reported map[token.Pos]bool) {
	fn := fakeClockMethod(pass, sel)
	switch {
	case fn == nil:
	case isCountWait(fn):
		reportCount(pass, sel, fn, reported)
	case isCountRead(fn) && !excused && !declaredHere(pass, fn) && !reported[sel.Pos()]:
		reported[sel.Pos()] = true
		pass.Reportf(sel.Pos(), "counts the fake clock's waiters (%s): name the timer by when it is due (AwaitDue, Armed)", fn.Name())
	}
}

// declaredHere is whether fn belongs to the package under analysis or to its external tests.
func declaredHere(pass *analysis.Pass, fn *types.Func) bool {
	if fn.Pkg() == nil {
		return false
	}
	return ownedBy(pass.Pkg.Path(), fn.Pkg().Path())
}

// reportReturnedCounts reports each count read in a return of lit itself (a predicate polled by
// a helper); a function literal nested in it is judged on its own.
func reportReturnedCounts(pass *analysis.Pass, lit *ast.FuncLit, reported map[token.Pos]bool) {
	ast.Inspect(lit.Body, func(m ast.Node) bool {
		switch m := m.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			for _, r := range m.Results {
				reportCountReads(pass, r, reported)
			}
		}
		return true
	})
}

// reportCountReads reports each count read under n.
func reportCountReads(pass *analysis.Pass, n ast.Node, reported map[token.Pos]bool) {
	if n == nil {
		return
	}
	ast.Inspect(n, func(m ast.Node) bool {
		call, ok := m.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel {
			if fn := fakeClockMethod(pass, sel); fn != nil && isCountRead(fn) {
				reportCount(pass, sel, fn, reported)
			}
		}
		return true
	})
}

func reportCount(pass *analysis.Pass, sel *ast.SelectorExpr, fn *types.Func, reported map[token.Pos]bool) {
	if reported[sel.Pos()] {
		return
	}
	reported[sel.Pos()] = true
	pass.Reportf(sel.Pos(), "waits on a count of the fake clock's waiters (%s): wait for the timer with AwaitDue", fn.Name())
}

// fakeClockMethod is the method sel selects when its receiver is a fake clock, else nil.
func fakeClockMethod(pass *analysis.Pass, sel *ast.SelectorExpr) *types.Func {
	s, ok := pass.TypesInfo.Selections[sel]
	if !ok || s.Kind() == types.FieldVal {
		return nil
	}
	fn, ok := s.Obj().(*types.Func)
	if !ok || !hasAdvance(s.Recv()) {
		return nil
	}
	return fn
}

// hasAdvance is whether t, or a pointer to it, has the method Advance(time.Duration).
func hasAdvance(t types.Type) bool {
	for _, recv := range []types.Type{t, types.NewPointer(t)} {
		found := types.NewMethodSet(recv).Lookup(nil, "Advance")
		if found == nil {
			continue
		}
		sig, ok := found.Type().(*types.Signature)
		if ok && sig.Params().Len() == 1 && isDuration(sig.Params().At(0).Type()) {
			return true
		}
	}
	return false
}

func isDuration(t types.Type) bool {
	return isNamed(t, "time", "Duration")
}

func isContext(t types.Type) bool {
	return isNamed(t, "context", "Context")
}

func isNamed(t types.Type, pkg, name string) bool {
	named, ok := t.(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == pkg && named.Obj().Name() == name
}

// isCountWait is whether fn takes a context and an integer.
func isCountWait(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	ctx, count := false, false
	for v := range sig.Params().Variables() {
		ctx = ctx || isContext(v.Type())
		count = count || isInteger(v.Type())
	}
	return ctx && count
}

// isCountRead is whether fn takes nothing and returns one int, a count as len gives one; a
// clock's own readings are durations or int64 milliseconds.
func isCountRead(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}
	basic, isBasic := sig.Results().At(0).Type().(*types.Basic)
	return isBasic && basic.Kind() == types.Int
}

// isInteger is whether t is an integer that is not a duration (a count, not a time).
func isInteger(t types.Type) bool {
	if isDuration(t) {
		return false
	}
	basic, ok := t.Underlying().(*types.Basic)
	return ok && basic.Info()&types.IsInteger != 0
}
