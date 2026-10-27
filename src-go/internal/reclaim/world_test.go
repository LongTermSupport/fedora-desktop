package reclaim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// openDir is the mode a test gives back a directory it locked, for its cleanup.
var openDir = fs.FileMode(0o700)

// progRule is what one program answers: argv (sudo already taken off) starting with prefix.
type progRule struct {
	prefix []string
	stdout string
	stderr string
	exit   int
}

// world is one machine for a test: a temporary home, the programs installed, what each
// answers, the lines typed at the terminal and the streams a run writes.
type world struct {
	t         *testing.T
	home      string
	installed map[string]bool
	rules     []progRule
	fake      *execx.Fake
	env       map[string]string
	stdout    bytes.Buffer
	stderr    bytes.Buffer
	tty       string
	noTTY     bool
	stdinTTY  bool
	colour    bool // stderr is a terminal
	host      string
	sudoAsked int // how many times sudo -v asked for a password
	// sudoRefuses makes "sudo -n -v" and "sudo -n true" fail and "sudo -v" refuse.
	sudoRefuses bool
	devNull     string
	// varDirs are the system directories the report sizes (/var/log and /var/cache on a host).
	varDirs []string
	// unscripted are the commands run that nothing scripted; any fails the test at its end.
	unscripted []string
	// runCtx is the run's context; cancel ends it as SignalContext does on a signal.
	runCtx func() context.Context
	cancel context.CancelCauseFunc
	// during runs while the command whose line it is keyed by runs: a Ctrl-C mid-command.
	during map[string]func()
	// cancelledUnder are the lines of the commands whose own context ended while they ran, so
	// that execx.OS would have signalled them instead of waiting for them.
	cancelledUnder []string
	// ttyIn, when set, is the terminal instead of the lines typed.
	ttyIn io.Reader
	// removeAll, when set, is the removal the clears use.
	removeAll func(string) error
}

// ctxRunner is the world's runner: the Fake, with the context each command ran under watched.
type ctxRunner struct{ w *world }

func (r ctxRunner) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	res, err := r.w.fake.Run(ctx, c)
	if hook := r.w.during[c.Line()]; hook != nil {
		hook()
	}
	if ctx.Err() != nil {
		r.w.cancelledUnder = append(r.w.cancelledUnder, c.Line())
	}
	return res, err
}

