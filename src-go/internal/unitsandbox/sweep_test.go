package unitsandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// repo is the repository root, from this package's directory.
const repo = "../../.."

// tmpfsKey is the directive whose presence makes a file one this rule reads.
const tmpfsKey = "TemporaryFileSystem="

// notUnits are the top-level tracked trees that hold no unit text: .claude (agent and daemon
// configuration) and src-go, whose renderers are judged on what they render (goRenderers). Plan
// folders' probes and tests are swept: a probe starts transient units.
var notUnits = []string{".claude", "src-go"}

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

// trackedFiles is every file git tracks in the repository, slash-separated from its root: the
// sweeps judge what the repository publishes, never what a run left on disk (an ignored cache
// another uid wrote is unreadable here and ships nowhere). The checkout may belong to another uid
// than the test's, which git refuses unless told the directory is safe.
func trackedFiles(t *testing.T) []string {
	t.Helper()
	abs, err := filepath.Abs(repo)
	if err != nil {
		t.Fatal(err)
	}
	res, err := (execx.OS{}).Run(t.Context(), execx.Cmd{Name: "git", Args: []string{"-c", "safe.directory=" + abs, "-C", abs, "ls-files", "-z"}})
	if err != nil {
		t.Fatalf("git ls-files in %s: %v", abs, err)
	}
	out := strings.Split(strings.TrimSuffix(string(res.Stdout), "\x00"), "\x00")
	if len(out) == 0 || out[0] == "" {
		t.Fatalf("git tracks no file in %s", abs)
	}
	return out
}

// regularFile is whether rel is a regular file in root; a tracked symlink is not read through.
func regularFile(t *testing.T, root *os.Root, rel string) bool {
	t.Helper()
	info, err := root.Lstat(rel)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().IsRegular()
}

// The sweep: every unit text the repository ships outside src-go (files/, playbooks/, tasks/ and
// the rest), wherever it sets a TemporaryFileSystem=.
func TestNoShippedUnitBindsAFileInsideATmpfs(t *testing.T) {
	root := openRoot(t, repo)
	judged := 0
	for _, rel := range trackedFiles(t) {
		inTree := func(tree string) bool { return strings.HasPrefix(rel, tree+"/") }
		if slices.ContainsFunc(notUnits, inTree) || strings.HasSuffix(rel, ".md") || !regularFile(t, root, rel) {
			continue
		}
		data, err := root.ReadFile(rel)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), tmpfsKey) {
			continue
		}
		judged++
		for _, f := range Check(string(data), kindInRepo) {
			t.Errorf("%s: %s", rel, f)
		}
	}
	if judged == 0 {
		t.Fatal("the sweep judged no file: it proved nothing")
	}
}

// Every Go file that writes TemporaryFileSystem= is one whose rendered units a test judges.
func TestEveryGoRendererIsJudged(t *testing.T) {
	root := openRoot(t, repo)
	for _, path := range trackedFiles(t) {
		rel, ok := strings.CutPrefix(path, "src-go/")
		if !ok || !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || rel == "internal/unitsandbox/unitsandbox.go" ||
			strings.HasPrefix(rel, "vendor/") || strings.Contains(rel, "/testdata/") || !regularFile(t, root, path) {
			continue
		}
		data, err := root.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), tmpfsKey) && !slices.Contains(goRenderers, rel) {
			t.Errorf("%s writes %s but no test judges what it renders: add one that calls Check, and list it in goRenderers", rel, tmpfsKey)
		}
	}
}
