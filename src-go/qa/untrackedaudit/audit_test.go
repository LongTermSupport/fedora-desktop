package untrackedaudit

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"maps"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

var epoch = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

const policy = `ageHours: 24
stores: [repos, worktrees, plan-runs, clones, facts]
daemonStores: [agent-reports, issue-reports]
assetExtensions: [.md, .patch, .diff]
scratchShapes: ["*-msg.txt", "msg-*", "body-*", "*-fields.json", "*.log", "*.out"]
activeBranches: []
`

// fakeGit answers by argv and directory; an argv it was not given is an error, so a check that
// asks something unplanned fails loudly.
type fakeGit struct {
	answers map[string]proc.Result
	asked   []string
}

func (f *fakeGit) Run(_ context.Context, c proc.Cmd) (proc.Result, error) {
	key := strings.Join(c.Argv, " ") + "@" + c.Dir
	f.asked = append(f.asked, key)
	res, ok := f.answers[key]
	if !ok {
		return proc.Result{}, errors.New("unplanned git call: " + key)
	}
	return res, nil
}

const (
	refsKey  = "git for-each-ref --format=%(refname:short)%09%(committerdate:unix) refs/heads@/repo"
	wtKey    = "git worktree list --porcelain@/repo"
	stashKey = "git stash list --format=%gd%x09%ct%x09%gs@/repo"
)

func ok(out string) proc.Result { return proc.Result{Stdout: out} }

// cleanGit is a repository with one branch fully pushed, the main work tree only and no stash.
func cleanGit() map[string]proc.Result {
	return map[string]proc.Result{
		refsKey: ok("main\t" + unix(epoch.Add(-48*time.Hour)) + "\n"),
		"git rev-list --count main --not --remotes@/repo": ok("0\n"),
		wtKey:    ok("worktree /repo\nHEAD abc\nbranch refs/heads/main\n\n"),
		stashKey: ok(""),
	}
}

func unix(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) }

func file(age time.Duration) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("x"), ModTime: epoch.Add(-age)}
}

func tree(files map[string]*fstest.MapFile) fstest.MapFS {
	fsys := fstest.MapFS{"scripts/untracked-policy.yml": {Data: []byte(policy), ModTime: epoch}}
	maps.Copy(fsys, files)
	return fsys
}

type outcome struct {
	code     ExitCode
	out, err string
}

func runWith(t *testing.T, fsys fs.FS, git map[string]proc.Result, mtimes map[string]time.Time) outcome {
	t.Helper()
	var out, errOut bytes.Buffer
	env := Env{
		Stdout:  &out,
		Stderr:  &errOut,
		Cwd:     "/repo",
		Resolve: func(p string) (string, error) { return p, nil },
		Open:    func(string) fs.FS { return fsys },
		Git:     &fakeGit{answers: git},
		Now:     func() time.Time { return epoch },
		Mtime: func(p string) (time.Time, error) {
			if m, found := mtimes[p]; found {
				return m, nil
			}
			return time.Time{}, fs.ErrNotExist
		},
	}
	code := Main(nil, env)
	return outcome{code: code, out: out.String(), err: errOut.String()}
}

// Green controls: nothing the audit flags.
func TestGreenControls(t *testing.T) {
	cases := map[string]map[string]*fstest.MapFile{
		"a fresh research note":             {"untracked/scratch/notes.md": file(2 * time.Hour)},
		"an old allowlisted message body":   {"untracked/scratch/p245-msg.txt": file(90 * time.Hour)},
		"an old capture":                    {"untracked/scratch/qa-run.log": file(90 * time.Hour)},
		"an old file in a registered store": {"untracked/repos/clone/README.md": file(900 * time.Hour)},
		"an old plan-run log":               {"untracked/plan-runs/x/y/run.md": file(900 * time.Hour)},
		"an old file in worktrees store":    {"untracked/worktrees/a/b.patch": file(900 * time.Hour)},
		"no untracked directory at all":     {},
		"a file just under the age floor":   {"untracked/x.md": file(23*time.Hour + 59*time.Minute)},
		"an old shaped fields file":         {"untracked/scratch/issue-fields.json": file(100 * time.Hour)},
		"an old out capture in a subfolder": {"untracked/scratch/a/b/c.out": file(100 * time.Hour)},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			got := runWith(t, tree(files), cleanGit(), nil)
			if got.code != ExitOK {
				t.Fatalf("exit %d, out %q err %q", got.code, got.out, got.err)
			}
		})
	}
}