// interrupt ends the run's context with SIGINT's cause, as Ctrl-C does.
func (w *world) interrupt() {
	w.cancel(&execx.SignalError{Signal: syscall.SIGINT})
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{
		t: t, home: filepath.Join(t.TempDir(), "home"),
		installed: map[string]bool{"sudo": true, "du": true, "df": true, "setsid": true},
		env:       map[string]string{}, stdinTTY: true, host: "box",
		devNull: filepath.Join(t.TempDir(), "null"),
		during:  map[string]func(){},
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	w.runCtx, w.cancel = func() context.Context { return ctx }, cancel
	t.Cleanup(func() { w.cancel(context.Canceled) })
	if err := os.MkdirAll(w.home, 0o750); err != nil {
		t.Fatal(err)
	}
	w.env["HOME"] = w.home
	w.fake = &execx.Fake{}
	w.fake.OnFunc([]string{}, w.dispatch)
	t.Cleanup(func() {
		if len(w.unscripted) > 0 {
			t.Errorf("unscripted commands ran: %q", w.unscripted)
		}
	})
	return w
}

func (w *world) install(progs ...string) {
	for _, p := range progs {
		w.installed[p] = true
	}
}

func (w *world) uninstall(progs ...string) {
	for _, p := range progs {
		delete(w.installed, p)
	}
}

// answer makes argv starting with prefix print stdout and exit with exit. Later rules win.
func (w *world) answer(prefix []string, stdout string, exit int) {
	w.rules = append(w.rules, progRule{prefix: prefix, stdout: stdout, exit: exit})
}

func (w *world) answerErr(prefix []string, stdout, stderr string, exit int) {
	w.rules = append(w.rules, progRule{prefix: prefix, stdout: stdout, stderr: stderr, exit: exit})
}

// typed is what the person types at the terminal, one line each.
func (w *world) typed(lines ...string) {
	w.tty = strings.Join(lines, "\n") + "\n"
}

// file writes a file under the home directory.
func (w *world) file(rel, content string) {
	w.t.Helper()
	p := filepath.Join(w.home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) dir(rel string) string {
	w.t.Helper()
	p := filepath.Join(w.home, rel)
	if err := os.MkdirAll(p, 0o750); err != nil {
		w.t.Fatal(err)
	}
	return p
}

func (w *world) dispatch(c execx.Cmd) (execx.Result, error) {
	argv := append([]string{c.Name}, c.Args...)
	if c.Name == "sudo" {
		if !w.installed["sudo"] {
			return execx.Result{}, &execx.NotInstalledError{Name: "sudo"}
		}
		switch {
		case slices.Equal(c.Args, []string{"-n", "-v"}), slices.Equal(c.Args, []string{"-n", "true"}):
			if w.sudoRefuses {
				return execx.Result{Stderr: []byte("sudo: a password is required\n"), ExitCode: 1}, nil
			}
			return execx.Result{}, nil
		case slices.Equal(c.Args, []string{"-v"}):
			w.sudoAsked++
			if w.sudoRefuses {
				return execx.Result{ExitCode: 1}, nil
			}
			return execx.Result{}, nil
		case len(c.Args) > 1 && c.Args[0] == "-n":
			argv = c.Args[1:]
		default:
			w.t.Errorf("unexpected sudo call: %s", c.Line())
			return execx.Result{ExitCode: 1}, nil
		}
	}
	if !w.installed[argv[0]] {
		return execx.Result{}, &execx.NotInstalledError{Name: argv[0]}
	}
	for _, r := range slices.Backward(w.rules) {
		if startsWith(argv, r.prefix) {
			return execx.Result{Stdout: []byte(r.stdout), Stderr: []byte(r.stderr), ExitCode: r.exit}, nil
		}
	}
	for _, p := range readOnlyDefaults {
		if startsWith(argv, p) {
			return execx.Result{}, nil
		}
	}
	w.unscripted = append(w.unscripted, c.Line())
	return execx.Result{}, fmt.Errorf("%w: %s", execx.ErrUnexpected, c.Line())
}

// readOnlyDefaults are the commands that only read, answered with an empty success when a test
// scripts nothing for them. Every other command a test does not script fails (execx.ErrUnexpected)
// and fails the test.
var readOnlyDefaults = [][]string{
	{"du"},
	{"df"},
	{"duf"},
	{"journalctl", "--disk-usage"},
	{"dnf", "repoquery"},
	{"podman", "system", "df"},
	{"docker", "system", "df"},
	{"flatpak", "uninstall", "--unused", "--assumeno"},
	{"trash-list"},
}

func startsWith(argv, prefix []string) bool {
	return len(prefix) <= len(argv) && slices.Equal(argv[:len(prefix)], prefix)
}

func (w *world) envFor() Env {
	tm := &term.Term{Err: &w.stderr, IsTerminal: func(any) bool { return w.colour }, Getenv: w.getenv}
	return Env{
		Runner: ctxRunner{w}, Fg: w.fake,
		Sudo: &sudo.Sudo{
			Runner: ctxRunner{w}, Term: tm, Stdin: strings.NewReader(""), Stdout: &w.stderr,
			OpenTTY: func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("")), nil },
		},
		Stdout: &w.stdout, Stderr: &w.stderr, Stdin: "stdin",
		Getenv: w.getenv,
		LookPath: func(p string) (string, error) {
			if w.installed[p] {
				return "/usr/bin/" + p, nil
			}
			return "", errors.New("not found")
		},
		Hostname: func() (string, error) { return w.host, nil },
		IsTerminal: func(v any) bool {
			if v == "stdin" {
				return w.stdinTTY
			}
			return w.colour
		},
		OpenTTY: func() (io.ReadCloser, error) {
			if w.noTTY {
				return nil, errors.New("no tty")
			}
			if w.ttyIn != nil {
				return io.NopCloser(w.ttyIn), nil
			}
			return io.NopCloser(strings.NewReader(w.tty)), nil
		},
		RemoveAll: func(p string) error {
			if w.removeAll != nil {
				return w.removeAll(p)
			}
			return os.RemoveAll(p)
		},
		DevNull: func() (*os.File, error) {
			return os.OpenFile(w.devNull, os.O_CREATE|os.O_WRONLY, 0o600)
		},
		Columns: func() int { return 100 },
		VarDirs: w.varDirs,
	}
}

