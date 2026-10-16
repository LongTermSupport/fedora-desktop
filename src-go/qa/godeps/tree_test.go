package godeps

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, text := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIdenticalTreesAgree(t *testing.T) {
	want, got := t.TempDir(), t.TempDir()
	files := map[string]string{"modules.txt": "# ex.com/a v1.0.0\n", "ex.com/a/a.go": "package a\n"}
	writeTree(t, want, files)
	writeTree(t, got, files)
	diffs, n, err := compareTrees(want, got)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 0 || n != 2 {
		t.Fatalf("diffs = %v, files = %d", diffs, n)
	}
}

// G9: every missing, extra or different file is named.
func TestEveryDifferenceIsNamed(t *testing.T) {
	want, got := t.TempDir(), t.TempDir()
	writeTree(t, want, map[string]string{"same.go": "x", "changed.go": "new", "missing.go": "m"})
	writeTree(t, got, map[string]string{"same.go": "x", "changed.go": "old", "extra.go": "e"})
	diffs, _, err := compareTrees(want, got)
	if err != nil {
		t.Fatal(err)
	}
	wantDiffs := []string{"changed.go differs", "extra.go is not what go mod vendor writes", "missing.go is missing"}
	if !slices.Equal(diffs, wantDiffs) {
		t.Fatalf("diffs = %q, want %q", diffs, wantDiffs)
	}
}

// An absent tree is an empty one, so no requirements and no vendor directory agree.
func TestAbsentTreesAreEmpty(t *testing.T) {
	base := t.TempDir()
	diffs, n, err := compareTrees(filepath.Join(base, "none"), filepath.Join(base, "neither"))
	if err != nil || len(diffs) != 0 || n != 0 {
		t.Fatalf("diffs = %v, files = %d, err = %v", diffs, n, err)
	}
	writeTree(t, filepath.Join(base, "vendor"), map[string]string{"modules.txt": "x"})
	diffs, _, err = compareTrees(filepath.Join(base, "none"), filepath.Join(base, "vendor"))
	if err != nil || len(diffs) != 1 {
		t.Fatalf("diffs = %v, err = %v", diffs, err)
	}
}

func TestAnUnreadableTreeIsAnError(t *testing.T) {
	base := t.TempDir()
	writeTree(t, base, map[string]string{"file": "x"})
	if _, _, err := compareTrees(filepath.Join(base, "file"), t.TempDir()); err == nil {
		t.Fatal("a file where a tree should be is accepted")
	}
}