// Red: the incidents of the week, recreated.
func TestRedFixtures(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]*fstest.MapFile
		kind  string
		path  string
		home  string
	}{
		{
			"a 36 hour old research note",
			map[string]*fstest.MapFile{"untracked/scratch/perm-research.md": file(36 * time.Hour)},
			KindAsset, "untracked/scratch/perm-research.md", "RESEARCH",
		},
		{
			"a patch standing in for a commit",
			map[string]*fstest.MapFile{"untracked/scratch/diag.patch": file(30 * time.Hour)},
			KindPatch, "untracked/scratch/diag.patch", "pushed branch",
		},
		{
			"a diff",
			map[string]*fstest.MapFile{"untracked/scratch/d.diff": file(30 * time.Hour)},
			KindPatch, "untracked/scratch/d.diff", "pushed branch",
		},
		{
			"an owner decision sheet",
			map[string]*fstest.MapFile{"untracked/owner-decisions-x.md": file(50 * time.Hour)},
			KindDecision, "untracked/owner-decisions-x.md", "GitHub issue",
		},
		{
			"an old message body saved as markdown",
			map[string]*fstest.MapFile{"untracked/scratch/msg-1.md": file(40 * time.Hour)},
			KindAsset, "untracked/scratch/msg-1.md", "RESEARCH",
		},
		{
			"an old unshaped file",
			map[string]*fstest.MapFile{"untracked/scratch/handoff.txt": file(40 * time.Hour)},
			KindUnshaped, "untracked/scratch/handoff.txt", "tracked home",
		},
		{
			"a rescue directory",
			map[string]*fstest.MapFile{"untracked/wip-rescue/a.go": file(40 * time.Hour)},
			KindUnshaped, "untracked/wip-rescue/a.go", "tracked home",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runWith(t, tree(tc.files), cleanGit(), nil)
			if got.code != ExitFindings {
				t.Fatalf("exit %d, out %q err %q", got.code, got.out, got.err)
			}
			if !strings.Contains(got.out, tc.kind) || !strings.Contains(got.out, tc.path) || !strings.Contains(got.out, tc.home) {
				t.Fatalf("report lacks %q/%q/%q:\n%s", tc.kind, tc.path, tc.home, got.out)
			}
			if !strings.Contains(got.out, "h old") {
				t.Fatalf("report names no age:\n%s", got.out)
			}
		})
	}
}

// The daemon's stores are a warning list, never a failure.
func TestDaemonStoresWarn(t *testing.T) {
	files := map[string]*fstest.MapFile{
		"untracked/agent-reports/auto/a.md": file(100 * time.Hour),
		"untracked/agent-reports/auto/b.md": file(100 * time.Hour),
		"untracked/issue-reports/c.md":      file(100 * time.Hour),
		"untracked/issue-reports/new.md":    file(1 * time.Hour),
	}
	got := runWith(t, tree(files), cleanGit(), nil)
	if got.code != ExitOK {
		t.Fatalf("exit %d: %s%s", got.code, got.out, got.err)
	}
	for _, want := range []string{"WARNING", "untracked/agent-reports: 2", "untracked/issue-reports: 1"} {
		if !strings.Contains(got.out, want) {
			t.Fatalf("missing %q:\n%s", want, got.out)
		}
	}
}

func TestUnpushedBranchIsAFinding(t *testing.T) {
	git := cleanGit()
	git[refsKey] = ok("main\t" + unix(epoch.Add(-48*time.Hour)) + "\nfeat/x\t" + unix(epoch.Add(-72*time.Hour)) + "\n")
	git["git rev-list --count feat/x --not --remotes@/repo"] = ok("3\n")
	got := runWith(t, tree(nil), git, nil)
	if got.code != ExitFindings || !strings.Contains(got.out, KindBranch) || !strings.Contains(got.out, "feat/x") ||
		!strings.Contains(got.out, "3 commit") || !strings.Contains(got.out, "git push") {
		t.Fatalf("exit %d:\n%s%s", got.code, got.out, got.err)
	}
	if strings.Contains(got.out, "main") {
		t.Fatalf("a pushed branch was reported:\n%s", got.out)
	}
}

