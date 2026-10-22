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

// F10: --list-tokens alone lists the pool as the bash does, expiry coloured on a terminal.
func TestListTokensIsAnswered(t *testing.T) {
	dir := install(t)
	r := tokenRun(t, dir, []string{"work.2030-06-01.token", "old.2030-01-14.token", "soon.2030-01-20.token", "plain.token"}, "--list-tokens")
	r.front.IsTerminal = func(any) bool { return true }
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

// D2 (T3): output that is not a terminal, or NO_COLOR, gets no colour.
func TestListTokensColourOnlyOnATerminal(t *testing.T) {
	dir := install(t)
	for name, change := range map[string]func(r *run){
		"not a terminal": func(*run) {},
		"NO_COLOR": func(r *run) {
			r.front.IsTerminal = func(any) bool { return true }
			r.env("NO_COLOR=1")
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := tokenRun(t, dir, []string{"work.2030-06-01.token"}, "--list-tokens")
			change(r)
			if code := r.start(t); code != term.ExitOK {
				t.Fatalf("exit %d, stderr %q", code, r.stderr)
			}
			if out := r.stdout.String(); strings.Contains(out, "\x1b[") || !strings.Contains(out, "    Expires: 2030-06-01 (✓ Valid)\n") {
				t.Fatalf("stdout %q", out)
			}
		})
	}
}

// D1: a typical desktop (a UTF-8 locale that is not C, a zone, names with capitals, digits
// and dashes) is answered by the front, in byte order.
func TestListTokensAnswersATypicalDesktop(t *testing.T) {
	dir := install(t)
	r := tokenRun(t, dir, []string{"work-2.2030-06-01.token", "Work.2030-07-01.token", "play_3.2030-12-31.token"}, "--list-tokens")
	r.front.Environ = []string{"HOME=" + r.home, "PATH=/usr/bin", "LANG=en_GB.UTF-8", "LC_COLLATE=en_GB.UTF-8", "TZ=Europe/London"}
	if code := r.start(t); code != term.ExitOK || len(r.execer.Execs()) != 0 {
		t.Fatalf("exit %d, handed over %d, stderr %q", code, len(r.execer.Execs()), r.stderr)
	}
	out := r.stdout.String()
	first, second, third := strings.Index(out, "• Work\n"), strings.Index(out, "• play_3\n"), strings.Index(out, "• work-2\n")
	if first < 0 || second < first || third < second {
		t.Fatalf("not listed in byte order:\n%s", out)
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

// F4 for --list-tokens: a file the bash would read otherwise, an older or newer bash, or
// another argument: the bash answers.
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
		"another version": func(_ *testing.T, r *run) { r.front.Version = "0.0.1" },
		"no version":      func(_ *testing.T, r *run) { r.front.Version = "" },
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
