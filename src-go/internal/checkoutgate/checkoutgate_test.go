package checkoutgate_test

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/checkoutgate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const checkout = "/home/u/Projects/fedora-desktop"

// world is the checkout as git would describe it; each field is one answer.
type world struct {
	status      string // porcelain v1 -z
	detached    bool
	noUpstream  bool
	fetchFails  bool
	upstreamErr bool
	ahead       int
	behind      int
	brokenGit   string // the argv prefix (after git) that fails as a fault
}

func (w world) fake() *execx.Fake {
	f := &execx.Fake{}
	f.On([]string{"git", "status"}, execx.Result{Stdout: []byte(w.status)})
	if w.detached {
		f.On([]string{"git", "symbolic-ref"}, execx.Result{ExitCode: 1})
	} else {
		f.On([]string{"git", "symbolic-ref"}, execx.Result{Stdout: []byte("F44\n")})
	}
	f.On([]string{"git", "rev-parse", "--verify", "HEAD"}, execx.Result{Stdout: []byte("0123456789abcdef0123456789abcdef01234567\n")})
	if w.noUpstream {
		f.On([]string{"git", "config"}, execx.Result{ExitCode: 1})
	} else {
		f.On([]string{"git", "config"}, execx.Result{Stdout: []byte("refs/heads/F44\n")})
	}
	if w.fetchFails {
		f.On([]string{"git", "fetch"}, execx.Result{ExitCode: 128, Stderr: []byte("fatal: Could not resolve host: github.com")})
	} else {
		f.On([]string{"git", "fetch"}, execx.Result{})
	}
	if w.upstreamErr {
		f.On([]string{"git", "rev-parse", "--abbrev-ref"},
			execx.Result{ExitCode: 128, Stderr: []byte("fatal: no such branch: 'gone'")})
	} else {
		f.On([]string{"git", "rev-parse", "--abbrev-ref"}, execx.Result{Stdout: []byte("origin/F44\n")})
	}
	f.On([]string{"git", "rev-list"}, execx.Result{Stdout: fmtCounts(w.ahead, w.behind)})
	if w.brokenGit != "" {
		f.On(append([]string{"git"}, strings.Fields(w.brokenGit)...), execx.Result{ExitCode: 128, Stderr: []byte("fatal: broken")})
	}
	return f
}

func fmtCounts(a, b int) []byte {
	return []byte(strconv.Itoa(a) + "\t" + strconv.Itoa(b) + "\n")
}

type result struct {
	code   term.ExitCode
	stderr string
	git    *execx.Fake
}

func run(t *testing.T, w world, args ...string) result {
	t.Helper()
	f := w.fake()
	var errOut bytes.Buffer
	code := checkoutgate.App{Git: gitx.New(f), Stderr: &errOut}.Run(context.Background(), args)
	return result{code: code, stderr: errOut.String(), git: f}
}

func (r result) says(t *testing.T, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(r.stderr, p) {
			t.Fatalf("stderr lacks %q:\n%s", p, r.stderr)
		}
	}
}

// G1: clean and level after a fetch passes, naming the upstream on stderr.
func TestG1CleanAndLevelPasses(t *testing.T) {
	r := run(t, world{}, checkout, "--changed")
	if r.code != term.ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	r.says(t, "clean and level with origin/F44")
	if !r.git.Called("git fetch --quiet") {
		t.Fatal("G6: no fetch before the comparison")
	}
	for _, c := range r.git.Calls() {
		if c.Dir != checkout {
			t.Fatalf("%s ran in %q, not the checkout", c.Line(), c.Dir)
		}
	}
}

// G2, G11: uncommitted paths, tracked or untracked, are refused, named, with the fix and --force.
func TestG2DirtyIsRefusedNamingThePaths(t *testing.T) {
	r := run(t, world{status: " M run.bash\x00?? notes.txt\x00"}, checkout, "--changed")
	if r.code != term.ExitFailure {
		t.Fatalf("exit %d", r.code)
	}
	r.says(t, "run.bash --changed refused", "2 uncommitted path(s)", "run.bash", "notes.txt",
		"git commit", "git stash", "./run.bash --changed --force")
}

// G2: a long list names the first ten and counts the rest.
func TestG2ALongListIsCut(t *testing.T) {
	var status strings.Builder
	for i := range 13 {
		status.WriteString("?? f" + strconv.Itoa(i+1) + "\x00")
	}
	r := run(t, world{status: status.String()}, checkout, "--rerun")
	r.says(t, "13 uncommitted path(s)", "f10", "and 3 more")
	if strings.Contains(r.stderr, "f11") {
		t.Fatalf("names more than ten:\n%s", r.stderr)
	}
}

// G3: every finding is reported together; dirty and behind in one refusal.
func TestG3EveryCauseInOneRefusal(t *testing.T) {
	r := run(t, world{status: " M a\x00", behind: 2}, checkout, "--changed")
	if r.code != term.ExitFailure {
		t.Fatalf("exit %d", r.code)
	}
	r.says(t, "1 uncommitted path(s)", "2 commit(s) behind origin/F44", "git pull --ff-only")
}