func TestActiveBranchIsExempt(t *testing.T) {
	cfg := strings.Replace(policy, "activeBranches: []", "activeBranches: [feat/x]", 1)
	fsys := tree(nil)
	fsys["scripts/untracked-policy.yml"] = &fstest.MapFile{Data: []byte(cfg)}
	git := cleanGit()
	git[refsKey] = ok("feat/x\t" + unix(epoch.Add(-72*time.Hour)) + "\n")
	git["git rev-list --count feat/x --not --remotes@/repo"] = ok("3\n")
	if got := runWith(t, fsys, git, nil); got.code != ExitOK {
		t.Fatalf("exit %d:\n%s%s", got.code, got.out, got.err)
	}
}

const linkedWT = "worktree /repo\nHEAD a\nbranch refs/heads/main\n\nworktree /wt/one\nHEAD b\nbranch refs/heads/main\n\n"

func TestDirtyWorktreeOlderThanTheFloorIsAFinding(t *testing.T) {
	git := cleanGit()
	git[wtKey] = ok(linkedWT)
	git["git status --porcelain@/wt/one"] = ok(" M a.go\n?? notes.md\n")
	git["git log -1 --format=%ct@/wt/one"] = ok(unix(epoch.Add(-100*time.Hour)) + "\n")
	mt := map[string]time.Time{"/wt/one/a.go": epoch.Add(-60 * time.Hour), "/wt/one/notes.md": epoch.Add(-30 * time.Hour)}
	got := runWith(t, tree(nil), git, mt)
	if got.code != ExitFindings || !strings.Contains(got.out, KindDirty) || !strings.Contains(got.out, "/wt/one") ||
		!strings.Contains(got.out, "2 changed") {
		t.Fatalf("exit %d:\n%s%s", got.code, got.out, got.err)
	}
}

func TestFreshDirtyWorktreeIsQuiet(t *testing.T) {
	git := cleanGit()
	git[wtKey] = ok(linkedWT)
	git["git status --porcelain@/wt/one"] = ok(" M a.go\n")
	git["git log -1 --format=%ct@/wt/one"] = ok(unix(epoch.Add(-100*time.Hour)) + "\n")
	mt := map[string]time.Time{"/wt/one/a.go": epoch.Add(-2 * time.Hour)}
	if got := runWith(t, tree(nil), git, mt); got.code != ExitOK {
		t.Fatalf("exit %d:\n%s%s", got.code, got.out, got.err)
	}
}

func TestDetachedWorktreeWithUnpushedCommits(t *testing.T) {
	git := cleanGit()
	git[wtKey] = ok("worktree /repo\nHEAD a\nbranch refs/heads/main\n\nworktree /wt/two\nHEAD b\ndetached\n\n")
	git["git status --porcelain@/wt/two"] = ok("")
	git["git log -1 --format=%ct@/wt/two"] = ok(unix(epoch.Add(-100*time.Hour)) + "\n")
	git["git rev-list --count HEAD --not --remotes@/wt/two"] = ok("2\n")
	got := runWith(t, tree(nil), git, nil)
	if got.code != ExitFindings || !strings.Contains(got.out, KindWorktreeCommits) || !strings.Contains(got.out, "/wt/two") {
		t.Fatalf("exit %d:\n%s%s", got.code, got.out, got.err)
	}
}

func TestStashIsAFinding(t *testing.T) {
	git := cleanGit()
	git[stashKey] = ok("stash@{0}\t" + unix(epoch.Add(-5*time.Hour)) + "\tWIP on x\n")
	got := runWith(t, tree(nil), git, nil)
	if got.code != ExitFindings || !strings.Contains(got.out, KindStash) || !strings.Contains(got.out, "stash@{0}") {
		t.Fatalf("exit %d:\n%s%s", got.code, got.out, got.err)
	}
}

func TestReportIsGroupedByKind(t *testing.T) {
	git := cleanGit()
	git[stashKey] = ok("stash@{0}\t" + unix(epoch) + "\tWIP\n")
	files := map[string]*fstest.MapFile{
		"untracked/a.md":    file(40 * time.Hour),
		"untracked/b.patch": file(40 * time.Hour),
		"untracked/c.md":    file(40 * time.Hour),
		"untracked/d.patch": file(40 * time.Hour),
	}
	got := runWith(t, tree(files), git, nil)
	assetHeader := strings.Index(got.out, KindAsset)
	patchHeader := strings.Index(got.out, KindPatch)
	if assetHeader < 0 || patchHeader < 0 || strings.Count(got.out, KindAsset) != 1 || strings.Count(got.out, KindPatch) != 1 {
		t.Fatalf("kinds not grouped under one header each:\n%s", got.out)
	}
	if !strings.Contains(got.out, "summary: 5 finding(s)") {
		t.Fatalf("no summary:\n%s", got.out)
	}
}

