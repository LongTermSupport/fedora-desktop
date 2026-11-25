package teamsync

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// The class this guards (issue #422): an event the link judges out counted as dropped with no
// reason anyone can read. Every drop goes through judged.drop, which counts it under its reason
// (status's `dropped_by`) and logs it; a bare `Counts.Dropped++` anywhere else in the package
// is a drop whose reason is lost.
func TestEveryDropIsCountedWithItsReason(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Name.Name == "drop" {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if bareDrop(n) {
					t.Errorf("%s: %s counts a drop without its reason; call judged.drop", fset.Position(n.Pos()), fn.Name.Name)
				}
				return true
			})
		}
	}
	if checked == 0 {
		t.Fatal("no source file was checked")
	}
}

// bareDrop is `<x>.Dropped++` or `<x>.Dropped += <n>`.
func bareDrop(n ast.Node) bool {
	var lhs ast.Expr
	switch s := n.(type) {
	case *ast.IncDecStmt:
		lhs = s.X
	case *ast.AssignStmt:
		if s.Tok != token.ADD_ASSIGN || len(s.Lhs) != 1 {
			return false
		}
		lhs = s.Lhs[0]
	default:
		return false
	}
	sel, ok := lhs.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Dropped"
}
