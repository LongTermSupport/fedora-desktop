package rules

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// selectorParser is the one function that reads claude's conversation selectors in a recorded
// launch.
const selectorParser = "selectorOf"

// conversationSelectors are claude's conversation selectors as a recorded launch spells them,
// and the prefixes of their `=` forms.
var conversationSelectors = map[string]bool{
	"-c": true, "--continue": true, "-r": true, "--resume": true, "--session-id": true,
	"--resume=": true, "--session-id=": true,
}

// SelectorParse keeps the reading of claude's conversation selectors in a recorded launch in
// registry's selectorOf.
var SelectorParse = &analysis.Analyzer{
	Name: "project_selector_parse",
	Doc: `project_selector_parse flags, outside test files, in internal/registry and in every
package that imports it (the readers of a session record's launch words), a constant string that
is one of claude's conversation selectors (-c, --continue, -r, --resume, --session-id, or the
prefix --resume= or --session-id=) used as an operand of == or !=, as a case of a tagged switch,
or as an argument of a function of package strings or slices, anywhere but registry's
selectorOf. The constant is matched by its value, so a named constant is seen.

-r and --resume take an optional value: a bare --resume opens Claude Code's picker, and the word
after it is its id only when it is not a flag. selectorOf says so; ResumeID read the selectors
for itself and took the next word whatever it was, so a session launched as
ccy --resume --teams dev1@team-a was recorded by ccy-sessions restore as resuming "--teams"
(fedora-desktop#411). Two readers of one grammar disagree as soon as either is changed.

Not caught: a selector held in a variable or a slice and compared through it, a map keyed by
selectors, and a package that reads launch words without importing the registry (one that is
handed the words as a []string). A package that never imports the registry parses some other
command line (ccc's own --continue, a teamchat CMD's presence check) and is not judged.

How to fix: ask selectorOf (or a registry function built on it: withoutSelectors, ResumeID),
and add any new selector there, so every reader takes the same words.`,
	Run:        perPass(runSelectorParse),
	ResultType: noResultType,
}

func runSelectorParse(pass *analysis.Pass) {
	path := pass.Pkg.Path()
	if !ownedBy(path, registryPkg) && !importsRegistry(pass.Pkg) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			if ownedBy(path, registryPkg) && funcName(d) == selectorParser {
				continue
			}
			ast.Inspect(d, func(n ast.Node) bool {
				reportSelectorReads(pass, n)
				return true
			})
		}
	}
}

// reportSelectorReads reports each selector constant n reads as a word compared.
func reportSelectorReads(pass *analysis.Pass, n ast.Node) {
	for _, e := range selectorOperands(pass, n) {
		if isSelectorConstant(pass, e) {
			pass.Reportf(e.Pos(), "claude's conversation selectors are read in registry's selectorOf alone: ask it, do not compare the words (project_selector_parse)")
		}
	}
}

// importsRegistry is whether the package imports internal/registry.
func importsRegistry(pkg *types.Package) bool {
	for _, imp := range pkg.Imports() {
		if ownedBy(imp.Path(), registryPkg) {
			return true
		}
	}
	return false
}

// selectorOperands are the expressions of n that are read as a word compared: the operands of
// == and !=, a tagged switch's cases, and the arguments of a strings or slices function.
func selectorOperands(pass *analysis.Pass, n ast.Node) []ast.Expr {
	switch n := n.(type) {
	case *ast.BinaryExpr:
		if n.Op == token.EQL || n.Op == token.NEQ {
			return []ast.Expr{n.X, n.Y}
		}
	case *ast.SwitchStmt:
		if n.Tag == nil {
			return nil
		}
		var out []ast.Expr
		for _, s := range n.Body.List {
			if cc, ok := s.(*ast.CaseClause); ok {
				out = append(out, cc.List...)
			}
		}
		return out
	case *ast.CallExpr:
		fn := calledFunc(pass, n)
		if fn != nil && fn.Pkg() != nil && (fn.Pkg().Path() == "strings" || fn.Pkg().Path() == "slices") {
			return n.Args
		}
	}
	return nil
}

// isSelectorConstant is whether e is a constant string naming a conversation selector.
func isSelectorConstant(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return false
	}
	return conversationSelectors[constant.StringVal(tv.Value)]
}
