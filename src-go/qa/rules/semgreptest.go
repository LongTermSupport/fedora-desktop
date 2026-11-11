package rules

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

// SemgrepTestRunner forbids proving a semgrep rule with semgrep's own test runner.
var SemgrepTestRunner = newSemgrepTestRunner(semgrepTestRunnerAllowances)

// semgrepTestRunnerAllowances are the functions project_semgrep_test_runner does not judge.
var semgrepTestRunnerAllowances = []allowance{
	{pkg: "qa/rules", fn: "checkSemgrepTestRunner", reason: "the rule itself, which names the runner to find it and runs nothing"},
}

func newSemgrepTestRunner(allow []allowance) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "project_semgrep_test_runner",
		Doc: `project_semgrep_test_runner flags a semgrep argv that runs semgrep's own test runner:
the --test flag in a function that also names semgrep, or the test subcommand right after
semgrep.

semgrep --test never passes on --timeout, --jobs or any other limit: it scans each fixture with
semgrep's default of 5 s of wall time per rule per file. On a loaded host a rule that is sound
runs past that, is aborted, and reports nothing, so its self-test fails as a Timeout or, when
another rule of the file still reports, as a "rule id mismatch" that hides the Timeout
(fedora-desktop#206). The osemgrep test subcommand takes no limit either. A gate that proves a
rule must set the limit it is judged under.

The check is by name: a string constant "semgrep" anywhere in the function makes every "--test"
constant in it a finding, so an argv built in parts or through a named constant is still seen.
Test files are not judged, nor a function in the semgrepTestRunnerAllowances table.

How to fix: prove the rule with semgrepreport's self-test, a scan run with TimeoutFlag whose
report is judged against the fixture's annotations.`,
		Run:        perPass(func(pass *analysis.Pass) { runSemgrepTestRunner(pass, allow) }),
		ResultType: noResultType,
	}
}

func runSemgrepTestRunner(pass *analysis.Pass, allow []allowance) {
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			if allowedIn(pass, funcName(decl), allow) {
				continue
			}
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
				checkSemgrepTestRunner(pass, fd)
			}
		}
	}
}

// checkSemgrepTestRunner reports the test runner in each semgrep argv of fd.
func checkSemgrepTestRunner(pass *analysis.Pass, fd *ast.FuncDecl) {
	namesSemgrep := false
	var flags []ast.Expr
	var subcommands []ast.Expr
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		var list []ast.Expr
		switch n := n.(type) {
		case *ast.CallExpr:
			list = n.Args
		case *ast.CompositeLit:
			list = n.Elts
		case ast.Expr:
			switch constText(pass, n) {
			case "semgrep":
				namesSemgrep = true
			case "--test":
				flags = append(flags, n)
			}
			return true
		}
		for i := 0; i+1 < len(list); i++ {
			if constText(pass, list[i]) == "semgrep" && constText(pass, list[i+1]) == "test" {
				subcommands = append(subcommands, list[i+1])
			}
		}
		return true
	})
	if !namesSemgrep {
		return
	}
	for _, e := range append(flags, subcommands...) {
		pass.Reportf(e.Pos(), "semgrep's test runner scans with a fixed 5 s limit no flag can raise, so a loaded host fails a sound rule: prove it with semgrepreport's self-test")
	}
}

// constText is the value of e when it is a string constant, else "".
func constText(pass *analysis.Pass, e ast.Expr) string {
	text, ok := stringConst(pass, e)
	if !ok {
		return ""
	}
	return text
}