func (w *world) getenv(k string) string { return w.env[k] }

func (w *world) run(args ...string) term.ExitCode {
	w.t.Helper()
	return App{Env: w.envFor()}.Run(w.runCtx(), args)
}

// ran reports whether a command whose line starts with prefix was run (sudo included).
func (w *world) ran(prefix string) bool {
	if w.fake.Called(prefix) {
		return true
	}
	for _, c := range w.fake.Foregrounds() {
		if strings.HasPrefix(c.Line(), prefix) {
			return true
		}
	}
	return false
}

// changes is every command run that may change the machine, as the scenario table writes it:
// every command run as root, and every command one of whose words is in changingWords.
func (w *world) changes() []string {
	var out []string
	for _, c := range w.fake.Calls() {
		argv, root := append([]string{c.Name}, c.Args...), false
		if c.Name == "sudo" {
			if isSudoCredential(c.Args) {
				continue
			}
			argv, root = c.Args[1:], true
		}
		line := strings.Join(argv, " ")
		if root {
			out = append(out, "root "+line)
			continue
		}
		if namesChange(argv) {
			out = append(out, line)
		}
	}
	return out
}

// isSudoCredential is one of internal/sudo's own calls, which get or probe the credential and
// run nothing: -v, -n -v, -n true and -k.
func isSudoCredential(args []string) bool {
	for _, a := range [][]string{{"-v"}, {"-n", "-v"}, {"-n", "true"}, {"-k"}} {
		if slices.Equal(args, a) {
			return true
		}
	}
	return len(args) < 2 || args[0] != "-n"
}

// namesChange is whether a word of argv (an option split at '-' and '=', a path never) is one
// of changingWords.
func namesChange(argv []string) bool {
	for i, arg := range argv {
		if i == 0 {
			arg = filepath.Base(arg)
		} else if strings.Contains(arg, "/") {
			continue
		}
		for word := range strings.FieldsFuncSeq(arg, func(r rune) bool { return r == '-' || r == '=' }) {
			if slices.Contains(changingWords, word) {
				return true
			}
		}
	}
	return false
}

func (w *world) wantErr(subs ...string) {
	w.t.Helper()
	for _, s := range subs {
		if !strings.Contains(w.stderr.String(), s) {
			w.t.Errorf("stderr lacks %q:\n%s", s, w.stderr.String())
		}
	}
}

func (w *world) wantNoErr(subs ...string) {
	w.t.Helper()
	for _, s := range subs {
		if strings.Contains(w.stderr.String(), s) {
			w.t.Errorf("stderr has %q:\n%s", s, w.stderr.String())
		}
	}
}

func (w *world) wantOut(subs ...string) {
	w.t.Helper()
	for _, s := range subs {
		if !strings.Contains(w.stdout.String(), s) {
			w.t.Errorf("stdout lacks %q:\n%s", s, w.stdout.String())
		}
	}
}

func wantCode(t *testing.T, got, want term.ExitCode) {
	t.Helper()
	if got != want {
		t.Fatalf("exit %d, want %d", got, want)
	}
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(des))
	for _, d := range des {
		names = append(names, d.Name())
	}
	return names
}
