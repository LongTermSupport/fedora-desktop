package unitsandbox

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// repo is the repository root, from this package's directory.
const repo = "../../.."

// tmpfsKey is the directive whose presence makes a file one this rule reads.
const tmpfsKey = "TemporaryFileSystem="

// notUnits are the trees git does not publish (.git, the untracked scratch, the hooks daemon's
// own install, dependencies) and src-go, whose renderers are judged on what they render
// (goRenderers). Plan folders' probes and tests are swept: a probe starts transient units.
var notUnits = []string{".git", ".claude", "untracked", "node_modules", "src-go"}

// knownDirectories are the bind sources the sweep finds that the repository does not ship under
// files/, each a directory and who makes it.
var knownDirectories = map[string]string{
	"/var/lib/agent-bus/%i": "a v1 team's state directory, made by agent-bus-install's ensure_dir",
}

// goRenderers are the Go files that write TemporaryFileSystem= into a unit, each judged on its
// rendered text by a test that calls Check (teamchat/unitfiles' TestNoUnitBindsAFileInsideATmpfs).
var goRenderers = []string{"internal/teamchat/host/sandbox.go"}

// kindInRepo is what a bind's source is: the file or directory files/ ships at that path, or a
// known directory.
func kindInRepo(src string) SourceKind {
	if _, ok := knownDirectories[src]; ok {
		return Directory
	}
	info, err := os.Stat(filepath.Join(repo, "files", filepath.FromSlash(src)))
	switch {
	case err != nil:
		return Unknown
	case info.IsDir():
		return Directory
	default:
		return File
	}
}

// openRoot is dir as an os.Root, closed when the test ends: the walks read through it.
func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return root
}

// The sweep: every unit text the repository ships outside src-go (files/, playbooks/, tasks/ and
// the rest), wherever it sets a TemporaryFileSystem=.
func TestNoShippedUnitBindsAFileInsideATmpfs(t *testing.T) {
	root := openRoot(t, repo)
	judged := 0
	err := filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if slices.Contains(notUnits, rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || strings.HasSuffix(rel, ".md") {
			return nil
		}
		data, err := root.ReadFile(rel)
		if err != nil {
			return err
		}
		if !strings.Contains(string(data), tmpfsKey) {
			return nil
		}
		judged++
		for _, f := range Check(string(data), kindInRepo) {
			t.Errorf("%s: %s", rel, f)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if judged == 0 {
		t.Fatal("the sweep judged no file: it proved nothing")
	}
}

// Every Go file that writes TemporaryFileSystem= is one whose rendered units a test judges.
func TestEveryGoRendererIsJudged(t *testing.T) {
	dir := filepath.Join(repo, "src-go")
	root := openRoot(t, dir)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "vendor" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || rel == "internal/unitsandbox/unitsandbox.go" {
			return nil
		}
		data, err := root.ReadFile(rel)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), tmpfsKey) && !slices.Contains(goRenderers, filepath.ToSlash(rel)) {
			t.Errorf("%s writes %s but no test judges what it renders: add one that calls Check, and list it in goRenderers", rel, tmpfsKey)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
