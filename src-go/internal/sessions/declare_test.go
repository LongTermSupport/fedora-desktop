package sessions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The launch argv the declare tests give: a token, a key, a compose answer, two one-shot
// arguments a launch drops, and claude's own words after `--`.
var declaredLaunch = []string{"--token", "work", "--ssh-key", "/keys/id_github", "--rebuild", "--compose", "start", "--prompt", "hi", "--", "--model", "opus"}

// declareHost is a host with no live session and restore on, run as an ordinary user.
func declareHost(h *harness) string {
	restoreHost(h)
	restoreOn(h, true)
	asUser(h)
	return realDir(h.t, h, "work/proj")
}

// asUser makes the run an ordinary user's, whoever runs the tests.
func asUser(h *harness) { h.app.Getuid = func() int { return 1000 } }

func declare(extra ...string) []string {
	return append(append([]string{"declare"}, extra...), declaredLaunch...)
}

// recordBytes is the ccy-proj record as it is on disk.
func recordBytes(t *testing.T, h *harness) string {
	t.Helper()
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Clean(filepath.Join(regdir, "ccy-proj")))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func sessionsRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, statErr := os.Stat(filepath.Join(dir, "files/var/local/claude-yolo/lib/session-registry.bash")); statErr == nil {
			return dir
		}
	}
	t.Fatalf("repo root not found above %s", wd)
	return ""
}

// launcherBash runs a snippet in dir with the launcher's real libraries sourced.
func launcherBash(t *testing.T, dir, stateDir, snippet string, args ...string) string {
	t.Helper()
	lib := filepath.Join(sessionsRepoRoot(t), "files/var/local/claude-yolo/lib")
	script := "set -euo pipefail\nsource \"$1/common-pure.bash\"\nsource \"$1/session-registry.bash\"\nshift\ncd \"$1\"\nshift\n" + snippet
	res, err := execx.OS{}.Run(context.Background(), execx.Cmd{
		Name: "bash", Args: append([]string{"-c", script, "bash", lib, dir}, args...),
		Env: []string{"CCY_STATE_DIR=" + stateDir, "PATH=/usr/bin:/bin"},
	})
	if err != nil {
		t.Fatalf("bash: %v\nstderr: %s", err, res.Stderr)
	}
	return string(res.Stdout)
}

