package rules

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

// clockCalls are the time functions that read the wall clock.
var clockCalls = []string{"Now", "Since", "Until"}

// TestClock forbids reading the wall clock in tests.
var TestClock = newTestClock(clockAllowances)

func newTestClock(allow []clockAllowance) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "project_testclock",
		Doc: `project_testclock forbids time.Now, time.Since and time.Until in _test.go files.

A test that reads the wall clock fails at the wrong moment: date-dependent tests failed in the
hour after midnight. Tests use a fixed time (time.Date) or an injected clock. A test that
genuinely needs wall time (proving a real sleep ran) is named, with its reason, in the
clockAllowances table of the rules package; it is never excused by a comment.

How to fix: replace the call with a fixed time, or inject the clock through the seam the
code under test already has (Now, Sleep).`,
		Run: perPass(func(pass *analysis.Pass) {
			runTestClock(pass, allow)
		}),
		ResultType: noResultType,
	}
}

func runTestClock(pass *analysis.Pass, allow []clockAllowance) {
	for _, f := range pass.Files {
		if !isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || allowed(pass, fd, allow) {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if call, isCall := n.(*ast.CallExpr); isCall {
					reportClockCall(pass, call)
				}
				return true
			})
		}
	}
}

func reportClockCall(pass *analysis.Pass, call *ast.CallExpr) {
	fn := calledFunc(pass, call)
	for _, name := range clockCalls {
		if isPkgFunc(fn, "time", name) {
			pass.Reportf(call.Pos(), "time.%s in a test: use a fixed time or an injected clock", name)
		}
	}
}

func allowed(pass *analysis.Pass, fd *ast.FuncDecl, allow []clockAllowance) bool {
	for _, a := range allow {
		if a.fn == fd.Name.Name && ownedBy(pass.Pkg.Path(), a.pkg) {
			return true
		}
	}
	return false
}
