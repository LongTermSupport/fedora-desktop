package failpoint

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// declared is every Name constant of names.go, read from its syntax tree, so no list here can
// drift from the package's.
func declared(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "names.go", nil, 0)
	check(t, err)
	var names []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, isValue := spec.(*ast.ValueSpec)
			if !isValue || len(vs.Values) != 1 {
				t.Fatalf("names.go: a constant that is not one name and one value")
			}
			lit, isLit := vs.Values[0].(*ast.BasicLit)
			if !isLit || lit.Kind != token.STRING {
				t.Fatalf("names.go: %s is not a string literal", vs.Names[0].Name)
			}
			name, err := strconv.Unquote(lit.Value)
			check(t, err)
			names = append(names, name)
		}
	}
	return names
}

// n3c's points (PT1's list and n3c-D2's tenth), each declared once.
func TestTheDeclaredPointsAreN3csTen(t *testing.T) {
	want := []string{
		"keeper-link-writing", "link-in-remove", "link-after-remove-answered", "link-after-hand-out",
		"link-while-handed", "keeper-before-store-delete", "link-mid-end-sends", "keeper-before-end",
		"link-after-judge-before-persist",
	}
	got := declared(t)
	seen := map[string]bool{}
	for _, n := range got {
		if seen[n] {
			t.Errorf("%s is declared twice", n)
		}
		seen[n] = true
	}
	for _, n := range want {
		if !seen[n] {
			t.Errorf("%s is not declared", n)
		}
	}
	if len(got) != len(want) {
		t.Errorf("declared %v, want %v", got, want)
	}
}

// The gate: a release build of the member binary holds no point's name, so no failpoint can
// be armed in what is installed.
func TestAReleaseBuildHoldsNoPointName(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "teamchat")
	if _, err := execx.Combined(t.Context(), execx.OS{}, execx.Cmd{
		Name: "go",
		Args: []string{"build", "-tags", "teamchatrelease", "-o", bin, "../../../cmd/teamchat"},
	}); err != nil {
		t.Fatalf("release build: %v", err)
	}
	data, err := os.ReadFile(filepath.Clean(bin))
	check(t, err)
	// The scan reads the binary it built: a message cli prints is there.
	if !bytes.Contains(data, []byte("is not built in this binary")) {
		t.Fatal("the scan does not read the release binary")
	}
	for _, n := range declared(t) {
		if bytes.Contains(data, []byte(n)) {
			t.Errorf("the release binary holds the failpoint name %s", n)
		}
	}
}
