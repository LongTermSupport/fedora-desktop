package rules

import (
	"go/ast"
	"go/constant"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// prefixFuncs are the strings functions a hand-rolled home abbreviation is built from.
var prefixFuncs = []string{"Replace", "ReplaceAll", "TrimPrefix", "HasPrefix", "CutPrefix"}

// HomeAbbrev routes every home abbreviation through term.AbbrevHome.
var HomeAbbrev = &analysis.Analyzer{
	Name: "project_homeabbrev",
	Doc: `project_homeabbrev forbids a "~" string in a strings prefix or replace call outside internal/term.

strings.Replace, ReplaceAll, TrimPrefix, HasPrefix and CutPrefix with a string constant that is
"~" or begins with "~" are a hand-rolled home abbreviation. The bare prefix replace turned
/home/<user>X into ~X when another user's directory began with the home directory's name.

How to fix: term.AbbrevHome(path, home).`,
	Run:        perPass(runHomeAbbrev),
	ResultType: noResultType,
}

func runHomeAbbrev(pass *analysis.Pass) {
	if ownedBy(pass.Pkg.Path(), homeOwner) {
		return
	}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn := calledFunc(pass, call)
			for _, name := range prefixFuncs {
				if isPkgFunc(fn, "strings", name) {
					reportTilde(pass, call, name)
				}
			}
			return true
		})
	}
}

func reportTilde(pass *analysis.Pass, call *ast.CallExpr, name string) {
	for _, arg := range call.Args {
		tv, ok := pass.TypesInfo.Types[arg]
		if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
			continue
		}
		if s := constant.StringVal(tv.Value); strings.HasPrefix(s, "~") {
			pass.Reportf(call.Pos(), "strings.%s with %q: abbreviate home with term.AbbrevHome", name, s)
			return
		}
	}
}
