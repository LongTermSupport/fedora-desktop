package pki

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// production is the package's non-test source files.
func production(t *testing.T) (*token.FileSet, []*ast.File) {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, parseErr := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatal("no production source files found")
	}
	return fset, files
}

// P11: the package does no file or network I/O: the host stores the bytes. Package net is
// there for net.IP, the type a certificate's address names are, and for nothing else.
func TestP11PkiImportsNoFileOrNetworkPackage(t *testing.T) {
	forbidden := []string{"os", "io/ioutil", "io/fs", "path/filepath", "net/http", "syscall", "os/exec", "crypto/tls"}
	fset, files := production(t)
	for _, f := range files {
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if slices.Contains(forbidden, path) {
				t.Errorf("%s imports %s", f.Name.Name, path)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == "net" && sel.Sel.Name != "IP" {
				t.Errorf("%s uses net.%s: only net.IP is for certificates", fset.Position(sel.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
}

// P1: the only reading of time is the Maker's Clock, so a test with a fake clock decides every
// validity.
func TestP1PkiReadsTimeOnlyThroughItsClock(t *testing.T) {
	fset, files := production(t)
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, isIdent := sel.X.(*ast.Ident); isIdent && pkg.Name == "time" && (sel.Sel.Name == "Now" || sel.Sel.Name == "Since" || sel.Sel.Name == "Until") {
				t.Errorf("%s reads the wall clock with time.%s", fset.Position(sel.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
}
