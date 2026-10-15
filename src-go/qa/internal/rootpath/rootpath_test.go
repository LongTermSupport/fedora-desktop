package rootpath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func realDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func link(t *testing.T, target string) string {
	t.Helper()
	l := filepath.Join(realDir(t), "view")
	if err := os.Symlink(target, l); err != nil {
		t.Fatal(err)
	}
	return l
}

func repoWithScripts(t *testing.T) string {
	t.Helper()
	root := realDir(t)
	if err := os.Mkdir(filepath.Join(root, "scripts"), 0o750); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAnAbsoluteCleanRootIsReturnedAsItIs(t *testing.T) {
	root := realDir(t)
	got, err := Resolve("/elsewhere", root)
	if err != nil || got != root {
		t.Fatalf("got %q, %v; want %q", got, err, root)
	}
}

func TestARelativeRootIsTakenAgainstTheWorkingDirectory(t *testing.T) {
	cwd := repoWithScripts(t)
	got, err := Resolve(cwd, "scripts")
	if err != nil || got != filepath.Join(cwd, "scripts") {
		t.Fatalf("got %q, %v", got, err)
	}
	got, err = Resolve(cwd, ".")
	if err != nil || got != cwd {
		t.Fatalf("dot: got %q, %v; want %q", got, err, cwd)
	}
}

func TestATrailingSlashAndDotDotSegmentsAreCleaned(t *testing.T) {
	root := repoWithScripts(t)
	got, err := Resolve("/", root+"/scripts/../")
	if err != nil || got != root {
		t.Fatalf("got %q, %v; want %q", got, err, root)
	}
}

func TestASymlinkedRootIsResolvedToItsRealPath(t *testing.T) {
	root := realDir(t)
	got, err := Resolve("/", link(t, root))
	if err != nil || got != root {
		t.Fatalf("got %q, %v; want %q", got, err, root)
	}
	if strings.Contains(got, "view") {
		t.Fatalf("the link name survived: %q", got)
	}
}

func TestARootThatDoesNotExistIsAnError(t *testing.T) {
	missing := filepath.Join(realDir(t), "nope")
	got, err := Resolve("/", missing)
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("got %q, %v; want an error naming %s", got, err, missing)
	}
}

func TestTheNamedRootWinsOverScripts(t *testing.T) {
	root := repoWithScripts(t)
	got, err := Root(realDir(t), root, "ignored/scripts")
	if err != nil || got != root {
		t.Fatalf("got %q, %v; want %q", got, err, root)
	}
}

func TestWithoutARootItIsTheParentOfScripts(t *testing.T) {
	root := repoWithScripts(t)
	for name, scripts := range map[string]string{
		"absolute":       filepath.Join(root, "scripts"),
		"trailing slash": filepath.Join(root, "scripts") + "/",
		"relative":       "scripts",
	} {
		got, err := Root(root, "", scripts)
		if err != nil || got != root {
			t.Errorf("%s: got %q, %v; want %q", name, got, err, root)
		}
	}
}

func TestWithNeitherItIsTheWorkingDirectory(t *testing.T) {
	root := realDir(t)
	got, err := Root(root, "", "")
	if err != nil || got != root {
		t.Fatalf("got %q, %v; want %q", got, err, root)
	}
}

func TestAGateWithItsOwnResolverSeamGetsTheSameRulesOnFakePaths(t *testing.T) {
	eval := func(p string) (string, error) {
		if p == "/links/view" {
			return "/real/repo/", nil
		}
		return "", os.ErrNotExist
	}
	got, err := RootVia(eval, "/cwd", "/links/view", "")
	if err != nil || got != "/real/repo" {
		t.Fatalf("got %q, %v", got, err)
	}
	got, err = RootVia(eval, "/cwd", "", "/links/view/scripts/")
	if err != nil || got != "/real/repo" {
		t.Fatalf("scripts parent: got %q, %v", got, err)
	}
	if _, err = RootVia(eval, "/cwd", "/nowhere", ""); err == nil || !strings.Contains(err.Error(), "/nowhere") {
		t.Fatalf("an unresolvable root must be an error naming it, got %v", err)
	}
}

func TestTheParentOfAScriptsDirectoryReachedThroughASymlinkIsResolved(t *testing.T) {
	root := repoWithScripts(t)
	got, err := Root("/", "", filepath.Join(link(t, root), "scripts"))
	if err != nil || got != root {
		t.Fatalf("got %q, %v; want %q", got, err, root)
	}
}