// A declared record is byte for byte the record the launcher's own writer makes for the same
// session launched with the same argv (its name from get_project_name, its arguments through
// the launcher's replay filter, its compose answer as the launch records it), with the fresh mark.
// restore --check then calls it RESTORABLE.
func TestDeclareWritesTheLaunchersRecordAndCheckJudgesItRestorable(t *testing.T) {
	h := newHarness(t)
	dir := declareHost(h)
	exitCodeIs(t, h.run(declare("--fresh-if-no-conversation", dir, "--")...), 0, h)
	if h.stdout() != "ccy-proj DECLARED\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	theirs := launcherBash(t, dir, t.TempDir(), `name="ccy-$(get_project_name)"
mapfile -t launch < <(ccy_registry_launch_args "$@")
mapfile -t replay < <(ccy_registry_replay_args ccy "${launch[@]}")
ccy_registry_write --compose started --fresh yes "$name" "$PWD" /var/local/claude-yolo/claude-yolo ccy yes "${replay[@]}"
cat "$(ccy_registry_dir)/$name"`, declaredLaunch...)
	if got := recordBytes(t, h); got != theirs {
		t.Fatalf("declared record:\n%s\nlauncher's record:\n%s", got, theirs)
	}

	h.out.Reset()
	exitCodeIs(t, h.run("restore", "--check"), 0, h)
	if h.stdout() != "ccy-proj RESTORABLE\n" {
		t.Fatalf("check stdout %q", h.stdout())
	}
	h.errOut.Reset()
	exitCodeIs(t, h.run("restore", "--dry-run"), 0, h)
	contains(t, "dry run", h.stderr(), "would start ccy-proj fresh in "+dir+" (no conversation is recorded): /var/local/claude-yolo/claude-yolo --token work --ssh-key /keys/id_github --compose start --supervise -- --model opus")
}

func TestDeclaringTheSameRecordAgainChangesNothingEvenWhileItRuns(t *testing.T) {
	h := newHarness(t)
	dir := declareHost(h)
	args := declare("--fresh-if-no-conversation", dir, "--")
	exitCodeIs(t, h.run(args...), 0, h)
	before := recordBytes(t, h)
	h.sessions("ccy-proj 0 " + dir)
	h.out.Reset()
	exitCodeIs(t, h.run(args...), 0, h)
	if h.stdout() != "ccy-proj UNCHANGED\n" || recordBytes(t, h) != before {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestADifferentRecordUnderTheNameIsReplacedOnlyWhenAskedAndNeverWhileLive(t *testing.T) {
	h := newHarness(t)
	dir := declareHost(h)
	exitCodeIs(t, h.run(declare("--fresh-if-no-conversation", dir, "--")...), 0, h)
	before := recordBytes(t, h)
	other := []string{"declare", "--fresh-if-no-conversation", dir, "--", "--no-ssh"}

	h.out.Reset()
	exitCodeIs(t, h.run(other...), 1, h)
	contains(t, "refusal", h.stderr(), "ccy-proj already has a different record", "--replace")
	if recordBytes(t, h) != before || h.stdout() != "" {
		t.Fatal("a refused declare changed the record or printed a verdict")
	}

	h.sessions("ccy-proj 0 " + dir)
	h.errOut.Reset()
	exitCodeIs(t, h.run(append([]string{"declare", "--replace"}, other[1:]...)...), 1, h)
	contains(t, "live", h.stderr(), "ccy-proj is running")
	if recordBytes(t, h) != before {
		t.Fatal("a declare over a live session changed its record")
	}

	h.sessions()
	exitCodeIs(t, h.run(append([]string{"declare", "--replace"}, other[1:]...)...), 0, h)
	if h.stdout() != "ccy-proj REPLACED\n" || !strings.Contains(recordBytes(t, h), "arg=--no-ssh\n") {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestALiveSessionWithNoRecordIsNotDeclaredOver(t *testing.T) {
	h := newHarness(t)
	dir := declareHost(h)
	h.sessions("ccy-proj 1 " + dir)
	exitCodeIs(t, h.run(declare("--fresh-if-no-conversation", dir, "--")...), 1, h)
	contains(t, "live", h.stderr(), "ccy-proj is running")
}

func TestTheNameCanBeGivenAndIsAccountedForInTheCheck(t *testing.T) {
	h := newHarness(t)
	dir := declareHost(h)
	exitCodeIs(t, h.run("declare", "--name", "ccy-dev-box", "--fresh-if-no-conversation", dir, "--", "--no-ssh"), 0, h)
	if h.stdout() != "ccy-dev-box DECLARED\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	for _, bad := range []string{"dev-box", "ccy-", "ccy-a/b", "ccy-a.b", "cc-dev"} {
		h.out.Reset()
		exitCodeIs(t, h.run("declare", "--name", bad, "--fresh-if-no-conversation", dir), 64, h)
	}
}

// Declare refuses whatever the boot restore could not start, and writes nothing.
func TestDeclareRefusesARecordTheRestoreCouldNotStart(t *testing.T) {
	cases := []struct {
		label string
		args  []string
		says  string
	}{
		{"unmarked, no conversation", []string{}, "no-conversation"},
		{"no bus", []string{"--fresh-if-no-conversation", "@dir", "--", "--no-bus"}, "without the agent team bus"},
		{"opted out", []string{"--fresh-if-no-conversation", "@dir", "--", "--no-restore"}, "--no-restore"},
		{"opencode", []string{"--fresh-if-no-conversation", "@dir", "--", "--opencode"}, "opencode"},
		{"bad compose", []string{"--fresh-if-no-conversation", "@dir", "--", "--compose", "maybe"}, "--compose"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			h := newHarness(t)
			dir := declareHost(h)
			args := c.args
			if len(args) == 0 {
				args = []string{"@dir"}
			}
			full := []string{"declare"}
			for _, a := range args {
				full = append(full, strings.ReplaceAll(a, "@dir", dir))
			}
			exitCodeIs(t, h.run(full...), 1, h)
			contains(t, c.label, h.stderr(), c.says)
			regdir, err := registry.Dir(h.app.Getenv)
			if err != nil {
				t.Fatal(err)
			}
			if _, statErr := os.Stat(filepath.Join(regdir, "ccy-proj")); !os.IsNotExist(statErr) {
				t.Fatal("a refused declare left a record")
			}
		})
	}
}

func TestDeclareRefusesADirectoryTheRestoreCouldNotStartIn(t *testing.T) {
	h := newHarness(t)
	declareHost(h)
	file := h.write("plainfile", "x")
	for _, c := range []struct {
		dir  string
		code term.ExitCode
	}{
		{filepath.Join(h.root, "absent"), 1},
		{file, 1},
		{"relative/dir", 64},
		{h.root + "/work/../work/proj", 64},
		{h.root + "/work/proj/", 64},
	} {
		h.errOut.Reset()
		exitCodeIs(t, h.run("declare", "--fresh-if-no-conversation", c.dir), c.code, h)
	}
}

func TestDeclareUsageMistakes(t *testing.T) {
	h := newHarness(t)
	dir := declareHost(h)
	for _, args := range [][]string{
		{"declare"},
		{"declare", "--bogus", dir},
		{"declare", "--name"},
		{"declare", dir, "extra"},
	} {
		exitCodeIs(t, h.run(args...), 64, h)
	}
}

func TestDeclareRefusesRootAndSaysWhenRestoreIsOff(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	asUser(h)
	dir := realDir(t, h, "work/proj")
	exitCodeIs(t, h.run("declare", "--fresh-if-no-conversation", dir, "--", "--no-ssh"), 0, h)
	contains(t, "restore off", h.stderr(), "session restore is off for this user")

	h.app.Getuid = func() int { return 0 }
	exitCodeIs(t, h.run("declare", "--fresh-if-no-conversation", dir, "--", "--no-ssh"), 1, h)
	contains(t, "root", h.stderr(), "as the user whose session it is, not root")
}

// The name is the launcher's own (get_project_name), for folders under a generic parent and not.
func TestTheDefaultNameIsTheLaunchersProjectName(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"work/proj", "Clients/My.Site", "src/Some Thing", "a-b/c_d"} {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		want := strings.TrimSpace(launcherBash(t, dir, t.TempDir(), `get_project_name`))
		if got := projectName(dir); got != want {
			t.Errorf("%s: go %q, launcher %q", rel, got, want)
		}
	}
}
