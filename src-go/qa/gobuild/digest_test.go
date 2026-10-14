package gobuild

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const toyModule = "example.test/mod"

// put writes one file under <root>/src-go.
func put(t *testing.T, root, name, text string) {
	t.Helper()
	path := filepath.Join(root, "src-go", filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func sum(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

// toyTree is a module whose command imports one package that imports another; a third is unused.
func toyTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put(t, root, "go.mod", "module "+toyModule+"\n\ngo 1.26\n")
	put(t, root, "cmd/alpha/main.go", "package main\n\nimport (\n\t\"fmt\"\n\n\t\""+toyModule+"/internal/a\"\n)\n\nfunc main() { fmt.Println(a.A) }\n")
	put(t, root, "internal/a/a.go", "// Package a. import \""+toyModule+"/internal/unused\"\npackage a\n\nimport (\n\t// \""+toyModule+"/internal/unused\"\n\tb \""+toyModule+"/internal/b\"\n\t_ \"embed\"\n)\n\nvar A = b.B\n")
	put(t, root, "internal/b/b.go", "package b\n\nimport \"strings\"\n\nvar B = strings.ToUpper(\"b\")\n")
	put(t, root, "internal/b/b_test.go", "package b\n")
	put(t, root, "internal/b/usage.txt", "text the package embeds\n")
	put(t, root, "internal/unused/u.go", "package unused\n")
	return root
}

func mustDigest(t *testing.T, root, command string) string {
	t.Helper()
	digest, err := SourceDigest(root, command)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func TestTheClosureIsTheCommandAndWhatItImportsHoweverDeep(t *testing.T) {
	got, err := Closure(toyTree(t), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cmd/alpha", "internal/a", "internal/b"}; !slices.Equal(got, want) {
		t.Fatalf("closure %v, want %v", got, want)
	}
}

func TestTheFilesAreGoModAndEveryFileOfAClosurePackageButTests(t *testing.T) {
	got, err := Files(toyTree(t), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cmd/alpha/main.go", "go.mod", "internal/a/a.go", "internal/b/b.go", "internal/b/usage.txt"}
	if !slices.Equal(got, want) {
		t.Fatalf("files %v, want %v", got, want)
	}
}

func TestTheDigestIsTheSha256OfPathNulFileHashNewlineInPathOrder(t *testing.T) {
	root := toyTree(t)
	files, err := Files(root, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	var listing strings.Builder
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, "src-go", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&listing, "%s\x00%s\n", name, sum(data))
	}
	if got, want := mustDigest(t, root, "alpha"), sum([]byte(listing.String())); got != want {
		t.Fatalf("digest %s, want %s", got, want)
	}
}

func TestAChangeToAFileTheBinaryIsBuiltFromChangesTheDigest(t *testing.T) {
	root := toyTree(t)
	for name, text := range map[string]string{
		"internal/b/b.go":      "package b\n\nvar B = \"other\"\n",
		"internal/b/usage.txt": "other text\n",
		"go.mod":               "module " + toyModule + "\n\ngo 1.27\n",
		"cmd/alpha/main.go":    "package main\n\nfunc main() {}\n",
	} {
		before := mustDigest(t, root, "alpha")
		put(t, root, name, text)
		if mustDigest(t, root, "alpha") == before {
			t.Errorf("a change to %s left the digest", name)
		}
	}
}

func TestAChangeTheBinaryDoesNotSeeLeavesTheDigest(t *testing.T) {
	root := toyTree(t)
	before := mustDigest(t, root, "alpha")
	put(t, root, "internal/unused/u.go", "package unused\n\nvar X = 1\n")
	put(t, root, "internal/b/b_test.go", "package b\n\nvar T = 1\n")
	put(t, root, "internal/b/testdata/deep/f.go", "package deep\n")
	if after := mustDigest(t, root, "alpha"); after != before {
		t.Fatalf("digest moved from %s to %s", before, after)
	}
}

func TestAPackageThatStopsBeingImportedLeavesTheClosure(t *testing.T) {
	root := toyTree(t)
	put(t, root, "internal/a/a.go", "package a\n\nvar A = 1\n")
	got, err := Closure(root, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cmd/alpha", "internal/a"}; !slices.Equal(got, want) {
		t.Fatalf("closure %v, want %v", got, want)
	}
}

func TestAnImportOfAPackageThatIsNotThereIsAnErrorNamingIt(t *testing.T) {
	root := toyTree(t)
	put(t, root, "internal/a/a.go", "package a\n\nimport _ \""+toyModule+"/internal/gone\"\n")
	if _, err := SourceDigest(root, "alpha"); err == nil || !strings.Contains(err.Error(), "internal/gone") {
		t.Fatalf("error %v does not name internal/gone", err)
	}
}

func TestACheckoutWithNoModuleFileIsAnErrorNamingIt(t *testing.T) {
	root := toyTree(t)
	if err := os.Remove(filepath.Join(root, "src-go", "go.mod")); err != nil {
		t.Fatal(err)
	}
	if _, err := SourceDigest(root, "alpha"); err == nil || !strings.Contains(err.Error(), "go.mod") {
		t.Fatalf("error %v does not name go.mod", err)
	}
}

func TestAModuleFileThatNamesNoModuleIsAnError(t *testing.T) {
	root := toyTree(t)
	put(t, root, "go.mod", "go 1.26\n")
	if _, err := Closure(root, "alpha"); err == nil || !strings.Contains(err.Error(), "names no module") {
		t.Fatalf("error %v", err)
	}
}

func TestASourceFileThatDoesNotParseIsAnErrorNamingIt(t *testing.T) {
	root := toyTree(t)
	put(t, root, "internal/b/b.go", "this is not Go\n")
	if _, err := Closure(root, "alpha"); err == nil || !strings.Contains(err.Error(), "internal/b/b.go") {
		t.Fatalf("error %v does not name the file", err)
	}
}

func TestAnotherCommandHasItsOwnClosureAndDigest(t *testing.T) {
	root := toyTree(t)
	put(t, root, "cmd/other/main.go", "package main\n\nimport _ \""+toyModule+"/internal/unused\"\n")
	got, err := Closure(root, "other")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cmd/other", "internal/unused"}; !slices.Equal(got, want) {
		t.Fatalf("closure %v, want %v", got, want)
	}
	before := mustDigest(t, root, "alpha")
	if mustDigest(t, root, "other") == before {
		t.Fatal("two commands with different closures share a digest")
	}
	put(t, root, "internal/unused/u.go", "package unused\n\nvar X = 1\n")
	if after := mustDigest(t, root, "alpha"); after != before {
		t.Fatal("a package alpha does not import moved its digest")
	}
}

func TestACommandThatIsNotThereIsAnErrorNamingIt(t *testing.T) {
	if _, err := SourceDigest(toyTree(t), "nope"); err == nil || !strings.Contains(err.Error(), "cmd/nope") {
		t.Fatalf("error %v does not name cmd/nope", err)
	}
}
