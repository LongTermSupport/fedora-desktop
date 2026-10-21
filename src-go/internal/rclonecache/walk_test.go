package rclonecache

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
}

// E17: every non-directory entry is a file; one is cached when its mirror exists.
func TestAnalyse(t *testing.T) {
	root := t.TempDir()
	dir, cache := filepath.Join(root, "mnt"), filepath.Join(root, "cache")
	writeFile(t, filepath.Join(dir, "a.raw"), 100)
	writeFile(t, filepath.Join(dir, "sub", "b.raw"), 300)
	writeFile(t, filepath.Join(dir, "sub", "c.raw"), 300)
	writeFile(t, filepath.Join(dir, "sub", "deep", "d.raw"), 50)
	writeFile(t, filepath.Join(dir, "e.raw"), 7)
	writeFile(t, filepath.Join(dir, "f.raw"), 1)
	writeFile(t, filepath.Join(dir, "g.raw"), 2)
	// a.raw is cached (a sparse cache holds fewer bytes), and so is d.raw.
	writeFile(t, filepath.Join(cache, "a.raw"), 40)
	writeFile(t, filepath.Join(cache, "sub", "deep", "d.raw"), 50)
	// A symlink to a directory counts as a file and is not followed (B7).
	// Its lstat size is its target's length, 3.
	if err := os.Symlink("sub", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	cov := analyse(dir, cache)
	if len(cov.unreadable) != 0 {
		t.Fatalf("unreadable %v", cov.unreadable)
	}
	if cov.totalFiles != 8 || cov.cachedFiles != 2 || cov.cachedSize != 90 || cov.totalSize != 763 {
		t.Errorf("counts %+v", cov)
	}
	// Largest first, equal sizes by path ascending (B8), at most five.
	got := make([]string, 0, len(cov.top))
	for _, f := range cov.top {
		got = append(got, strings.TrimPrefix(f.path, dir+"/"))
	}
	want := []string{"sub/b.raw", "sub/c.raw", "e.raw", "link", "g.raw"}
	if !slices.Equal(got, want) {
		t.Errorf("top %q, want %q", got, want)
	}
}

// E19: a query path that cannot be read at all claims no counts.
func TestAnalyseMissingRoot(t *testing.T) {
	cov := analyse(filepath.Join(t.TempDir(), "gone"), t.TempDir())
	if cov.totalFiles != 0 || len(cov.unreadable) != 1 {
		t.Fatalf("%+v", cov)
	}
}

// A mirror that cannot be asked about (not "does not exist") is unreadable, not uncached.
func TestAnalyseUnaskableMirror(t *testing.T) {
	root := t.TempDir()
	dir, cache := filepath.Join(root, "mnt"), filepath.Join(root, "cache")
	writeFile(t, filepath.Join(dir, "loop"), 10)
	if err := os.MkdirAll(cache, 0o750); err != nil {
		t.Fatal(err)
	}
	// A mirror that is a symlink to itself cannot be stat'ed: ELOOP, which root gets too.
	if err := os.Symlink("loop", filepath.Join(cache, "loop")); err != nil {
		t.Fatal(err)
	}
	cov := analyse(dir, cache)
	if cov.totalFiles != 1 || cov.cachedFiles != 0 || len(cov.unreadable) != 1 ||
		!strings.Contains(cov.unreadable[0].Error(), "cache") {
		t.Fatalf("%+v", cov)
	}
}

// A mirror below a cached file (ENOTDIR) is simply not cached.
func TestAnalyseMirrorBelowAFile(t *testing.T) {
	root := t.TempDir()
	dir, cache := filepath.Join(root, "mnt"), filepath.Join(root, "cache")
	writeFile(t, filepath.Join(dir, "sub", "a"), 10)
	writeFile(t, filepath.Join(cache, "sub"), 1)
	cov := analyse(dir, cache)
	if cov.totalFiles != 1 || cov.cachedFiles != 0 || len(cov.unreadable) != 0 {
		t.Fatalf("%+v", cov)
	}
}