// G4: a detached HEAD has no upstream: only the clean check applies, and nothing is fetched.
func TestG4DetachedChecksOnlyTheTree(t *testing.T) {
	r := run(t, world{detached: true}, checkout, "--changed")
	if r.code != term.ExitOK {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	r.says(t, "detached at 0123456789ab")
	if r.git.Called("git fetch") {
		t.Fatal("fetched for a detached HEAD")
	}
	r = run(t, world{detached: true, status: " M a\x00"}, checkout, "--changed")
	if r.code != term.ExitFailure {
		t.Fatalf("detached and dirty: exit %d", r.code)
	}
}

// G5: a branch with no upstream is refused with the push that sets one; nothing is fetched.
func TestG5NoUpstreamIsRefused(t *testing.T) {
	r := run(t, world{noUpstream: true}, checkout, "--changed")
	if r.code != term.ExitFailure {
		t.Fatalf("exit %d", r.code)
	}
	r.says(t, "F44 has no upstream", "git push -u origin F44", "--force")
	if r.git.Called("git fetch") {
		t.Fatal("fetched with no upstream")
	}
}

// G6: a failed fetch is a refusal with git's words, never a pass.
func TestG6AFailedFetchIsRefused(t *testing.T) {
	r := run(t, world{fetchFails: true}, checkout, "--changed")
	if r.code != term.ExitFailure {
		t.Fatalf("exit %d", r.code)
	}
	r.says(t, "fetch failed", "Could not resolve host", "cannot be told", "--force")
	if r.git.Called("git rev-list") {
		t.Fatal("compared against tracking refs the fetch did not refresh")
	}
}

// G7, G8, G9: behind, ahead and diverged each name the count and their own fix.
func TestG7G8G9BehindAheadDiverged(t *testing.T) {
	for name, c := range map[string]struct {
		w    world
		says []string
	}{
		"behind":   {world{behind: 3}, []string{"3 commit(s) behind origin/F44", "git pull --ff-only"}},
		"ahead":    {world{ahead: 1}, []string{"1 commit(s) ahead of origin/F44", "no remote holds", "git push"}},
		"diverged": {world{ahead: 1, behind: 2}, []string{"diverged from origin/F44", "1 commit(s) of its own", "2 it lacks", "git pull --rebase", "git push"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := run(t, c.w, checkout, "--changed")
			if r.code != term.ExitFailure {
				t.Fatalf("exit %d", r.code)
			}
			r.says(t, c.says...)
			r.says(t, "./run.bash --changed --force")
		})
	}
}

// G10: an upstream that is set but gone after the fetch is a refusal with git's words.
func TestG10AnUpstreamThatIsGone(t *testing.T) {
	r := run(t, world{upstreamErr: true}, checkout, "--changed")
	if r.code != term.ExitFailure {
		t.Fatalf("exit %d", r.code)
	}
	r.says(t, "F44's upstream cannot be read", "no such branch", "git push -u origin F44")
}

// G12: --force runs anyway over every refusal, saying on stderr what it overrode.
func TestG12ForceRunsOverEveryRefusal(t *testing.T) {
	for name, w := range map[string]world{
		"dirty":       {status: " M a\x00"},
		"no upstream": {noUpstream: true},
		"fetch":       {fetchFails: true},
		"gone":        {upstreamErr: true},
		"behind":      {behind: 1},
		"ahead":       {ahead: 1},
		"diverged":    {ahead: 1, behind: 1},
	} {
		t.Run(name, func(t *testing.T) {
			r := run(t, w, "--force", checkout, "--changed")
			if r.code != term.ExitOK {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			r.says(t, "--force: running anyway")
			if strings.Contains(r.stderr, "refused") {
				t.Fatalf("says refused under --force:\n%s", r.stderr)
			}
		})
	}
}

// G12: --force over a clean, level checkout has nothing to override and says the plain pass.
func TestG12ForceOverNothing(t *testing.T) {
	r := run(t, world{}, "--force", checkout, "--changed")
	if r.code != term.ExitOK || strings.Contains(r.stderr, "running anyway") {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
}

// G13: a git that cannot answer is a fault: exit 1 with git's words, --force or not.
func TestG13AGitFaultIsNotForceable(t *testing.T) {
	for _, broken := range []string{"status", "symbolic-ref", "config", "rev-list"} {
		for _, args := range [][]string{{checkout, "--changed"}, {"--force", checkout, "--changed"}} {
			r := run(t, world{brokenGit: broken}, args...)
			if r.code != term.ExitFailure {
				t.Fatalf("%s %v: exit %d", broken, args, r.code)
			}
			r.says(t, "cannot judge the checkout", "fatal: broken")
		}
	}
}

// G14: the command line is [--force] <checkout> <run.bash option>; anything else is usage.
func TestG14Usage(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{checkout},
		{checkout, "changed"},
		{"--force"},
		{"--forse", checkout, "--changed"},
		{checkout, "--changed", "extra"},
		{"relative", "--changed"},
	} {
		r := run(t, world{}, args...)
		if r.code != term.ExitUsage {
			t.Fatalf("%v: exit %d", args, r.code)
		}
		r.says(t, "usage: checkout-gate [--force] <checkout> <run.bash option>")
		if len(r.git.Calls()) != 0 {
			t.Fatalf("%v: ran git", args)
		}
	}
}

// G15: a failed write of the verdict is a failure.
func TestG15AnUnwritableStderrFails(t *testing.T) {
	f := world{}.fake()
	code := checkoutgate.App{Git: gitx.New(f), Stderr: failWriter{}}.Run(context.Background(), []string{checkout, "--changed"})
	if code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }
