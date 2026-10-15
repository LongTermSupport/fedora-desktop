package watchlist_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/watchlist"
)

const (
	urlAlpha = "https://github.com/example-org/alpha"
	urlBeta  = "https://github.com/example-org/beta"
	shaC     = "cccccccccccccccccccccccccccccccccccccccc"
)

type world struct {
	root string
	git  *execx.Fake
	out  bytes.Buffer
	err  bytes.Buffer
}

// newWorld is a checkout holding the given list rows.
func newWorld(t *testing.T, rows ...string) *world {
	t.Helper()
	w := &world{root: t.TempDir(), git: &execx.Fake{}}
	if err := os.MkdirAll(filepath.Join(w.root, "CLAUDE"), 0o750); err != nil {
		t.Fatal(err)
	}
	w.writeList(t, "# header\n"+strings.Join(rows, ""))
	return w
}

func (w *world) listPath() string { return watchlist.ListPath(w.root) }

func (w *world) writeList(t *testing.T, s string) {
	t.Helper()
	if err := os.WriteFile(w.listPath(), []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (w *world) list(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(w.listPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// mirror makes the clone directory of repo exist, as a clone would.
func (w *world) mirror(t *testing.T, repo string) string {
	t.Helper()
	dir := filepath.Join(w.root, "untracked", "watchlist", repo)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	return dir
}

func (w *world) run(args ...string) term.ExitCode {
	w.out.Reset()
	w.err.Reset()
	app := watchlist.NewApp(w.root, w.git, &w.out, &w.err)
	return app.Run(context.Background(), args)
}

func (w *world) ranGit(prefix ...string) bool {
	for _, c := range w.git.Calls() {
		if slices.Equal(c.Args[:min(len(prefix), len(c.Args))], prefix) {
			return true
		}
	}
	return false
}

func ok(out string) execx.Result { return execx.Result{Stdout: []byte(out)} }

func refuse(msg string) execx.Result { return execx.Result{Stderr: []byte(msg), ExitCode: 128} }

// head scripts the fetch and rev-parse of a mirror of branch main.
func (w *world) head(sha string) {
	w.git.On([]string{"git", "fetch", "origin"}, ok(""))
	w.git.On([]string{"git", "rev-parse", "--verify", "refs/remotes/origin/main"}, ok(sha+"\n"))
}

// W1
func TestHelpIsStdoutAndExitZero(t *testing.T) {
	w := newWorld(t)
	if code := w.run("--help"); code != term.ExitOK || !strings.Contains(w.out.String(), "watchlist") {
		t.Fatalf("exit %d, out %q", code, w.out.String())
	}
}

// W2
func TestUsageMistakesAreExit64(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"frobnicate"},
		{"--nope", "sync"},
		{"--root"},
		{"add"},
		{"mark", "example-org/alpha", shaA},
		{"mark", "example-org/alpha", "--run", "r"},
		{"sync", "a", "b"},
		{"add", urlAlpha, "extra"},
	} {
		w := newWorld(t)
		if code := w.run(args...); code != term.ExitUsage {
			t.Errorf("%v: exit %d", args, code)
		}
		if w.out.Len() != 0 || w.err.Len() == 0 {
			t.Errorf("%v: usage must be on stderr only (out %q)", args, w.out.String())
		}
	}
}

func TestRootFlagNamesTheCheckout(t *testing.T) {
	w := newWorld(t, row(urlAlpha, "main", "-", "-"))
	w.mirror(t, "example-org__alpha")
	w.head(shaA)
	app := watchlist.NewApp(t.TempDir(), w.git, &w.out, &w.err)
	if code := app.Run(context.Background(), []string{"--root", w.root, "sync"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
}

// W3, W4
func TestAMissingListIsAnError(t *testing.T) {
	w := newWorld(t)
	if err := os.Remove(w.listPath()); err != nil {
		t.Fatal(err)
	}
	if code := w.run("sync"); code != term.ExitFailure || !strings.HasPrefix(w.err.String(), "ERROR:") {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
}

// W10, W11, W12
func TestAddAppendsARowWithTheDefaultBranch(t *testing.T) {
	w := newWorld(t, row(urlAlpha, "main", "-", "-"))
	w.git.On([]string{"git", "ls-remote", "--symref", urlBeta, "HEAD"}, ok("ref: refs/heads/trunk\tHEAD\n"+shaA+"\tHEAD\n"))
	if code := w.run("add", urlBeta); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if want := "added example-org/beta (trunk): initial full review pending\n"; w.out.String() != want {
		t.Fatalf("out %q", w.out.String())
	}
	entries, err := watchlist.Parse([]byte(w.list(t)), "list")
	if err != nil || len(entries) != 2 || entries[1].URL != urlBeta || entries[1].Branch != "trunk" || entries[1].LastSHA != "" {
		t.Fatalf("list now %v, %v", entries, err)
	}
	if w.ranGit("clone") {
		t.Fatal("add must not clone")
	}
}

// W10
func TestAddRefusesBadURLsAndDuplicatesWithoutRunningGit(t *testing.T) {
	for _, u := range []string{
		"http://github.com/example-org/beta", "git@github.com:example-org/beta.git",
		"/srv/local/repo", "https://gitlab.com/example-org/beta", urlAlpha, "https://github.com/Example-Org/Alpha",
	} {
		w := newWorld(t, row(urlAlpha, "main", "-", "-"))
		before := w.list(t)
		if code := w.run("add", u); code != term.ExitFailure || !strings.HasPrefix(w.err.String(), "ERROR:") {
			t.Errorf("%s: exit %d, err %q", u, code, w.err.String())
		}
		if len(w.git.Calls()) != 0 {
			t.Errorf("%s: git ran before the refusal", u)
		}
		if w.list(t) != before {
			t.Errorf("%s: the list changed", u)
		}
	}
}

// W11
func TestAddFailsLoudlyWhenTheRemoteAnswersBadly(t *testing.T) {
	for name, res := range map[string]execx.Result{
		"git refuses":      refuse("fatal: repository not found"),
		"no default":       ok(shaA + "\tHEAD\n"),
		"nothing at all":   ok(""),
		"bare branch line": ok("ref: refs/heads/\tHEAD\n"),
	} {
		w := newWorld(t)
		before := w.list(t)
		w.git.On([]string{"git", "ls-remote"}, res)
		if code := w.run("add", urlBeta); code != term.ExitFailure || !strings.HasPrefix(w.err.String(), "ERROR:") {
			t.Errorf("%s: exit %d, err %q", name, code, w.err.String())
		}
		if w.list(t) != before {
			t.Errorf("%s: the list changed", name)
		}
	}
}

// W13
func TestSyncNamingAnUnknownRepoOrAnEmptyListIsAnError(t *testing.T) {
	w := newWorld(t, row(urlAlpha, "main", "-", "-"))
	if code := w.run("sync", "example-org/nope"); code != term.ExitFailure || !strings.Contains(w.err.String(), "example-org/nope") {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
	empty := newWorld(t)
	if code := empty.run("sync"); code != term.ExitFailure || !strings.Contains(empty.err.String(), "the watch list is empty") {
		t.Fatalf("exit %d, err %q", code, empty.err.String())
	}
}

// W14, W15, W16, W20
func TestSyncClonesAMissingMirrorAndReportsAnInitialReview(t *testing.T) {
	w := newWorld(t, row(urlAlpha, "main", "-", "-"))
	w.git.On([]string{"git", "clone"}, ok(""))
	w.git.On([]string{"git", "rev-parse", "--verify", "refs/remotes/origin/main"}, ok(shaA+"\n"))
	if code := w.run("sync"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	dir := filepath.Join(w.root, "untracked", "watchlist", "example-org__alpha")
	want := "== example-org/alpha (main)\ninitial: full review\nhead: " + shaA + "\nmirror: " + dir + "\n"
	if w.out.String() != want {
		t.Fatalf("out %q\nwant %q", w.out.String(), want)
	}
	if w.ranGit("log") || w.ranGit("diff") {
		t.Fatal("an initial review lists no range")
	}
	cloneArgs := []string{"clone", "--branch", "main", "--single-branch", urlAlpha, dir}
	if !w.ranGit(cloneArgs...) {
		t.Fatalf("calls %v", w.git.Calls())
	}
	if w.ranGit("fetch") {
		t.Fatal("a fresh clone needs no fetch")
	}
}

// W14, W17
func TestSyncFetchesAnExistingMirrorAndReportsUnchanged(t *testing.T) {
	w := newWorld(t, row(urlAlpha, "main", shaA, "run-1"))
	dir := w.mirror(t, "example-org__alpha")
	w.head(shaA)
	if code := w.run("sync"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	want := "== example-org/alpha (main)\nlast-checked: " + shaA + " (run run-1)\nhead: " + shaA + "\nmirror: " + dir + "\nunchanged\n"
	if w.out.String() != want {
		t.Fatalf("out %q", w.out.String())
	}
	fetch := w.git.Calls()[0]
	if fetch.Dir != dir || !slices.Equal(fetch.Args, []string{"fetch", "origin", "+refs/heads/main:refs/remotes/origin/main"}) {
		t.Fatalf("fetch %v in %q", fetch.Args, fetch.Dir)
	}
	if w.ranGit("clone") {
		t.Fatal("an existing mirror is not cloned again")
	}
}

// W18
func TestSyncListsTheCommitsAndPathsSinceTheLastReview(t *testing.T) {
	w := newWorld(t, row(urlAlpha, "main", shaA, "run-1"))
	dir := w.mirror(t, "example-org__alpha")
	w.head(shaB)
	w.git.On([]string{"git", "cat-file", "-e"}, ok(""))
	w.git.On([]string{"git", "merge-base", "--is-ancestor", shaA, shaB}, ok(""))
	w.git.On([]string{"git", "log", "--format=%h %s", shaA + ".." + shaB}, ok("bbb2222 second\naaa1111 first\n"))
	w.git.On([]string{"git", "diff", "--name-only", shaA, shaB}, ok("src/x.go\nREADME.md\n"))
	if code := w.run("sync", "example-org/alpha"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	want := "== example-org/alpha (main)\nlast-checked: " + shaA + " (run run-1)\nhead: " + shaB + "\nmirror: " + dir + "\n" +
		"commits (2):\n  bbb2222 second\n  aaa1111 first\nchanged paths (2):\n  src/x.go\n  README.md\n"
	if w.out.String() != want {
		t.Fatalf("out %q\nwant %q", w.out.String(), want)
	}
}

// W13: a repository is named by owner/repo or by URL, in any case.
func TestSyncAcceptsTheNameInEitherFormAndSelectsOnlyThatRepo(t *testing.T) {
	for _, name := range []string{"Example-Org/Beta", urlBeta, "https://github.com/EXAMPLE-ORG/BETA"} {
		w := newWorld(t, row(urlAlpha, "main", "-", "-"), row(urlBeta, "main", "-", "-"))
		w.mirror(t, "example-org__beta")
		w.head(shaA)
		if code := w.run("sync", name); code != term.ExitOK {
			t.Fatalf("%s: exit %d: %s", name, code, w.err.String())
		}
		if !strings.Contains(w.out.String(), "== example-org/beta") || strings.Contains(w.out.String(), "alpha") {
			t.Fatalf("%s: out %q", name, w.out.String())
		}
	}
}

// W19: the mirror still holds the reviewed commit (git keeps rewritten-away commits until a gc)
// but the head does not descend from it: the remote rewrote its history.
func TestSyncAfterAForcePushIsAnErrorEvenWhileTheMirrorHoldsTheOldCommit(t *testing.T) {
	w := newWorld(t, row(urlAlpha, "main", shaA, "run-1"))
	w.mirror(t, "example-org__alpha")
	w.head(shaB)
	w.git.On([]string{"git", "cat-file", "-e", shaA + "^{commit}"}, ok(""))
	w.git.On([]string{"git", "merge-base", "--is-ancestor", shaA, shaB}, execx.Result{ExitCode: 1})
	w.git.On([]string{"git", "log"}, ok("bbb2222 replacement\n"))
	w.git.On([]string{"git", "diff"}, ok("x\n"))
	if code := w.run("sync"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.err.String(), "rewrote") || !strings.Contains(w.err.String(), shaA) ||
		!strings.Contains(w.err.String(), "example-org/alpha") {
		t.Fatalf("err %q", w.err.String())
	}
	if strings.Contains(w.out.String(), "commits (") || w.ranGit("log") || w.ranGit("diff") {
		t.Fatalf("a range was listed across a rewrite: %q", w.out.String())
	}
}

// W19
func TestSyncWithALostLastShaIsAnErrorNotAFullReview(t *testing.T) {
	w := newWorld(t, row(urlAlpha, "main", shaA, "run-1"))
	w.mirror(t, "example-org__alpha")
	w.head(shaB)
	w.git.On([]string{"git", "cat-file", "-e"}, refuse("fatal: Not a valid object name"))
	if code := w.run("sync"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.err.String(), "example-org/alpha") || !strings.Contains(w.err.String(), shaA) {
		t.Fatalf("err %q", w.err.String())
	}
	if strings.Contains(w.out.String(), "initial") {
		t.Fatalf("fell back to a full review: %q", w.out.String())
	}
}

// W21
func TestSyncAbortsOnTheFirstGitFailureAndNeverWritesTheList(t *testing.T) {
	stages := map[string]func(w *world){
		"fetch": func(w *world) {
			w.git.On([]string{"git", "fetch"}, refuse("fatal: unable to access"))
		},
		"rev-parse": func(w *world) {
			w.git.On([]string{"git", "fetch"}, ok(""))
			w.git.On([]string{"git", "rev-parse"}, refuse("fatal: bad ref"))
		},
		"log": func(w *world) {
			w.head(shaB)
			w.git.On([]string{"git", "cat-file", "-e"}, ok(""))
			w.git.On([]string{"git", "merge-base"}, ok(""))
			w.git.On([]string{"git", "log"}, refuse("fatal: bad revision"))
		},
		"merge-base": func(w *world) {
			w.head(shaB)
			w.git.On([]string{"git", "cat-file", "-e"}, ok(""))
			w.git.On([]string{"git", "merge-base"}, refuse("fatal: Not a valid commit name"))
		},
		"diff": func(w *world) {
			w.head(shaB)
			w.git.On([]string{"git", "cat-file", "-e"}, ok(""))
			w.git.On([]string{"git", "merge-base"}, ok(""))
			w.git.On([]string{"git", "log"}, ok(""))
			w.git.On([]string{"git", "diff"}, refuse("fatal"))
		},
	}
	for stage, script := range stages {
		w := newWorld(t, row(urlAlpha, "main", shaA, "run-1"), row(urlBeta, "main", shaA, "run-1"))
		w.mirror(t, "example-org__alpha")
		w.mirror(t, "example-org__beta")
		before := w.list(t)
		script(w)
		if code := w.run("sync"); code != term.ExitFailure {
			t.Errorf("%s: exit %d", stage, code)
		}
		if !strings.HasPrefix(w.err.String(), "ERROR:") || !strings.Contains(w.err.String(), "example-org/alpha") {
			t.Errorf("%s: err %q", stage, w.err.String())
		}
		for _, c := range w.git.Calls() {
			if c.Dir != "" && strings.Contains(c.Dir, "example-org__beta") {
				t.Errorf("%s: a later repository was attempted", stage)
			}
		}
		if w.list(t) != before {
			t.Errorf("%s: sync wrote the list", stage)
		}
	}
}

// W14: git's progress and chatter never reach the payload stream.
func TestSyncKeepsGitChatterOffStdout(t *testing.T) {
	w := newWorld(t, row(urlAlpha, "main", "-", "-"))
	w.git.On([]string{"git", "clone"}, execx.Result{
		Stdout: []byte("Cloning into 'x'...\nremote: Counting objects\n"),
		Stderr: []byte("progress: 100%\n"),
	})
	w.git.On([]string{"git", "rev-parse", "--verify", "refs/remotes/origin/main"}, ok(shaA+"\n"))
	if code := w.run("sync"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	for _, chatter := range []string{"Cloning", "Counting", "progress"} {
		if strings.Contains(w.out.String(), chatter) || strings.Contains(w.err.String(), chatter) {
			t.Fatalf("git chatter %q leaked: out %q err %q", chatter, w.out.String(), w.err.String())
		}
	}
}

// W14: a directory left by a killed clone is named, not left to git's "not a repository".
func TestSyncNamesAnInterruptedCloneDirectory(t *testing.T) {
	w := newWorld(t, row(urlAlpha, "main", "-", "-"))
	dir := filepath.Join(w.root, "untracked", "watchlist", "example-org__alpha")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	w.head(shaA)
	if code := w.run("sync"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.err.String(), "not a git clone") || !strings.Contains(w.err.String(), dir) || w.ranGit("fetch") {
		t.Fatalf("err %q, calls %v", w.err.String(), w.git.Calls())
	}
}

// W21: a fetch that fails because the branch moved says what to edit.
func TestSyncFetchFailureHintsAtARenamedBranch(t *testing.T) {
	w := newWorld(t, row(urlAlpha, "master", shaA, "run-1"))
	w.mirror(t, "example-org__alpha")
	w.git.On([]string{"git", "fetch"}, refuse("fatal: couldn't find remote ref refs/heads/master"))
	if code := w.run("sync"); code != term.ExitFailure || !strings.Contains(w.err.String(), "branch column") {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
}

func TestSyncFailsLoudlyOnAFailedClone(t *testing.T) {
	w := newWorld(t, row(urlAlpha, "main", "-", "-"))
	w.git.On([]string{"git", "clone"}, refuse("fatal: could not read"))
	if code := w.run("sync"); code != term.ExitFailure || !strings.Contains(w.err.String(), "example-org/alpha") {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
}

// W22, W24
func TestMarkRecordsTheReviewAtomically(t *testing.T) {
	w := newWorld(t, row(urlAlpha, "main", "-", "-"), row(urlBeta, "main", shaA, "run-1"))
	w.mirror(t, "example-org__alpha")
	w.git.On([]string{"git", "cat-file", "-e", shaC + "^{commit}"}, ok(""))
	w.git.On([]string{"git", "merge-base", "--is-ancestor", shaC, "refs/remotes/origin/main"}, ok(""))
	if code := w.run("mark", "example-org/alpha", shaC, "--run", "scan-7"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	if want := "marked example-org/alpha at " + shaC + " (run scan-7)\n"; w.out.String() != want {
		t.Fatalf("out %q", w.out.String())
	}
	entries, err := watchlist.Parse([]byte(w.list(t)), "list")
	if err != nil || len(entries) != 2 {
		t.Fatalf("list %v, %v", entries, err)
	}
	if entries[0].LastSHA != shaC || entries[0].Run != "scan-7" || entries[1].LastSHA != shaA {
		t.Fatalf("entries %+v", entries)
	}
	leftovers, globErr := filepath.Glob(w.listPath() + ".tmp.*")
	if globErr != nil || len(leftovers) != 0 {
		t.Fatalf("temporary files left: %v, %v", leftovers, globErr)
	}
}

// W22
func TestMarkRefusesBadInputsAndLeavesTheListAlone(t *testing.T) {
	cases := map[string][]string{
		"unknown repo": {"mark", "example-org/nope", shaC, "--run", "r"},
		"short sha":    {"mark", "example-org/alpha", "abc123", "--run", "r"},
		"upper sha":    {"mark", "example-org/alpha", strings.ToUpper(shaC), "--run", "r"},
		"run spaces":   {"mark", "example-org/alpha", shaC, "--run", "two words"},
		"dash run":     {"mark", "example-org/alpha", shaC, "--run", "-"},
	}
	for name, args := range cases {
		w := newWorld(t, row(urlAlpha, "main", "-", "-"))
		w.mirror(t, "example-org__alpha")
		w.git.On([]string{"git", "cat-file"}, ok(""))
		before := w.list(t)
		if code := w.run(args...); code != term.ExitFailure || !strings.HasPrefix(w.err.String(), "ERROR:") {
			t.Errorf("%s: exit %d, err %q", name, code, w.err.String())
		}
		if w.list(t) != before {
			t.Errorf("%s: the list changed", name)
		}
	}
}

// W23: the sha must be on the branch the mirror tracks.
func TestMarkRefusesAShaThatIsNotOnTheBranch(t *testing.T) {
	w := newWorld(t, row(urlAlpha, "main", "-", "-"))
	w.mirror(t, "example-org__alpha")
	before := w.list(t)
	w.git.On([]string{"git", "cat-file", "-e"}, ok(""))
	w.git.On([]string{"git", "merge-base", "--is-ancestor", shaC, "refs/remotes/origin/main"}, execx.Result{ExitCode: 1})
	if code := w.run("mark", "example-org/alpha", shaC, "--run", "r"); code != term.ExitFailure ||
		!strings.Contains(w.err.String(), "origin/main") {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
	if w.list(t) != before {
		t.Fatal("the list changed")
	}
}

// W23: a review never moves backwards.
func TestMarkRefusesAShaOlderThanTheLastReview(t *testing.T) {
	w := newWorld(t, row(urlAlpha, "main", shaB, "run-1"))
	w.mirror(t, "example-org__alpha")
	before := w.list(t)
	w.git.On([]string{"git", "cat-file", "-e"}, ok(""))
	w.git.On([]string{"git", "merge-base", "--is-ancestor", shaC, "refs/remotes/origin/main"}, ok(""))
	w.git.On([]string{"git", "merge-base", "--is-ancestor", shaB, shaC}, execx.Result{ExitCode: 1})
	if code := w.run("mark", "example-org/alpha", shaC, "--run", "r"); code != term.ExitFailure ||
		!strings.Contains(w.err.String(), "last-checked") {
		t.Fatalf("exit %d, err %q", code, w.err.String())
	}
	if w.list(t) != before {
		t.Fatal("the list changed")
	}
}

// W23
func TestMarkNeedsTheMirrorAndTheCommit(t *testing.T) {
	noMirror := newWorld(t, row(urlAlpha, "main", "-", "-"))
	before := noMirror.list(t)
	if code := noMirror.run("mark", "example-org/alpha", shaC, "--run", "r"); code != term.ExitFailure {
		t.Fatalf("no mirror: exit %d", code)
	}
	if noMirror.list(t) != before {
		t.Fatal("no mirror: the list changed")
	}
	noCommit := newWorld(t, row(urlAlpha, "main", "-", "-"))
	noCommit.mirror(t, "example-org__alpha")
	noCommit.git.On([]string{"git", "cat-file"}, refuse("fatal: Not a valid object name"))
	if code := noCommit.run("mark", "example-org/alpha", shaC, "--run", "r"); code != term.ExitFailure || !strings.Contains(noCommit.err.String(), shaC) {
		t.Fatalf("no commit: exit %d, err %q", code, noCommit.err.String())
	}
	if noCommit.list(t) != before {
		t.Fatal("no commit: the list changed")
	}
}
