package rules

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// tildeFuncs are the package functions a hand-rolled home abbreviation is built from: the
// prefix, trim and replace forms of strings, the fmt formatters, and path joins.
var tildeFuncs = map[string][]string{
	"strings": {"Replace", "ReplaceAll", "NewReplacer", "TrimPrefix", "HasPrefix", "CutPrefix", "TrimLeft", "Trim"},
	"fmt":     {"Sprintf", "Sprint", "Sprintln", "Errorf", "Fprintf", "Fprint", "Fprintln", "Appendf", "Printf", "Print", "Println"},
	"path":    {"Join"},
	"path/filepath": {
		"Join",
	},
}

// tildeWriters are the buffer types whose Write methods build a string piece by piece.
var tildeWriters = map[string]string{"strings": "Builder", "bytes": "Buffer"}

// HomeAbbrev routes every home abbreviation through term.AbbrevHome.
var HomeAbbrev = &analysis.Analyzer{
	Name: "project_homeabbrev",
	Doc: `project_homeabbrev forbids building a "~" home abbreviation by hand outside internal/term.

Flagged, with a string constant that is "~" or begins with "~": strings.Replace, ReplaceAll,
NewReplacer, TrimPrefix, HasPrefix, CutPrefix, TrimLeft and Trim; any fmt formatter
(Sprintf("~%s", p)); path.Join and filepath.Join; WriteString, WriteByte and WriteRune on a
strings.Builder or bytes.Buffer; and a string concatenation whose leftmost operand is such a
constant ("~" + rest). The bare prefix replace turned /home/<user>X into ~X when another user's
directory began with the home directory's name.

Not caught: a "~" assembled from pieces (string(rune(126)), a "~" held in a variable or a
non-constant), and a "~" that does not begin the text.

How to fix: term.AbbrevHome(path, home).`,
	Run:        perPass(runHomeAbbrev),
	ResultType: noResultType,
}

func runHomeAbbrev(pass *analysis.Pass) {
	if ownedBy(pass.Pkg.Path(), homeOwner) {
		return
	}
	reported := map[token.Pos]bool{}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				checkTildeCall(pass, n)
			case *ast.BinaryExpr:
				checkTildeConcat(pass, n, reported)
			}
			return true
		})
	}
}

func checkTildeCall(pass *analysis.Pass, call *ast.CallExpr) {
	fn := calledFunc(pass, call)
	if fn == nil || fn.Pkg() == nil {
		return
	}
	if recv := receiverNamed(fn); recv != nil {
		if isTildeWriter(recv) && strings.HasPrefix(fn.Name(), "Write") {
			reportTilde(pass, call, fn.Name())
		}
		return
	}
	for _, name := range tildeFuncs[fn.Pkg().Path()] {
		if name == fn.Name() {
			reportTilde(pass, call, fn.Pkg().Name()+"."+name)
		}
	}
}

func isTildeWriter(t *types.Named) bool {
	obj := t.Obj()
	return obj.Pkg() != nil && tildeWriters[obj.Pkg().Path()] == obj.Name()
}

func reportTilde(pass *analysis.Pass, call *ast.CallExpr, name string) {
	for _, arg := range call.Args {
		if s, ok := tildeConstant(pass, arg); ok {
			pass.Reportf(call.Pos(), "%s with %q: abbreviate home with term.AbbrevHome", name, s)
			return
		}
	}
}

// tildeConstant is the constant text of expr when it is a string, or a rune or byte, that is
// "~" or begins with it.
func tildeConstant(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil {
		return "", false
	}
	if tv.Value.Kind() == constant.String {
		s := constant.StringVal(tv.Value)
		return s, strings.HasPrefix(s, "~")
	}
	if v, exact := constant.Int64Val(tv.Value); exact && v == '~' {
		return "~", true
	}
	return "", false
}

// checkTildeConcat reports a concatenation that begins with a "~" constant. The nested sums of
// a chain share one leftmost operand, which is reported once.
func checkTildeConcat(pass *analysis.Pass, b *ast.BinaryExpr, reported map[token.Pos]bool) {
	if b.Op != token.ADD || pass.TypesInfo.Types[b].Value != nil {
		return
	}
	if basic, ok := pass.TypesInfo.TypeOf(b).Underlying().(*types.Basic); !ok || basic.Info()&types.IsString == 0 {
		return
	}
	var leaf ast.Expr = b
	for {
		inner, ok := ast.Unparen(leaf).(*ast.BinaryExpr)
		if !ok || inner.Op != token.ADD {
			break
		}
		leaf = inner.X
	}
	if reported[leaf.Pos()] {
		return
	}
	if s, ok := tildeConstant(pass, leaf); ok {
		reported[leaf.Pos()] = true
		pass.Reportf(leaf.Pos(), "%q built by hand: abbreviate home with term.AbbrevHome", s)
	}
}
