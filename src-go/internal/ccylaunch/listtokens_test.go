package ccylaunch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

var pinnedNow = time.Date(2030, 1, 15, 12, 0, 0, 0, time.UTC)

// tokenRun is a front of the install in dir with its own HOME, a token pool of files (none
// when files is nil), and the pinned clock.
func tokenRun(t *testing.T, dir string, files []string, args ...string) *run {
	t.Helper()
	r := newRun(dir, args...)
	r.home = t.TempDir()
	if files != nil {
		if err := os.MkdirAll(r.pool(), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			r.token(t, f)
		}
	}
	r.front.Environ = []string{"HOME=" + r.home, "PATH=/usr/bin", "LANG=C.UTF-8", "TZ=UTC"}
	r.front.Clock = fixedClock{now: pinnedNow}
	return r
}

func (r *run) pool() string { return filepath.Join(r.home, ".claude-tokens", "ccy", "tokens") }

// token writes a token file into the pool, holding "secret-<name>".
func (r *run) token(t *testing.T, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.pool(), name), []byte("secret-"+name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// F10: --list-tokens alone lists the pool as the bash does, expiry coloured as the bash colours
// it, whatever the output is.
func TestListTokensIsAnswered(t *testing.T) {
	dir := install(t)
	r := tokenRun(t, dir, []string{"work.2030-06-01.token", "old.2030-01-14.token", "soon.2030-01-20.token", "plain.token"}, "--list-tokens")
	if code := r.start(t); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, r.stderr)
	}
	pool := r.pool()
	for _, want := range []string{
		"Available Claude Code Tokens for ccy\n",
		"Token storage: " + pool + "\n",
		"  • old\n    File: " + pool + "/old.2030-01-14.token\n    Expires: \x1b[31m2030-01-14\x1b[0m (✗ EXPIRED)\n",
		"    Expires: \x1b[33m2030-01-20\x1b[0m (✓ Valid)\n",
		"    Expires: \x1b[32m2030-06-01\x1b[0m (✓ Valid)\n",
		"  • plain.token\n    File: " + pool + "/plain.token\n    Status: ✗ INVALID FORMAT (missing expiry date)\n",
	} {
		if !strings.Contains(r.stdout.String(), want) {
			t.Errorf("the listing lacks %q:\n%s", want, r.stdout)
		}
	}
	if len(r.execer.Execs()) != 0 || strings.Contains(r.stdout.String(), "secret-") {
		t.Fatal("the front handed over, or printed a token")
	}
}

func TestListTokensOfAnEmptyPoolExitsOne(t *testing.T) {
	dir := install(t)
	r := tokenRun(t, dir, nil, "--list-tokens")
	if code := r.start(t); code != term.ExitFailure {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(r.stdout.String(), "No tokens found in: ") {
		t.Fatalf("stdout %q", r.stdout)
	}
}

// F4 for --list-tokens: a pool the bash would read otherwise, an unclean path, another zone
// reading, an older or newer bash, or another argument: the bash answers.
func TestListTokensHandsOverWhereTheBashReadsOtherwise(t *testing.T) {
	dir := install(t)
	cases := map[string]func(t *testing.T, r *run){
		"a name only the bash dates": func(t *testing.T, r *run) {
			t.Helper()
			r.token(t, "work2030-06-01.token")
		},
		"a directory named as a token": func(t *testing.T, r *run) {
			t.Helper()
			if err := os.Mkdir(filepath.Join(r.pool(), "d.token"), 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"a collation that sorts otherwise": func(t *testing.T, r *run) {
			t.Helper()
			r.env("LC_ALL=en_GB.UTF-8")
			r.token(t, "Work.2030-06-01.token")
		},
		"a zone Go cannot read":    func(_ *testing.T, r *run) { r.front.Environ[3] = "TZ=UTC0" },
		"a home that is not clean": func(_ *testing.T, r *run) { r.front.Environ[0] = "HOME=" + r.home + "/" },
		"another version":          func(_ *testing.T, r *run) { r.front.Version = "0.0.1" },
		"no version":               func(_ *testing.T, r *run) { r.front.Version = "" },
		"the clocks change today": func(t *testing.T, r *run) {
			t.Helper()
			london, err := time.LoadLocation("Europe/London")
			if err != nil {
				t.Fatal(err)
			}
			r.front.Clock = fixedClock{now: time.Date(2030, 3, 31, 12, 0, 0, 0, london)}
			r.token(t, "next.2030-04-01.token")
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r := tokenRun(t, dir, []string{"work.2030-06-01.token"}, "--list-tokens")
			change(t, r)
			r.start(t)
			r.handedOver(t, dir)
		})
	}
	r := tokenRun(t, dir, []string{"work.2030-06-01.token"}, "--list-tokens", "--no-ssh")
	r.start(t)
	r.handedOver(t, dir)
}
