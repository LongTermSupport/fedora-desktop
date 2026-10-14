package rules

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// clockCalls are the time functions that read the wall clock.
var clockCalls = []string{"Now", "Since", "Until"}

// TestClock forbids reading the wall clock in tests.
var TestClock = newTestClock(clockAllowances)

func newTestClock(allow []allowance) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "project_testclock",
		Doc: `project_testclock forbids time.Now, time.Since and time.Until in _test.go files.

Any reference is flagged, not only a call: a function value (n := time.Now), an argument
(stamp(time.Now)), a struct field, a package-level initialiser. A clock read hidden in a
non-test helper is not seen: production code takes its clock through a seam.

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

func runTestClock(pass *analysis.Pass, allow []allowance) {
	for _, f := range pass.Files {
		if !isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if fn := funcName(decl); fn != "" && allowedIn(pass, fn, allow) {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				if id, isIdent := n.(*ast.Ident); isIdent {
					reportClockRead(pass, id)
				}
				return true
			})
		}
	}
}

// reportClockRead flags any reference to a wall-clock function: a call, a function value, an
// argument, a package-level initialiser.
func reportClockRead(pass *analysis.Pass, id *ast.Ident) {
	fn, ok := pass.TypesInfo.Uses[id].(*types.Func)
	if !ok {
		return
	}
	for _, name := range clockCalls {
		if isPkgFunc(fn, "time", name) {
			pass.Reportf(id.Pos(), "time.%s in a test: use a fixed time or an injected clock", name)
		}
	}
}

// allowedIn is whether the allowance table excuses the function of this package.
func allowedIn(pass *analysis.Pass, fn string, allow []allowance) bool {
	for _, a := range allow {
		if a.fn == fn && ownedBy(pass.Pkg.Path(), a.pkg) {
			return true
		}
	}
	return false
}
