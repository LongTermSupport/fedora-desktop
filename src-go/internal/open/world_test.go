package open

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// world is one machine for a test: a temporary home with XDG directories, the programs on
// PATH, the environment, a scripted runner and picker, and the terminal.
type world struct {
	t          *testing.T
	home       string
	dataDirs   string // the system applications parent: <dataDirs>/applications
	env        map[string]string
	installed  map[string]bool
	unreadable map[string]bool
	runner     *fakeRunner
	picker     *fakePicker
	stdout     bytes.Buffer
	stderr     bytes.Buffer
	terminal   bool   // stderr is a terminal
	tty        string // what the controlling terminal answers; "" with noTTY means none
	noTTY      bool
	signals    chan os.Signal
}

func newWorld(t *testing.T) *world {
	t.Helper()
	root := t.TempDir()
	w := &world{
		t: t, home: filepath.Join(root, "home"), dataDirs: filepath.Join(root, "usr", "share"),
		env: map[string]string{}, installed: map[string]bool{}, unreadable: map[string]bool{},
		runner: &fakeRunner{}, picker: &fakePicker{}, terminal: true,
		signals: make(chan os.Signal, 1),
	}
	w.env["HOME"] = w.home
	w.env["XDG_DATA_DIRS"] = w.dataDirs
	for _, d := range []string{w.home, filepath.Join(w.dataDirs, "applications")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w.runner.On([]string{"tput", "cols"}, execx.Result{Stdout: []byte("100\n")})
	return w
}

// install puts programs on PATH.
func (w *world) install(progs ...string) {
	for _, p := range progs {
		w.installed[p] = true
	}
}

// file writes a file under the home directory and returns its path.
func (w *world) file(name, content string) string {
	w.t.Helper()
	p := filepath.Join(w.home, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
	return p
}

// desktop writes a desktop entry into the system applications directory.
func (w *world) desktop(rel, content string) string {
	w.t.Helper()
	p := filepath.Join(w.dataDirs, "applications", rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
	return p
}

// gui makes this a graphical session with gio installed, answering `gio mime <mime>` with
// out.
func (w *world) gui(mime, out string) {
	w.env["WAYLAND_DISPLAY"] = "wayland-0"
	w.install("gio")
	w.runner.On([]string{"gio", "mime", mime}, execx.Result{Stdout: []byte(out)})
}

// types makes `file` the installed detector, answering mime for every path.
func (w *world) types(mime string) {
	w.install("file")
	w.runner.On([]string{"file", "--brief", "--mime-type", "--"}, execx.Result{Stdout: []byte(mime + "\n")})
}

func (w *world) app() App {
	return App{Env: Env{
		Runner: w.runner, Picker: w.picker,
		Stdin: strings.NewReader(""), Stdout: &w.stdout, Stderr: &w.stderr,
		Getenv: func(k string) string { return w.env[k] },
		LookPath: func(p string) (string, error) {
			if w.installed[p] {
				return "/usr/bin/" + p, nil
			}
			return "", errors.New("not found")
		},
		Readable: func(p string) error {
			if w.unreadable[p] {
				return errors.New("permission denied")
			}
			return nil
		},
		IsTerminal: func(any) bool { return w.terminal },
		OpenTTY: func() (io.ReadCloser, error) {
			if w.noTTY {
				return nil, errors.New("no tty")
			}
			return io.NopCloser(strings.NewReader(w.tty)), nil
		},
		Interrupts: w.signals,
	}}
}

func (w *world) run(args ...string) term.ExitCode {
	w.t.Helper()
	return w.app().Run(context.Background(), args)
}

// ran is every command line the runner saw, except the width probes.
func (w *world) ran() []string {
	var lines []string
	for _, c := range w.runner.Calls() {
		if c.Name == "tput" {
			continue
		}
		lines = append(lines, c.Line())
	}
	return lines
}

func (w *world) called(line string) bool { return slices.Contains(w.ran(), line) }

// cmd is the recorded command whose line is line.
func (w *world) cmd(line string) execx.Cmd {
	w.t.Helper()
	for _, c := range w.runner.Calls() {
		if c.Line() == line {
			return c
		}
	}
	w.t.Fatalf("no command %q among %q", line, w.ran())
	return execx.Cmd{}
}

func (w *world) wantStderr(part string) {
	w.t.Helper()
	if !strings.Contains(w.stderr.String(), part) {
		w.t.Fatalf("stderr lacks %q:\n%s", part, w.stderr.String())
	}
}

// fakeRunner is execx.Fake's script and record without its lock held while a rule answers: a
// pipeline runs its producer and its pager at once, each answered by a rule that may wait on the
// other, which execx.Fake (locked while a rule runs) would deadlock.
type fakeRunner struct {
	mu    sync.Mutex
	rules []fakeRule
	calls []execx.Cmd
}

type fakeRule struct {
	prefix []string
	result execx.Result
	fn     func(execx.Cmd) (execx.Result, error)
	err    error
}

// On answers every command whose argv starts with prefix; later rules win.
func (f *fakeRunner) On(prefix []string, r execx.Result) { f.add(fakeRule{prefix: prefix, result: r}) }

// OnFunc answers with fn, called without the runner locked.
func (f *fakeRunner) OnFunc(prefix []string, fn func(execx.Cmd) (execx.Result, error)) {
	f.add(fakeRule{prefix: prefix, fn: fn})
}

// OnError makes the command fail to start with err.
func (f *fakeRunner) OnError(prefix []string, err error) { f.add(fakeRule{prefix: prefix, err: err}) }

// Run implements execx.Runner.
func (f *fakeRunner) Run(_ context.Context, c execx.Cmd) (execx.Result, error) {
	r, found := f.rule(c)
	if !found {
		return execx.Result{}, fmt.Errorf("%w: %s", execx.ErrUnexpected, c.Line())
	}
	if r.err != nil {
		return execx.Result{}, r.err
	}
	res := r.result
	if r.fn != nil {
		var err error
		if res, err = r.fn(c); err != nil {
			return execx.Result{}, err
		}
	}
	if c.Stdout != nil && len(res.Stdout) > 0 {
		if _, err := c.Stdout.Write(res.Stdout); err != nil {
			return res, fmt.Errorf("fake stdout: %w", err)
		}
	}
	if c.Stderr != nil && len(res.Stderr) > 0 {
		if _, err := c.Stderr.Write(res.Stderr); err != nil {
			return res, fmt.Errorf("fake stderr: %w", err)
		}
	}
	if res.ExitCode != 0 {
		return res, &execx.ExitError{Name: c.Name, Code: res.ExitCode, Stderr: string(res.Stderr)}
	}
	return res, nil
}

// Calls is every command run so far, in order.
func (f *fakeRunner) Calls() []execx.Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// Called reports whether any command's line starts with prefix.
func (f *fakeRunner) Called(prefix string) bool {
	for _, c := range f.Calls() {
		if strings.HasPrefix(c.Line(), prefix) {
			return true
		}
	}
	return false
}

func (f *fakeRunner) add(r fakeRule) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rules = append(f.rules, r)
}

// rule records c and finds the latest rule whose prefix its argv starts with.
func (f *fakeRunner) rule(c execx.Cmd) (fakeRule, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	argv := append([]string{c.Name}, c.Args...)
	for _, r := range slices.Backward(f.rules) {
		if len(r.prefix) <= len(argv) && slices.Equal(argv[:len(r.prefix)], r.prefix) {
			return r, true
		}
	}
	return fakeRule{}, false
}

// fakePicker answers each Pick from a script and records the specs and headings it was given.
type fakePicker struct {
	answers  []pickAnswer
	specs    []ui.PickSpec
	headings []ui.Heading
}

type pickAnswer struct {
	picked ui.Picked
	err    error
}

func (p *fakePicker) Pick(_ context.Context, h ui.Heading, s ui.PickSpec) (ui.Picked, error) {
	p.specs = append(p.specs, s)
	p.headings = append(p.headings, h)
	if len(p.answers) == 0 {
		return ui.Picked{}, errors.New("fakePicker: unscripted pick")
	}
	a := p.answers[0]
	p.answers = p.answers[1:]
	return a.picked, a.err
}

// chooseRow scripts the picker to choose the row at place i (its hidden tag).
func (p *fakePicker) chooseRow(i string) {
	p.answers = append(p.answers, pickAnswer{picked: ui.Picked{Tag: i}})
}

func (p *fakePicker) fail(err error) {
	p.answers = append(p.answers, pickAnswer{err: err})
}
