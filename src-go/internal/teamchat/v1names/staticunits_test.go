package v1names_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// repoRoot is the checkout this package's tests run in, from src-go/internal/teamchat/v1names.
const repoRoot = "../../../.."

// unitTypes are systemd's unit file suffixes (systemd.unit(5)).
var unitTypes = []string{".service", ".socket", ".timer", ".path", ".mount", ".automount", ".swap", ".target", ".slice", ".scope", ".device"}

// staticUnit is whether a repository path is a teamchat unit file, or a file in a teamchat
// unit's drop-in directory, as itself or as a Jinja template of one.
func staticUnit(p string) bool {
	for part := range strings.SplitSeq(p, "/") {
		name := strings.TrimSuffix(strings.TrimSuffix(part, ".j2"), ".d")
		if strings.HasPrefix(name, paths.Product+"-") && slices.ContainsFunc(unitTypes, func(s string) bool { return strings.HasSuffix(name, s) }) {
			return true
		}
	}
	return false
}

func staticUnits(files []string) []string {
	var out []string
	for _, f := range files {
		if staticUnit(f) {
			out = append(out, f)
		}
	}
	return out
}

// trackedFiles is every file git tracks in the checkout at root. The checkout may belong to
// another uid than the test's (a container's, or a run as another user), which git refuses
// unless told the directory is safe; the test only reads the file list.
func trackedFiles(t *testing.T, root string) []string {
	t.Helper()
	abs, err := filepath.Abs(root)
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

// Defence Before Fix, "a unit text no gate judges" (Plan 00246 INSTALL-PATH.md):
// TestEveryUnitTextSourceIsJudged reads Go files only, so a unit written as a static file or a
// Jinja template under files/ or templates/ would reach a host judged by nothing. No tracked file
// may be a teamchat unit or drop-in: Go makes each one (package unitfiles, and team apply's
// drop-ins), and the play installs what `teamchat-admin units print` writes.
func TestNoTeamchatUnitIsAStaticFile(t *testing.T) {
	if found := staticUnits(trackedFiles(t, repoRoot)); len(found) > 0 {
		t.Fatalf("tracked files that are teamchat units, which no unit gate judges (make the text in Go, in package unitfiles, and judge it in madeUnits): %q", found)
	}
}

// The check can fail: a unit, a template unit, a timer, a Jinja template of one and a drop-in
// are found; a golden file, a doc, a v1 unit, a Go file and a script named for teamchat are not.
func TestTheStaticUnitCheckDiscriminates(t *testing.T) {
	files := []string{
		"files/etc/systemd/system/teamchat-server@.service",
		"files/etc/systemd/system/teamchat-health.timer",
		"playbooks/imports/templates/teamchat-tls@.service.j2",
		"files/etc/systemd/system/teamchat-server@.service.d/override.conf",
		"files/etc/systemd/system/teamchat-seats.socket",
		"src-go/internal/teamchat/unitfiles/testdata/teamchat-health.service.golden",
		"docs/teamchat-health.service.md",
		"files/etc/systemd/system/agent-bus-hs@.service",
		"src-go/internal/teamchat/unitfiles/unitfiles.go",
		"scripts/qa-teamchat-separate.bash",
	}
	want := files[:5]
	if got := staticUnits(files); !slices.Equal(got, want) {
		t.Errorf("found %q, want %q", got, want)
	}
}

// The check reads the checkout as git tracks it: red on a fixture checkout holding a static unit
// file beside an untracked one, which git does not list.
func TestTheStaticUnitCheckReadsTheCheckout(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if _, err := (execx.OS{}).Run(t.Context(), execx.Cmd{Name: "git", Args: append([]string{"-C", dir}, args...)}); err != nil {
			t.Fatalf("git %q: %v", args, err)
		}
	}
	run("init", "-q")
	tracked := "files/etc/systemd/system/teamchat-backup.timer"
	for _, name := range []string{tracked, "README", "untracked/teamchat-health.service"} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("[Timer]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run("add", tracked, "README")
	if got := staticUnits(trackedFiles(t, dir)); !slices.Equal(got, []string{tracked}) {
		t.Fatalf("found %q in the fixture checkout, want %q", got, tracked)
	}
}
