package rules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// FakeClockCount forbids waiting on a fake clock by the number of its armed waiters.
var FakeClockCount = &analysis.Analyzer{
	Name: "project_fakeclock_count",
	Doc: `project_fakeclock_count flags a wait on a fake clock by the number of its armed waiters.

A fake clock is a type whose method set has Advance(time.Duration). Flagged: any use (a call or
a method value) of its method that takes a context.Context and an integer (BlockUntil(ctx, n),
a count wait whatever it is called), and a call of its method that takes nothing and returns
an int (Waiters()) in the condition or body of a for statement, or in a return of a
function literal (until(func() bool { return clk.Waiters() == n })): a poll on the count.

A count says how many timers are armed, not which. The code under test arms, stops and re-arms
its own timers on its own goroutines, so the count a test waits for can be made by a timer it
did not mean (the link server's read limit, armed on accept before the hook's request was
written), the test advances too early, and the timer it meant is armed afterwards at the new
time and never fires: TestAnOpBeforeThePIDIsNotAnswered hung to the test binary's timeout.

Not flagged: a count asserted once (if clk.Waiters() != 0, in a range loop or a t.Run body),
an integer method without a context (StepWall(ms)), a reading that is not an int
(WallMS() int64), and a count on a type with no Advance.
Not caught: a count read into a variable and polled by a helper that takes the int, and a
fake whose advance is spelled otherwise.

How to fix: wait for the timer itself, by when it is due: clock.(*Fake).AwaitDue(ctx, d)
returns once a waiter due d from the fake's present reading is armed.`,
	Run:        perPass(runFakeClockCount),
	ResultType: noResultType,
}

func runFakeClockCount(pass *analysis.Pass) {
	reported := map[token.Pos]bool{}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				if fn := fakeClockMethod(pass, n); fn != nil && isCountWait(fn) {
					reportCount(pass, n, fn, reported)
				}
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
