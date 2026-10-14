// Package rules holds this module's project rules (Defence Before Fix): go/analysis analyzers
// for the defect classes the ccy-teams and ccy-sessions ports actually hit. Each is proven on
// a fixture reproducing the defect (testdata) and run over src-go by scripts/qa-go.bash.
// CLAUDE/GoProjectRules.md is the catalogue.
//
// The tables below are the rules' data: which package owns which seam, and the narrow,
// reasoned exceptions. Nothing is suppressed in the code under check.
package rules

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"reflect"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// execOwner is the only package that may start a process: every external command goes
// through its runner so a fake sees it.
const execOwner = "internal/execx"

// homeOwner is the only package that may abbreviate the home directory.
const homeOwner = "internal/term"

// commandOwners maps a program to the package that owns every use of it, so its flags and
// target forms are decided once (sudo -n; tmux's =name: target; the gum and fzf prompts).
var commandOwners = map[string]string{
	"sudo":      "internal/sudo",
	"tmux":      "internal/tmuxccy",
	"gum":       "internal/ui",
	"fzf":       "internal/ui",
	"systemctl": "internal/systemd",
	"git":       "internal/gitx",
}

// clockAllowance excuses one test function from the fixed-clock rule. It is data, with the
// reason it needs wall time, never a comment in the test.
type clockAllowance struct {
	pkg    string // package path suffix, as for an owner
	fn     string // the test function
	reason string
}

var clockAllowances = []clockAllowance{
	{
		pkg:    "internal/tmuxccy",
		fn:     "TestTakeOverDefaultsToTheRealClockAndSleep",
		reason: "proves the default Sleep really sleeps; only the wall clock can show that",
	},
}

// noResult is what every rule hands the analysis framework: the rules only report.
type noResult struct{}

var noResultType = reflect.TypeFor[noResult]()

// perPass adapts a reporting function to an analyzer's Run.
func perPass(run func(*analysis.Pass)) func(*analysis.Pass) (any, error) {
	return func(pass *analysis.Pass) (any, error) {
		run(pass)
		return noResult{}, nil
	}
}

// All is every rule, in the order the driver registers them.
func All() []*analysis.Analyzer {
	return []*analysis.Analyzer{SeamExec, SeamCommands, TestClock, HomeAbbrev}
}

// ownedBy is whether the package path (the test variants of it included) is owner, matched
// as a path suffix on a whole element so the fixtures and the real module share the table.
func ownedBy(pkgPath, owner string) bool {
	path := pkgPath
	if i := strings.IndexByte(path, ' '); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimSuffix(path, "_test")
	return path == owner || strings.HasSuffix(path, "/"+owner)
}

// isTestFile is whether the file is a _test.go file.
func isTestFile(pass *analysis.Pass, f *ast.File) bool {
	return strings.HasSuffix(filepath.Base(pass.Fset.Position(f.Pos()).Filename), "_test.go")
}

// calledFunc is the function a call expression calls, or nil for a conversion, a builtin or
// a call through a value.
func calledFunc(pass *analysis.Pass, call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		id = fn.Sel
	case *ast.Ident:
		id = fn
	default:
		return nil
	}
	f, ok := pass.TypesInfo.Uses[id].(*types.Func)
	if !ok {
		return nil
	}
	return f
}

// isPkgFunc is whether f is the package-level function pkgPath.name (not a method).
func isPkgFunc(f *types.Func, pkgPath, name string) bool {
	if f == nil || f.Pkg() == nil || f.Pkg().Path() != pkgPath || f.Name() != name {
		return false
	}
	sig, ok := f.Type().(*types.Signature)
	return ok && sig.Recv() == nil
}