// Exit 2: the audit cannot judge.
func TestCannotJudge(t *testing.T) {
	t.Run("no policy", func(t *testing.T) {
		if got := runWith(t, fstest.MapFS{}, cleanGit(), nil); got.code != ExitCannotRun {
			t.Fatalf("exit %d", got.code)
		}
	})
	t.Run("bad policy", func(t *testing.T) {
		fsys := fstest.MapFS{"scripts/untracked-policy.yml": {Data: []byte("ageHours: [")}}
		if got := runWith(t, fsys, cleanGit(), nil); got.code != ExitCannotRun {
			t.Fatalf("exit %d", got.code)
		}
	})
	t.Run("unknown policy key", func(t *testing.T) {
		fsys := tree(nil)
		fsys["scripts/untracked-policy.yml"] = &fstest.MapFile{Data: []byte(policy + "typo: 1\n")}
		if got := runWith(t, fsys, cleanGit(), nil); got.code != ExitCannotRun {
			t.Fatalf("exit %d", got.code)
		}
	})
	t.Run("zero age floor", func(t *testing.T) {
		fsys := tree(nil)
		fsys["scripts/untracked-policy.yml"] = &fstest.MapFile{Data: []byte(strings.Replace(policy, "ageHours: 24", "ageHours: 0", 1))}
		if got := runWith(t, fsys, cleanGit(), nil); got.code != ExitCannotRun {
			t.Fatalf("exit %d", got.code)
		}
	})
	t.Run("git fails", func(t *testing.T) {
		git := cleanGit()
		git[stashKey] = proc.Result{Code: 128, Stderr: "fatal: not a repository"}
		got := runWith(t, tree(nil), git, nil)
		if got.code != ExitCannotRun || !strings.Contains(got.err, "fatal") {
			t.Fatalf("exit %d: %s", got.code, got.err)
		}
	})
	t.Run("unplanned git call is an error", func(t *testing.T) {
		if got := runWith(t, tree(nil), map[string]proc.Result{}, nil); got.code != ExitCannotRun {
			t.Fatalf("exit %d", got.code)
		}
	})
	t.Run("unparseable count", func(t *testing.T) {
		git := cleanGit()
		git["git rev-list --count main --not --remotes@/repo"] = ok("many\n")
		if got := runWith(t, tree(nil), git, nil); got.code != ExitCannotRun {
			t.Fatalf("exit %d", got.code)
		}
	})
}

func TestUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	env := Env{Stdout: &out, Stderr: &errOut, Cwd: "/repo"}
	if code := Main([]string{"--help"}, env); code != ExitOK || !strings.Contains(out.String(), "usage:") {
		t.Fatalf("help: %d %q", code, out.String())
	}
	out.Reset()
	if code := Main([]string{"--nope"}, env); code != ExitCannotRun || !strings.Contains(errOut.String(), "unknown argument") {
		t.Fatalf("nope: %d %q", code, errOut.String())
	}
}

func TestPolicyFlag(t *testing.T) {
	fsys := fstest.MapFS{"custom.yml": {Data: []byte(policy)}}
	var out, errOut bytes.Buffer
	env := Env{
		Stdout: &out, Stderr: &errOut, Cwd: "/repo",
		Resolve: func(p string) (string, error) { return p, nil },
		Open:    func(string) fs.FS { return fsys },
		Git:     &fakeGit{answers: cleanGit()},
		Now:     func() time.Time { return epoch },
	}
	if code := Main([]string{"--policy", "p/custom.yml"}, env); code != ExitOK {
		t.Fatalf("exit %d: %s%s", code, out.String(), errOut.String())
	}
	for _, bad := range [][]string{{"--policy"}, {"--policy", ""}, {"--policy", "--root"}} {
		if code := Main(bad, env); code != ExitCannotRun {
			t.Fatalf("%v: exit %d", bad, code)
		}
	}
}

func TestUnreadableRootIsExit2(t *testing.T) {
	var out, errOut bytes.Buffer
	env := Env{
		Stdout: &out, Stderr: &errOut, Cwd: "/repo",
		Resolve: func(string) (string, error) { return "", errors.New("no such root") },
	}
	if code := Main(nil, env); code != ExitCannotRun {
		t.Fatalf("exit %d", code)
	}
}
