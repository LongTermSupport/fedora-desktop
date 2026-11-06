package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// TestSpin forbids a wait in a test that yields in a loop and has no way to fail.
var TestSpin = &analysis.Analyzer{
	Name: "project_test_spin",
	Doc: `project_test_spin flags a for loop in a _test.go file that yields with runtime.Gosched and
does not ask leakcheck.Idle anywhere in the loop (a nested loop included). The yield is named
by its function object, so an import alias does not hide it, and a reference counts as well as
a call (the function handed on as a value). A yield belongs to the innermost for statement
holding it; a range loop ends by itself and is not judged; a function literal in the loop is
part of it.

A wait that spins on a condition and yields has no exit but the condition. When the condition
can never hold it burns one CPU until the package's timeout (TestSD3_8bAQueuedSendOutlivesA401
ran 29 minutes at [runnable] in the engine's until, twice, in CI's failpoint stage), and the
only trace is a timeout panic that blames nobody. The exits that look like failure paths are
not: t.Context() ends only when the test does, so a guard on it never fires while the test's
own goroutine spins, and a count of polls is a bound on time in disguise that a loaded host
shortens.

leakcheck.Idle is the failure path that cannot misfire: it is true only when every other
goroutine is blocked on a channel, a select, a lock or a wait group, so nothing can change the
condition any more. A goroutine that runs, is runnable, sleeps, or waits in a system call or
the network poller keeps it false.

Not caught: a poll that does not yield (a loop of round trips to another goroutine, as
waitFor was), a yield taken before the loop and called in it through a variable
(sched := runtime.Gosched), a spin made of recursion or goto, and a loop in a non-test file
that a test calls (a harness helper).

How to fix: wait on the event the code under test produces (a channel the fake closes,
clock.AwaitDue, a poke), or keep the poll and ask leakcheck.Idle in it, failing when it is true:

	for !cond() {
		if leakcheck.Idle() {
			t.Fatal("every goroutine is blocked and the condition does not hold")
		}
		runtime.Gosched()
	}`,
	Run:        perPass(runTestSpin),
	ResultType: noResultType,
}

func runTestSpin(pass *analysis.Pass) {
	for _, f := range pass.Files {
		if !isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if loop, ok := n.(*ast.ForStmt); ok && yields(pass, loop) && !asksIdle(pass, loop) {
				pass.Reportf(loop.For, "spin wait with no failure path (project_test_spin): this loop yields with runtime.Gosched and ends only when its condition holds; ask leakcheck.Idle in it and fail when it is true, or wait on an event (a channel, clock.AwaitDue)")
			}
			return true
		})
	}
}

// yields is whether the loop's own code names runtime.Gosched: in its condition, post statement
// or body, a function literal included, but not in a loop nested in it.
func yields(pass *analysis.Pass, loop *ast.ForStmt) bool {
	found := false
	ast.Inspect(loop, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ForStmt:
			return n == loop
		case *ast.RangeStmt:
			return false
		case *ast.Ident:
			if fn, ok := pass.TypesInfo.Uses[n].(*types.Func); ok && isPkgFunc(fn, "runtime", "Gosched") {
				found = true
			}
		}
		return !found
	})
	return found
}

// asksIdle is whether any code of the loop, nested loops included, names leakcheck.Idle.
func asksIdle(pass *analysis.Pass, loop *ast.ForStmt) bool {
	found := false
	ast.Inspect(loop, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			fn, isFunc := pass.TypesInfo.Uses[id].(*types.Func)
			found = found || isFunc && fn.Name() == "Idle" && fn.Pkg() != nil && fn.Pkg().Name() == leakcheckPkgName
		}
		return !found
	})
	return found
}
