package ccylaunch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const hostOnly = "HOST ONLY\n"

// hostFile is a host file outside the checkout that a container would like ccy to act on.
func hostFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "host-only")
	if err := os.WriteFile(path, []byte(hostOnly), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// keptHost fails the test unless the host file still holds what hostFile wrote.
func keptHost(t *testing.T, path string) {
	t.Helper()
	if raw, err := os.ReadFile(path); err != nil || string(raw) != hostOnly {
		t.Fatalf("the host file was changed: %q %v", raw, err)
	}
}

// filesRun is a bash question about a fresh checkout (its real path in place of each CHECKOUT).
func filesRun(t *testing.T, args ...string) *run {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	given := make([]string, len(args))
	for i, a := range args {
		given[i] = strings.ReplaceAll(a, "CHECKOUT", dir)
	}
	r := newRun(install(t), given...)
	r.front.Dir = dir
	return r
}

// link plants a link at rel in the run's checkout.
func (r *run) link(t *testing.T, rel, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(r.at(rel)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, r.at(rel)); err != nil {
		t.Fatal(err)
	}
}

// refused asserts exit 1 with the checkout file named on stderr and nothing on stdout.
func (r *run) refused(t *testing.T, rel string) {
	t.Helper()
	r.answered(t, term.ExitFailure)
	if r.stdout.Len() != 0 || !strings.Contains(r.stderr.String(), r.front.Dir+"/"+rel) {
		t.Fatalf("stdout %q stderr %q, want the refusal naming %s", r.stdout, r.stderr, rel)
	}
}

// F25: --env-local-copy copies the checkout's ccy.env.local into the launch's private directory
// and prints the copy's path, which the launcher binds: nothing for no file.
func TestEnvLocalCopy(t *testing.T) {
	into := t.TempDir()
	r := filesRun(t, envLocalCopyFlag, "CHECKOUT", into)
	r.answered(t, term.ExitOK)
	if r.stdout.Len() != 0 {
		t.Fatalf("no file printed %q", r.stdout)
	}
	r = filesRun(t, envLocalCopyFlag, "CHECKOUT", into)
	r.write(t, localRel, "export ROLE=a\n")
	r.answered(t, term.ExitOK)
	copied := filepath.Join(into, "ccy.env.local")
	if r.stdout.String() != copied+"\n" {
		t.Fatalf("stdout %q, want %q", r.stdout, copied)
	}
	if raw, err := os.ReadFile(copied); err != nil || string(raw) != "export ROLE=a\n" {
		t.Fatalf("the copy holds %q %v", raw, err)
	}
}

// F25: a link at ccy.env.local is refused, named, and not followed: nothing is copied, so the
// launch binds no host file a container chose (Plan 00266 review finding 1).
func TestEnvLocalCopyRefusesALink(t *testing.T) {
	into, host := t.TempDir(), hostFile(t)
	r := filesRun(t, envLocalCopyFlag, "CHECKOUT", into)
	r.link(t, localRel, host)
	r.refused(t, localRel)
	if entries, err := os.ReadDir(into); err != nil || len(entries) != 0 {
		t.Fatalf("a refused copy left %v %v", entries, err)
	}
	for _, args := range [][]string{{envLocalCopyFlag}, {envLocalCopyFlag, "rel", into}, {envLocalCopyFlag, "CHECKOUT", "rel"}} {
		filesRun(t, args...).answered(t, term.ExitUsage)
	}
}

// F25: --checkout-files writes the .gitignore and the dist as the prelude does, through the
// checkout's os.Root.
func TestCheckoutFilesWrites(t *testing.T) {
	r := filesRun(t, checkoutFilesFlag, "CHECKOUT", "gitignore")
	r.answered(t, term.ExitOK)
	if r.stdout.String() != creatingGitignore || r.read(t, gitignoreRel) != gitignoreText {
		t.Fatalf("stdout %q, .gitignore %q", r.stdout, r.read(t, gitignoreRel))
	}
	r = filesRun(t, checkoutFilesFlag, "CHECKOUT", "dist")
	r.answered(t, term.ExitOK)
	if r.stderr.String() != writingDist || r.read(t, distRel) != distText {
		t.Fatalf("stderr %q, dist %q", r.stderr, r.read(t, distRel))
	}
	filesRun(t, checkoutFilesFlag, "CHECKOUT", "other").answered(t, term.ExitUsage)
}

// F25: a link at the .gitignore, the dist or ccy.env.local, or a .claude/ccy linked out of the
// checkout, is refused and named; the host file is untouched (Plan 00266 review finding 6).
func TestCheckoutFilesRefusesLinks(t *testing.T) {
	for _, c := range []struct{ part, rel string }{
		{"gitignore", gitignoreRel}, {"dist", distRel}, {"dist", localRel},
	} {
		t.Run(c.rel, func(t *testing.T) {
			host := hostFile(t)
			r := filesRun(t, checkoutFilesFlag, "CHECKOUT", c.part)
			r.link(t, c.rel, host)
			r.refused(t, c.rel)
			keptHost(t, host)
		})
	}
	outside := t.TempDir()
	r := filesRun(t, checkoutFilesFlag, "CHECKOUT", "gitignore")
	r.link(t, ".claude/ccy", outside)
	r.answered(t, term.ExitFailure)
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("the write reached %s: %v %v", outside, entries, err)
	}
	dangling := filepath.Join(t.TempDir(), "made")
	r = filesRun(t, checkoutFilesFlag, "CHECKOUT", "gitignore")
	r.link(t, gitignoreRel, dangling)
	r.refused(t, gitignoreRel)
	if _, err := os.Lstat(dangling); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the write created the link's target: %v", err)
	}
}
