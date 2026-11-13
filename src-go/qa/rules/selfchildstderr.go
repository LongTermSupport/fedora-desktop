package rules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// SelfChildStderr forbids judging the stderr of a child of the test binary empty as it stands.
var SelfChildStderr = &analysis.Analyzer{
	Name: "project_self_child_stderr",
	Doc: `project_self_child_stderr flags, in the test files of a package whose tests run their own
test binary as a child (a test file names os.Executable, os.Args[0] or testfiles.Executable), a
comparison with the empty string of a stderr as it stands: an expression whose last selector is
Stderr, or string(...) of one, compared with == or != against "".

A test binary built with -cover whose -coverpkg names no package it links has the coverage
hooks and no meta-data, and every process of it, a child run as another program included,
writes "program not built with -cover" on stderr as it starts (internal/coverage/cfile's
emitMetaData), whether or not GOCOVERDIR is set. qa-changed runs go-qa in diff mode, whose
-coverpkg is the changed packages only, so a test that requires such a child's stderr to be
empty passes in every full run and fails in a diff run of an unrelated change (issue #155: the
member kit's hook tests, then the wiring end-to-end tests).

Not caught: a stderr held in a variable of another name and compared, len(stderr) == 0, a
bytes.Equal or a strings.Contains on it, and a package that runs the binary of another
package's tests. A comparison of a stderr of another program in such a package is flagged
too; reading it through the helper is harmless there.

How to fix: read the child's stderr through testchild.Stderr (src-go/internal/testchild), which
takes out that one line and nothing else:

	if r.Exit != 0 || testchild.Stderr(r.Stderr) != "" {`,
	Run:        perPass(runSelfChildStderr),
	ResultType: noResultType,
}

func runSelfChildStderr(pass *analysis.Pass) {
	var tests []*ast.File
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			tests = append(tests, f)
		}
	}
	if !runsSelf(pass, tests) {
		return
	}
	for _, f := range tests {
		ast.Inspect(f, func(n ast.Node) bool {
			cmp, ok := n.(*ast.BinaryExpr)
			if ok && (cmp.Op == token.EQL || cmp.Op == token.NEQ) &&
				(isEmptyString(pass, cmp.Y) && isRawStderr(cmp.X) || isEmptyString(pass, cmp.X) && isRawStderr(cmp.Y)) {
				pass.Reportf(cmp.OpPos, `stderr of a child of the test binary compared with empty (project_self_child_stderr): a test binary built with -cover and no instrumented package writes "program not built with -cover" on stderr in every process, as qa-changed's diff mode builds it; read the stderr through testchild.Stderr`)
			}
			return true
		})
	}
}

// runsSelf is whether a test file names os.Executable, os.Args[0] or testfiles.Executable.
func runsSelf(pass *analysis.Pass, files []*ast.File) bool {
	found := false
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Ident:
				if fn, ok := pass.TypesInfo.Uses[n].(*types.Func); ok &&
					(isPkgFunc(fn, "os", "Executable") || fn.Name() == "Executable" && fn.Pkg() != nil && fn.Pkg().Name() == "testfiles") {
					found = true
				}
			case *ast.IndexExpr:
				found = found || isOsArgs(pass, n.X) && isZero(pass, n.Index)
			}
			return !found
		})
	}
	return found
}

func isOsArgs(pass *analysis.Pass, e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	v, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Var)
	return ok && v.Name() == "Args" && v.Pkg() != nil && v.Pkg().Path() == "os"
}

// isRawStderr is whether e is x.Stderr, or string(x.Stderr), parentheses aside.
func isRawStderr(e ast.Expr) bool {
	e = ast.Unparen(e)
	if call, ok := e.(*ast.CallExpr); ok && len(call.Args) == 1 {
		if id, isIdent := call.Fun.(*ast.Ident); isIdent && id.Name == "string" {
			e = ast.Unparen(call.Args[0])
		}
	}
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Stderr"
}
