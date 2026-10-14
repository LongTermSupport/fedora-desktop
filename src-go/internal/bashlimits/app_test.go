package bashlimits

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const allowlistRel = "scripts/bash-limits.allowlist"

// script is a bash file of n code lines.
func script(n int) string {
	return "#!/usr/bin/bash\n# comment\n" + strings.Repeat("echo x\n", n)
}

// gitIn runs git in dir and fails the test on any error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)
	if _, err := (execx.OS{}).Run(context.Background(), execx.Cmd{Name: "git", Args: full, Dir: dir}); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

func inRoot(t *testing.T, root string) *os.Root {
	t.Helper()
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func put(t *testing.T, root, rel, content string) {
	t.Helper()
	r := inRoot(t, root)
	if err := r.MkdirAll(filepath.Dir(rel), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := r.WriteFile(rel, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readAllowlist(t *testing.T, root string) string {
	t.Helper()
	b, err := inRoot(t, root).ReadFile(allowlistRel)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// repo is a committed git repository: one small script, one long allowlisted script, an exempt
// bootstrap and test suite, and the allowlist.
func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitIn(t, root, "init", "-q")
	put(t, root, "scripts/small.bash", script(5))
	put(t, root, "files/bin/long", script(80))
	put(t, root, "run.bash", script(500))
	put(t, root, "scripts/test-x.bash", script(500))
	put(t, root, "README.md", "# docs\n")
	put(t, root, allowlistRel, Allowlist{"files/bin/long": {Code: 80}}.Format())
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "seed")
	return root
}

type result struct {
	code     int
	out, err string
}

func run(t *testing.T, root string, args ...string) result {
	t.Helper()
	var out, errBuf bytes.Buffer
	app := &App{Runner: execx.OS{}, Out: &out, Err: &errBuf}
	code := app.Run(context.Background(), root, args)
	return result{code, out.String(), errBuf.String()}
}

func wantFail(t *testing.T, name string, r result, needles ...string) {
	t.Helper()
	if r.code != term.ExitFailure {
		t.Errorf("%s: exit %d, want failure; stderr:\n%s", name, r.code, r.err)
	}
	for _, n := range needles {
		if !strings.Contains(r.err, n) {
			t.Errorf("%s: stderr lacks %q:\n%s", name, n, r.err)
		}
	}
}

func TestCleanTreePasses(t *testing.T) {
	root := repo(t)
	r := run(t, root, "--base", "HEAD")
	if r.code != term.ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
	// 2 checked (small, long), 2 exempt, 1 allowlisted.
	for _, want := range []string{"BASH-LIMITS-OK", "2 bash file(s) checked", "2 exempt", "1 allowlisted"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("summary lacks %q: %s", want, r.out)
		}
	}
}

func TestPlantedViolationsFail(t *testing.T) {
	cases := []struct {
		name, rel, content, needle string
	}{
		{"61 code lines", "scripts/new.bash", script(61), "61 code lines"},
		{"five functions", "scripts/new.bash", "a() { :; }\nb() { :; }\nc() { :; }\nd() { :; }\ne() { :; }\n", "5 functions"},
		{"associative array", "scripts/new.bash", "declare -A m\nm[a]=1\n", "associative array"},
		{"fzf", "scripts/new.bash", "x=$(ls | fzf)\n", "interactive UI"},
		{"extensionless", "files/bin/newtool", "#!/usr/bin/env bash\n" + strings.Repeat("echo x\n", 61), "61 code lines"},
	}
	for _, c := range cases {
		root := repo(t)
		put(t, root, c.rel, c.content)
		// Untracked: the gate holds a script to the limits before it is added.
		wantFail(t, c.name+" (untracked)", run(t, root, "--base", "HEAD"), c.rel, c.needle, "src-go/CLAUDE.md")
		gitIn(t, root, "add", "-A")
		wantFail(t, c.name+" (staged)", run(t, root, "--base", "HEAD"), c.rel, c.needle)
	}
}

func TestExactlyAtTheLimitsPasses(t *testing.T) {
	root := repo(t)
	put(t, root, "scripts/edge.bash", strings.Repeat("e() { :; }\n", 4)+strings.Repeat("echo x\n", 56))
	if r := run(t, root); r.code != term.ExitOK {
		t.Fatalf("60 code lines and 4 functions failed: %s", r.err)
	}
}

func TestListedFileCannotGrow(t *testing.T) {
	root := repo(t)
	put(t, root, "files/bin/long", script(81))
	wantFail(t, "grown", run(t, root), "files/bin/long", "grew past", "code lines 81 > 80")
}

func TestListedFileThatShrankMustBeTightened(t *testing.T) {
	root := repo(t)
	put(t, root, "files/bin/long", script(70))
	wantFail(t, "shrunk", run(t, root), "files/bin/long", "tighten the record")

	r := run(t, root, "--update")
	if r.code != term.ExitOK {
		t.Fatalf("--update: exit %d: %s", r.code, r.err)
	}
	if got := readAllowlist(t, root); !strings.Contains(got, "files/bin/long\t70\t0\t-") {
		t.Fatalf("record not lowered:\n%s", got)
	}
	if r := run(t, root, "--base", "HEAD"); r.code != term.ExitOK {
		t.Fatalf("after --update: %s", r.err)
	}
}

func TestUpdateRemovesEntriesWithinLimitsAndGone(t *testing.T) {
	root := repo(t)
	put(t, root, "files/bin/long", script(10))
	put(t, root, allowlistRel, Allowlist{"files/bin/long": {Code: 80}, "files/bin/deleted": {Code: 99}}.Format())
	wantFail(t, "before", run(t, root), "now within every limit", "files/bin/deleted", "allowlisted")
	if r := run(t, root, "--update"); r.code != term.ExitOK {
		t.Fatalf("--update: %s", r.err)
	}
	got := readAllowlist(t, root)
	if strings.Contains(got, "files/bin/long\t") || strings.Contains(got, "deleted") {
		t.Fatalf("entries not removed:\n%s", got)
	}
}

func TestUpdateNeverRaisesOrAdds(t *testing.T) {
	root := repo(t)
	put(t, root, "files/bin/long", script(95))
	put(t, root, "scripts/new.bash", script(70))
	r := run(t, root, "--update")
	wantFail(t, "update with growth and a new offender", r, "files/bin/long", "scripts/new.bash")
	got := readAllowlist(t, root)
	if !strings.Contains(got, "files/bin/long\t80\t0\t-") || strings.Contains(got, "scripts/new.bash") {
		t.Fatalf("--update raised or added a record:\n%s", got)
	}
}

func TestStaleEntryFails(t *testing.T) {
	root := repo(t)
	put(t, root, allowlistRel, Allowlist{"files/bin/long": {Code: 80}, "files/bin/missing": {Code: 99}}.Format())
	wantFail(t, "stale", run(t, root), "files/bin/missing", "not a checked bash file")
}

func TestExemptedFileOnTheListIsStale(t *testing.T) {
	root := repo(t)
	put(t, root, allowlistRel, Allowlist{"files/bin/long": {Code: 80}, "run.bash": {Code: 500}}.Format())
	wantFail(t, "exempt entry", run(t, root), "run.bash", "exempt")
}

func TestBaseRejectsAddedAndRaisedEntries(t *testing.T) {
	root := repo(t)
	put(t, root, "scripts/new.bash", script(70))
	put(t, root, allowlistRel, Allowlist{"files/bin/long": {Code: 80}, "scripts/new.bash": {Code: 70}}.Format())
	// Without --base the sneaked entry makes the tree pass; with it, the gate objects.
	if r := run(t, root); r.code != term.ExitOK {
		t.Fatalf("control: %s", r.err)
	}
	wantFail(t, "added entry", run(t, root, "--base", "HEAD"), "scripts/new.bash", "new allowlist entry")

	put(t, root, "scripts/new.bash", "")
	put(t, root, "files/bin/long", script(90))
	put(t, root, allowlistRel, Allowlist{"files/bin/long": {Code: 90}}.Format())
	wantFail(t, "raised record", run(t, root, "--base", "HEAD"), "files/bin/long", "raised")
}

func TestBaseThatDoesNotResolveFails(t *testing.T) {
	root := repo(t)
	r := run(t, root, "--base", "no-such-ref")
	if r.code != ExitCannotRun || !strings.Contains(r.err, "no-such-ref") {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
}

func TestBaseWithoutAnAllowlistIsNoted(t *testing.T) {
	root := t.TempDir()
	gitIn(t, root, "init", "-q")
	put(t, root, "README.md", "x\n")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "first")
	put(t, root, allowlistRel, Allowlist{}.Format())
	r := run(t, root, "--base", "HEAD")
	if r.code != term.ExitOK || !strings.Contains(r.err, "has no allowlist") {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
}

func TestSeed(t *testing.T) {
	root := t.TempDir()
	gitIn(t, root, "init", "-q")
	put(t, root, "scripts/small.bash", script(5))
	put(t, root, "files/bin/long", script(80))
	put(t, root, "scripts/ui.bash", "fzf\n")
	r := run(t, root, "--seed")
	if r.code != term.ExitOK {
		t.Fatalf("seed: exit %d: %s", r.code, r.err)
	}
	got := readAllowlist(t, root)
	if !strings.Contains(got, "files/bin/long\t80\t0\t-") || !strings.Contains(got, "scripts/ui.bash\t1\t0\tui") || strings.Contains(got, "small.bash") {
		t.Fatalf("seed content:\n%s", got)
	}
	if again := run(t, root); again.code != term.ExitOK {
		t.Fatalf("seeded tree does not pass: %s", again.err)
	}
	before := readAllowlist(t, root)
	r = run(t, root, "--seed")
	wantFail(t, "second seed", r, "refusing", allowlistRel)
	if readAllowlist(t, root) != before {
		t.Fatal("a refused seed changed the file")
	}
}

func TestMissingAllowlistCannotRun(t *testing.T) {
	root := t.TempDir()
	gitIn(t, root, "init", "-q")
	r := run(t, root)
	if r.code != ExitCannotRun || !strings.Contains(r.err, "--seed") {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
}

func TestMalformedAllowlistCannotRun(t *testing.T) {
	root := repo(t)
	put(t, root, allowlistRel, "garbage\n")
	if r := run(t, root); r.code != ExitCannotRun {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
}

func TestDeletedTrackedFileIsSkipped(t *testing.T) {
	root := repo(t)
	if err := os.Remove(filepath.Join(root, "scripts/small.bash")); err != nil {
		t.Fatal(err)
	}
	if r := run(t, root); r.code != term.ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
}

func TestRootDefaultsToTheGitTopLevel(t *testing.T) {
	root := repo(t)
	put(t, root, "files/bin/long", script(81))
	sub := filepath.Join(root, "files", "bin")
	wantFail(t, "from a subdirectory", run(t, sub), "grew past")
}

func TestExplicitRootAndAllowlist(t *testing.T) {
	root := repo(t)
	put(t, root, "other.allowlist", Allowlist{"files/bin/long": {Code: 80}}.Format())
	r := run(t, t.TempDir(), "--root", root, "--allowlist", "other.allowlist")
	if r.code != term.ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
}

func TestNotAGitRepositoryCannotRun(t *testing.T) {
	if r := run(t, t.TempDir()); r.code != ExitCannotRun {
		t.Fatalf("exit %d: %s", r.code, r.err)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("closed pipe") }

func TestUnwritableOutputCannotRun(t *testing.T) {
	root := repo(t)
	app := &App{Runner: execx.OS{}, Out: brokenWriter{}, Err: &bytes.Buffer{}}
	if code := app.Run(context.Background(), root, nil); code != ExitCannotRun {
		t.Fatalf("exit %d, want %d: a verdict nobody could read is not a pass", code, ExitCannotRun)
	}
}

func TestUsageErrors(t *testing.T) {
	root := repo(t)
	for _, args := range [][]string{
		{"--nope"},
		{"--update", "--seed"},
		{"stray"},
		{"--allowlist", "/abs/path"},
		{"--allowlist", "../up"},
	} {
		if r := run(t, root, args...); r.code != term.ExitUsage {
			t.Errorf("%v: exit %d: %s", args, r.code, r.err)
		}
	}
	if r := run(t, root, "--help"); r.code != term.ExitOK {
		t.Errorf("--help: exit %d", r.code)
	}
}
