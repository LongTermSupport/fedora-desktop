package ccproject

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	checkout = "/srv/fedora-desktop"
	home     = "/home/u"
)

type world struct {
	fake    *execx.Fake
	stdout  bytes.Buffer
	stderr  bytes.Buffer
	stdin   string
	tty     bool
	restore string
	root    string
}

func newWorld() *world {
	return &world{fake: &execx.Fake{}, tty: true, root: checkout}
}

func gitFail(msg string) func(execx.Cmd) (execx.Result, error) {
	return func(execx.Cmd) (execx.Result, error) {
		return execx.Result{ExitCode: 128, Stderr: []byte(msg)}, nil
	}
}

// outside scripts git so the working directory is in no repository and the checkout is one.
func (w *world) outside() {
	w.fake.OnFunc([]string{"git", "rev-parse", "--is-inside-work-tree"}, func(c execx.Cmd) (execx.Result, error) {
		if c.Dir == checkout {
			return execx.Result{Stdout: []byte("true\n")}, nil
		}
		return gitFail("fatal: not a git repository (or any of the parent directories): .git")(c)
	})
}

func (w *world) run(args ...string) term.ExitCode {
	app := App{
		Runner:     w.fake,
		Stdin:      strings.NewReader(w.stdin),
		Stdout:     &w.stdout,
		Stderr:     &w.stderr,
		IsTerminal: func(any) bool { return w.tty },
		Getenv: func(k string) string {
			if k == "CCY_SESSION_RESTORE" {
				return w.restore
			}
			return ""
		},
		Getwd:   func() (string, error) { return home, nil },
		RootDir: w.root,
	}
	return app.Run(context.Background(), args)
}

func (w *world) asked() int { return strings.Count(w.stderr.String(), "[Y/n]") }

// P1.
func TestHelpAndArguments(t *testing.T) {
	w := newWorld()
	if code := w.run("--help"); code != term.ExitOK {
		t.Fatalf("--help: exit %d", code)
	}
	if !strings.Contains(w.stdout.String(), "usage: cc-project-dir") {
		t.Errorf("--help printed %q", w.stdout.String())
	}
	w = newWorld()
	if code := w.run("extra"); code != term.ExitUsage {
		t.Fatalf("an argument: exit %d, want %d", code, term.ExitUsage)
	}
	if len(w.fake.Calls()) != 0 || w.stdout.Len() != 0 {
		t.Errorf("a usage error ran %v and printed %q", w.fake.Calls(), w.stdout.String())
	}
}

// P2.
func TestRefusesABuildWithoutTheCheckout(t *testing.T) {
	w := newWorld()
	w.root = ""
	if code := w.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"-X main.RootDir", "play-claude-yolo.yml"} {
		if !strings.Contains(w.stderr.String(), want) {
			t.Errorf("stderr does not name %q: %q", want, w.stderr.String())
		}
	}
}

// P3.
func TestInsideARepositoryPrintsItsTopAndAsksNothing(t *testing.T) {
	w := newWorld()
	w.fake.On([]string{"git", "rev-parse", "--is-inside-work-tree"}, execx.Result{Stdout: []byte("true\n")})
	w.fake.On([]string{"git", "rev-parse", "--show-toplevel"}, execx.Result{Stdout: []byte("/srv/proj\n")})
	if code := w.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	if w.stdout.String() != "/srv/proj\n" {
		t.Errorf("stdout %q, want the top level", w.stdout.String())
	}
	if w.stderr.Len() != 0 {
		t.Errorf("stderr %q, want nothing", w.stderr.String())
	}
	for _, c := range w.fake.Calls() {
		if c.Dir != home {
			t.Errorf("%s ran in %q, want the working directory", c.Line(), c.Dir)
		}
	}
}

// P4.
func TestGitMissingIsItsOwnError(t *testing.T) {
	w := newWorld()
	w.fake.OnError([]string{"git"}, &execx.NotInstalledError{Name: "git"})
	if code := w.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stderr.String(), "git is not installed") {
		t.Errorf("stderr %q", w.stderr.String())
	}
	if strings.Contains(w.stderr.String(), "not in a git repository") {
		t.Errorf("a missing git was reported as no repository: %q", w.stderr.String())
	}
}

func TestGitFailingOtherwiseFails(t *testing.T) {
	w := newWorld()
	w.fake.OnError([]string{"git"}, exec.ErrDot)
	if code := w.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if w.stdout.Len() != 0 {
		t.Errorf("stdout %q", w.stdout.String())
	}
}

// P5.
func TestNoTerminalRefusesAndAsksNothing(t *testing.T) {
	for name, set := range map[string]func(*world){
		"stdin not a terminal": func(w *world) { w.tty = false },
		"a session restore":    func(w *world) { w.restore = "1" },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld()
			w.outside()
			w.stdin = "y\n"
			set(w)
			if code := w.run(); code != term.ExitFailure {
				t.Fatalf("exit %d", code)
			}
			if w.asked() != 0 || w.stdout.Len() != 0 {
				t.Errorf("asked %d times, stdout %q", w.asked(), w.stdout.String())
			}
			assertRefusal(t, w)
		})
	}
}

