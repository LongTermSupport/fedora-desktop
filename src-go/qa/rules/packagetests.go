package rules

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// PackageTests requires every package to have at least one _test.go file.
var PackageTests = &analysis.Analyzer{
	Name: "project_package_tests",
	Doc: `project_package_tests flags a package whose directory holds no _test.go file.

A package with no tests is code nothing exercises, and the module-wide coverage floor can hide it
behind the rest of the module: the total stays above the floor while the package is at 0%. Every
package, the cmd/ mains included, has tests, so the first untested package is a failure and not a
dip in a percentage. The check reads the package's directory, so it holds for a package whose
tests are all external (package x_test) too.

Only the presence of a test file is checked, not that it tests anything: a file with no Test
function passes. A directory that cannot be read is reported, never skipped.

How to fix: write the test first, in a _test.go file beside the code.`,
	Run:        perPass(runPackageTests),
	ResultType: noResultType,
}

func runPackageTests(pass *analysis.Pass) {
	if len(pass.Files) == 0 {
		return
	}
	for _, f := range pass.Files {
		// The test variant of a package is judged by its plain variant; a test binary's
		// generated main has a build-cache file name, not a .go one.
		if isTestFile(pass, f) || !strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, ".go") {
			return
		}
	}
	first := pass.Files[0]
	dir := filepath.Dir(pass.Fset.Position(first.Pos()).Filename)
	entries, err := os.ReadDir(dir)
	if err != nil {
		pass.Reportf(first.Name.Pos(), "cannot read the package directory to look for tests: %v", err)
		return
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), "_test.go") {
			return
		}
	}
	pass.Reportf(first.Name.Pos(), "package has no _test.go file; write its tests first")
}
