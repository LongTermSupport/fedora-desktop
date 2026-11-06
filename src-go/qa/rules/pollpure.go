package rules

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// fakeClockMovers are the methods of a fake clock that move time.
var fakeClockMovers = []string{"Advance", "Suspend", "StepWall"}

// PollPure forbids a condition handed to leakcheck.Poll or leakcheck.Drive that acts on the world.
var PollPure = &analysis.Analyzer{
	Name: "project_poll_pure",
	Doc: `project_poll_pure flags a function literal handed as the condition of leakcheck.Poll or
leakcheck.Drive (in a _test.go file) that moves a fake clock (Advance, Suspend, StepWall on a type
named Fake), sends on a channel (a select's included) or closes one. A nested function literal is
part of the condition.

Poll and Drive fail a test when every other goroutine is blocked and the condition does not hold,
and read the condition again after the idle reading so that no goroutine has run between the two.
A condition that acts breaks that: its second read moves the world the idle reading judged still,
and the test fails although the move would have let the code under test go on. Whether it does
depends on whether the code under test had finished reacting to the first read when the idle
reading was taken, so the test passes on one run and fails on the next, and more often on a loaded
host (TestWaitRetriesARestoredItemThatCouldNotBeFetched advanced the clock inside the condition).

Not caught: a condition held in a variable or built by a function, a call that acts through
another function (a helper that advances), and an act that is not a clock move, a send or a
close (a write to a fake, a call that releases a gate).

How to fix: read in the condition, and act in leakcheck.Drive's step, which is called only when
the world is idle and the condition does not hold:

	leakcheck.Drive(t, answered, func() { clk.Advance(d) })`,
	Run:        perPass(runPollPure),
	ResultType: noResultType,
}

func runPollPure(pass *analysis.Pass) {
	for _, f := range pass.Files {
		if !isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && isIdleWait(pass, call) && len(call.Args) >= 2 {
				if cond, ok := ast.Unparen(call.Args[1]).(*ast.FuncLit); ok {
					reportActs(pass, cond)
				}
			}
			return true
		})
	}
}

// isIdleWait is whether call is leakcheck.Poll or leakcheck.Drive, the package's functions rather
// than a method of the same name.
func isIdleWait(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := calledFunc(pass, call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Name() != leakcheckPkgName {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	return ok && sig.Recv() == nil && (fn.Name() == "Poll" || fn.Name() == "Drive")
}

// reportActs reports each act in the condition's code.
func reportActs(pass *analysis.Pass, cond *ast.FuncLit) {
	ast.Inspect(cond.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SendStmt:
			pass.Reportf(n.Arrow, "the condition of an idle wait acts (project_poll_pure): it sends on a channel; the idle reading is followed by a second read of it, which would then move the world it judged still; read in the condition and act in leakcheck.Drive's step")
		case *ast.CallExpr:
			if act := actOf(pass, n); act != "" {
				pass.Reportf(n.Pos(), "the condition of an idle wait acts (project_poll_pure): it %s; the idle reading is followed by a second read of it, which would then move the world it judged still; read in the condition and act in leakcheck.Drive's step", act)
			}
		}
		return true
	})
}

// actOf names what the call does to the world, or "" when it is not one of the acts the rule knows.
func actOf(pass *analysis.Pass, call *ast.CallExpr) string {
	if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
		if b, ok := pass.TypesInfo.Uses[id].(*types.Builtin); ok && b.Name() == "close" {
			return "closes a channel"
		}
	}
	fn := calledFunc(pass, call)
	if fn == nil {
		return ""
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil || !slices.Contains(fakeClockMovers, fn.Name()) {
		return ""
	}
	recv := sig.Recv().Type()
	if p, ok := recv.(*types.Pointer); ok {
		recv = p.Elem()
	}
	if named, ok := recv.(*types.Named); ok && named.Obj().Name() == "Fake" {
		return "moves a fake clock"
	}
	return ""
}