func assertRefusal(t *testing.T, w *world) {
	t.Helper()
	for _, want := range []string{home + " is not in a git repository", "cd into a project", "claude"} {
		if !strings.Contains(w.stderr.String(), want) {
			t.Errorf("stderr does not say %q: %q", want, w.stderr.String())
		}
	}
}

// P6.
func TestOfferAccepted(t *testing.T) {
	for _, answer := range []string{"\n", "y\n", "Y\n", " yes \n", "YES\n"} {
		w := newWorld()
		w.outside()
		w.stdin = answer
		if code := w.run(); code != term.ExitOK {
			t.Fatalf("answer %q: exit %d: %s", answer, code, w.stderr.String())
		}
		if w.stdout.String() != checkout+"\n" {
			t.Errorf("answer %q: stdout %q, want the checkout", answer, w.stdout.String())
		}
		if !strings.Contains(w.stderr.String(), checkout) || w.asked() != 1 {
			t.Errorf("answer %q: the offer %q", answer, w.stderr.String())
		}
	}
}

func TestOfferDeclined(t *testing.T) {
	for _, answer := range []string{"n\n", "N\n", "no\n"} {
		w := newWorld()
		w.outside()
		w.stdin = answer
		if code := w.run(); code != term.ExitFailure {
			t.Fatalf("answer %q: exit %d", answer, code)
		}
		if w.stdout.Len() != 0 {
			t.Errorf("answer %q: stdout %q", answer, w.stdout.String())
		}
		assertRefusal(t, w)
	}
}

func TestOfferRePromptsOnAnythingElse(t *testing.T) {
	w := newWorld()
	w.outside()
	w.stdin = "maybe\ny\n"
	if code := w.run(); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	if w.asked() != 2 || !strings.Contains(w.stderr.String(), "Please answer y or n") {
		t.Errorf("stderr %q", w.stderr.String())
	}
}

func TestOfferGivesUpAfterThreeTries(t *testing.T) {
	w := newWorld()
	w.outside()
	w.stdin = "a\nb\nc\ny\n"
	if code := w.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if w.asked() != maxTries || w.stdout.Len() != 0 {
		t.Errorf("asked %d, stdout %q", w.asked(), w.stdout.String())
	}
	if !strings.Contains(w.stderr.String(), "no valid answer") {
		t.Errorf("stderr %q", w.stderr.String())
	}
	assertRefusal(t, w)
}

func TestOfferEndOfInputCancels(t *testing.T) {
	for _, input := range []string{"", "y"} {
		w := newWorld()
		w.outside()
		w.stdin = input
		if code := w.run(); code != term.ExitFailure {
			t.Fatalf("input %q: exit %d", input, code)
		}
		if w.stdout.Len() != 0 || !strings.Contains(w.stderr.String(), "cancelled") {
			t.Errorf("input %q: stdout %q, stderr %q", input, w.stdout.String(), w.stderr.String())
		}
	}
}

// P7.
func TestOfferRefusesABrokenCheckout(t *testing.T) {
	w := newWorld()
	w.fake.OnFunc([]string{"git", "rev-parse", "--is-inside-work-tree"}, gitFail("fatal: not a git repository"))
	w.stdin = "y\n"
	if code := w.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if w.asked() != 0 || w.stdout.Len() != 0 {
		t.Errorf("offered a broken checkout: %q", w.stderr.String())
	}
	if !strings.Contains(w.stderr.String(), checkout) || !strings.Contains(w.stderr.String(), "play-claude-yolo.yml") {
		t.Errorf("stderr %q", w.stderr.String())
	}
}

func TestOfferRefusesAnUnreadableCheckout(t *testing.T) {
	w := newWorld()
	w.fake.OnFunc([]string{"git", "rev-parse", "--is-inside-work-tree"}, func(c execx.Cmd) (execx.Result, error) {
		if c.Dir == checkout {
			return execx.Result{}, errors.New("chdir: no such file or directory")
		}
		return gitFail("fatal: not a git repository")(c)
	})
	if code := w.run(); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if w.asked() != 0 || !strings.Contains(w.stderr.String(), checkout) {
		t.Errorf("stderr %q", w.stderr.String())
	}
}

func TestWorkingDirectoryUnreadableFails(t *testing.T) {
	w := newWorld()
	app := App{
		Runner: w.fake, Stdin: strings.NewReader(""), Stdout: &w.stdout, Stderr: &w.stderr,
		IsTerminal: func(any) bool { return true }, Getenv: func(string) string { return "" },
		Getwd:   func() (string, error) { return "", errors.New("getwd: no such file") },
		RootDir: checkout,
	}
	if code := app.Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if len(w.fake.Calls()) != 0 || !strings.Contains(w.stderr.String(), "working directory") {
		t.Errorf("calls %v, stderr %q", w.fake.Calls(), w.stderr.String())
	}
}

func TestNewAppIsTheRealProcess(t *testing.T) {
	app := NewApp(checkout)
	if app.Runner == nil || app.Stdin == nil || app.Stdout == nil || app.Stderr == nil ||
		app.IsTerminal == nil || app.Getenv == nil || app.Getwd == nil || app.RootDir != checkout {
		t.Errorf("NewApp left a seam unset: %+v", app)
	}
}
