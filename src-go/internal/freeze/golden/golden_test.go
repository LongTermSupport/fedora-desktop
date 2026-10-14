// Package golden replays the scenarios the bash podfreeze and lxcfreeze were recorded under and
// compares the Go tools with what the bash printed.
//
// testdata/golden/<scenario>/{stdout,stderr,status,calls} is the bash tools' own output for the
// scenario in testdata/scenarios.json, recorded by CLAUDE/Plan/00192-.../golden-capture/capture.py
// against the shipped bash. This test drives the Go binaries' entry points (App.RunContext) over
// the same world: the same commands answered the same way, the same typed replies, the same fzf
// answers. Every byte of both streams, the exit status and the sequence of commands the tool ran
// must match, except where a numbered bug of SPEC-freeze.md section 10 was fixed on purpose.
//
// Those are pinned in testdata/deviations.json: for each, the bugs it is, why the output moved,
// and which streams differ; testdata/deviation/<scenario>/<stream> is what the Go tool must print
// instead. A deviation that no longer differs from the golden is itself a failure, so the list
// cannot go stale.
//
// `go test -update-deviations` rewrites testdata/deviation/ from what the Go tools print now,
// for the scenarios already listed in deviations.json: review the diff before committing it.
package golden

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/lxcfreeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/podfreeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	podTool = "podfreeze"
	lxcTool = "lxcfreeze"
)

type answer struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Exit   int    `json:"exit"`
	// After, when set, makes this answer hold only once that command has been run.
	After []string `json:"after"`
}

type command struct {
	Argv    []string `json:"argv"`
	Answers []answer `json:"answers"`
}

// fzfAnswer is one run of the picker: the rows picked by index, or (no Pick) an exit status.
type fzfAnswer struct {
	Pick []int `json:"pick"`
	Exit *int  `json:"exit"`
}

type world struct {
	Tools    []string    `json:"tools"`
	Sudo     string      `json:"sudo"`
	Commands []command   `json:"commands"`
	Fzf      []fzfAnswer `json:"fzf"`
}

type scenario struct {
	Name     string            `json:"name"`
	Tool     string            `json:"tool"`
	World    string            `json:"world"`
	Args     []string          `json:"args"`
	TTY      *[]string         `json:"tty"`
	Env      map[string]string `json:"env"`
	Guard    bool              `json:"guard"`
	Tools    *[]string         `json:"tools"`
	Sudo     *string           `json:"sudo"`
	Fzf      *[]fzfAnswer      `json:"fzf"`
	Commands []command         `json:"commands"`
}

type scenarioFile struct {
	Worlds    map[string]world `json:"worlds"`
	Scenarios []scenario       `json:"scenarios"`
}

type deviation struct {
	Bugs    []int    `json:"bugs"`
	Why     string   `json:"why"`
	Streams []string `json:"streams"`
}

var streams = []string{"stdout", "stderr", "status", "calls"}

// suffixes are the file extensions of the recorded streams. They are not bare "stdout" and
// "status" because qa-changed.bash maps a changed file to the suites that mention its name.
var suffixes = map[string]string{"golden": ".golden", "deviation": ".pinned"}

// testdata is the recorded material, read only through this root.
var testdata = os.DirFS("testdata")

var update = flag.Bool("update-deviations", false, "rewrite testdata/deviation/ from what the Go tools print now")

func decodeFile(t *testing.T, path string, into any) {
	t.Helper()
	raw, err := fs.ReadFile(testdata, path)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// resolve is the scenario's world with its overrides applied, by the rule capture.py uses.
func resolve(f scenarioFile, s scenario) world {
	w := f.Worlds[s.World]
	w.Commands = slices.Clone(w.Commands)
	if s.Tools != nil {
		w.Tools = *s.Tools
	}
	if s.Sudo != nil {
		w.Sudo = *s.Sudo
	}
	if s.Fzf != nil {
		w.Fzf = *s.Fzf
	}
	for _, extra := range s.Commands {
		w.Commands = slices.DeleteFunc(w.Commands, func(c command) bool { return slices.Equal(c.Argv, extra.Argv) })
		w.Commands = append(w.Commands, extra)
	}
	return w
}

// machine is the world as the tool sees it through execx: the fake programs of capture.py.
type machine struct {
	w          world
	terminal   bool
	seen       map[string]int
	sudoCached bool
	calls      []string
}

func (m *machine) hasTool(name string) bool { return slices.Contains(m.w.Tools, name) }

func (m *machine) log(argv ...string) {
	var b strings.Builder
	b.WriteString("[")
	for i, a := range argv {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(jsonString(a))
	}
	b.WriteString("]")
	m.calls = append(m.calls, b.String())
}

// jsonString quotes s the way Python's json.dumps does: HTML left alone, everything outside
// ASCII written as \uXXXX.
func jsonString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return fmt.Sprintf("%q", s)
	}
	var out strings.Builder
	for _, r := range strings.TrimSuffix(buf.String(), "\n") {
		if r < utf8.RuneSelf {
			out.WriteRune(r)
			continue
		}
		fmt.Fprintf(&out, "\\u%04x", r)
	}
	return out.String()
}

func (m *machine) run(c execx.Cmd) (execx.Result, error) {
	if !m.hasTool(c.Name) {
		return execx.Result{}, &execx.NotInstalledError{Name: c.Name}
	}
	switch c.Name {
	case "sudo":
		return m.sudo(c.Args)
	case "fzf":
		return m.fzf(c)
	}
	return m.answer(append([]string{c.Name}, c.Args...)), nil
}

func (m *machine) sudo(args []string) (execx.Result, error) {
	cached := m.sudoCached || m.w.Sudo == "cached"
	nonInteractive := len(args) > 0 && args[0] == "-n"
	rest := args
	if nonInteractive {
		rest = args[1:]
	}
	if len(rest) == 0 || rest[0] == "-v" || rest[0] == "true" {
		switch {
		case nonInteractive && cached:
			return execx.Result{}, nil
		case nonInteractive:
			return execx.Result{Stderr: []byte("sudo: a password is required\n"), ExitCode: 1}, nil
		case !cached && !m.terminal:
			return execx.Result{Stderr: []byte("sudo: a terminal is required to read the password\n"), ExitCode: 1}, nil
		case !cached && m.w.Sudo == "refused":
			m.log("sudo-auth", "refused")
			return execx.Result{Stderr: []byte("sudo: 3 incorrect password attempts\n"), ExitCode: 1}, nil
		}
		m.log("sudo-auth", "ok")
		m.sudoCached = true
		return execx.Result{}, nil
	}
	if !cached {
		return execx.Result{Stderr: []byte("sudo: a password is required\n"), ExitCode: 1}, nil
	}
	return m.answer(rest), nil
}

func (m *machine) answer(argv []string) execx.Result {
	for _, c := range m.w.Commands {
		if !slices.Equal(c.Argv, argv) {
			continue
		}
		m.log(argv...)
		// The last answer whose "after" command has been run wins; one with no "after" always holds.
		var a answer
		for _, candidate := range c.Answers {
			if candidate.After == nil || m.seen[strings.Join(candidate.After, "\x00")] > 0 {
				a = candidate
			}
		}
		m.seen[strings.Join(argv, "\x00")]++
		return execx.Result{Stdout: []byte(a.Stdout), Stderr: []byte(a.Stderr), ExitCode: a.Exit}
	}
	m.log(append([]string{"!unscripted"}, argv...)...)
	unscripted, err := json.Marshal(argv)
	if err != nil {
		unscripted = []byte(strings.Join(argv, " "))
	}
	return execx.Result{Stderr: []byte("fake: unscripted command: " + string(unscripted) + "\n"), ExitCode: 127}
}

func (m *machine) fzf(c execx.Cmd) (execx.Result, error) {
	m.log(append([]string{"fzf"}, c.Args...)...)
	if c.Stdin == nil {
		return execx.Result{}, errors.New("fzf was given no rows")
	}
	raw, err := io.ReadAll(c.Stdin)
	if err != nil {
		return execx.Result{}, fmt.Errorf("read fzf rows: %w", err)
	}
	rows := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	n := m.seen["fzf"]
	m.seen["fzf"]++
	if n >= len(m.w.Fzf) {
		return execx.Result{Stderr: []byte("fake: no scripted fzf answer\n"), ExitCode: 127}, nil
	}
	reply := m.w.Fzf[n]
	if reply.Pick != nil {
		var out strings.Builder
		for _, i := range reply.Pick {
			out.WriteString(rows[i] + "\n")
		}
		return execx.Result{Stdout: []byte(out.String())}, nil
	}
	code := 130
	if reply.Exit != nil {
		code = *reply.Exit
	}
	return execx.Result{ExitCode: code}, nil
}

// runner adapts the machine to execx.Runner, streaming to the command's writers as the real
// runner does and reporting a non-zero exit as an *execx.ExitError.
type runner struct{ m *machine }

func (r runner) Run(_ context.Context, c execx.Cmd) (execx.Result, error) {
	res, err := r.m.run(c)
	if err != nil {
		return execx.Result{}, err
	}
	stdout, stderr := res.Stdout, res.Stderr
	if c.Stdout != nil {
		if _, err = c.Stdout.Write(stdout); err != nil {
			return res, fmt.Errorf("write stdout: %w", err)
		}
		res.Stdout = nil
	}
	if c.Stderr != nil {
		if _, err = c.Stderr.Write(stderr); err != nil {
			return res, fmt.Errorf("write stderr: %w", err)
		}
		res.Stderr = nil
	}
	if res.ExitCode != 0 {
		return res, &execx.ExitError{Name: c.Name, Code: res.ExitCode, Stderr: string(res.Stderr)}
	}
	return res, nil
}

type result struct {
	stdout, stderr, status, calls string
}

func (r result) stream(name string) string {
	switch name {
	case "stdout":
		return r.stdout
	case "stderr":
		return r.stderr
	case "status":
		return r.status
	}
	return r.calls
}

type ttyReader struct{ *strings.Reader }

func (ttyReader) Close() error { return nil }

func play(f scenarioFile, s scenario) (result, error) {
	m := &machine{w: resolve(f, s), terminal: s.TTY != nil, seen: map[string]int{}}
	var out, errOut bytes.Buffer
	env := freeze.NewEnv(&out, &errOut, runner{m})
	env.Getenv = func(key string) string { return s.Env[key] }
	env.Stat = func(string) (os.FileInfo, error) { return nil, fs.ErrNotExist }
	env.LookPath = func(name string) (string, error) {
		if m.hasTool(name) {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
	env.IsTerminal = func(any) bool { return m.terminal }
	env.OpenTTY = func() (io.ReadCloser, error) {
		if s.TTY == nil {
			return nil, errors.New("no controlling terminal")
		}
		var typed strings.Builder
		for _, reply := range *s.TTY {
			typed.WriteString(reply + "\n")
		}
		return ttyReader{strings.NewReader(typed.String())}, nil
	}
	var status term.ExitCode
	switch s.Tool {
	case podTool:
		status = (&podfreeze.App{Env: env}).RunContext(context.Background(), s.Args)
	case lxcTool:
		status = (&lxcfreeze.App{Env: env, Stdin: strings.NewReader("")}).RunContext(context.Background(), s.Args)
	default:
		return result{}, fmt.Errorf("scenario %s: unknown tool %q", s.Name, s.Tool)
	}
	calls := strings.Join(m.calls, "\n")
	if calls != "" {
		calls += "\n"
	}
	return result{stdout: out.String(), stderr: errOut.String(), status: fmt.Sprintf("%d\n", status), calls: calls}, nil
}

func readStream(t *testing.T, dir, name, stream string) string {
	t.Helper()
	raw, err := fs.ReadFile(testdata, path.Join(dir, name, stream+suffixes[dir]))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// rewriteDeviation records what the Go tools print now as the pinned output of a deviation.
func rewriteDeviation(t *testing.T, name string, dev deviation, got result) {
	t.Helper()
	root, err := os.OpenRoot("testdata")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	dir := path.Join("deviation", name)
	if err = root.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, stream := range dev.Streams {
		if err = root.WriteFile(path.Join(dir, stream+suffixes["deviation"]), []byte(got.stream(stream)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// check compares what the Go tool printed with the bash golden, stream by stream, or with the
// pinned deviation for a stream that moved on purpose.
func check(t *testing.T, name string, dev *deviation, got result) {
	t.Helper()
	for _, stream := range streams {
		want := readStream(t, "golden", name, stream)
		if dev != nil && slices.Contains(dev.Streams, stream) {
			pinned := readStream(t, "deviation", name, stream)
			if pinned == want {
				t.Errorf("%s: pinned as a deviation but identical to the bash output: remove it from deviations.json", stream)
			}
			want = pinned
		}
		if got.stream(stream) != want {
			t.Errorf("%s differs from the recorded output.\n--- want\n%s\n--- got\n%s", stream, want, got.stream(stream))
		}
	}
}

func TestTheGoToolsPrintWhatTheBashToolsPrinted(t *testing.T) {
	var f scenarioFile
	decodeFile(t, "scenarios.json", &f)
	var deviations map[string]deviation
	decodeFile(t, "deviations.json", &deviations)
	names := map[string]bool{}
	for _, s := range f.Scenarios {
		names[s.Name] = true
		t.Run(s.Name, func(t *testing.T) {
			got, err := play(f, s)
			if err != nil {
				t.Fatal(err)
			}
			dev, deviates := deviations[s.Name]
			if deviates && *update {
				rewriteDeviation(t, s.Name, dev, got)
			}
			if !deviates {
				check(t, s.Name, nil, got)
				return
			}
			check(t, s.Name, &dev, got)
		})
	}
	for name, dev := range deviations {
		if !names[name] {
			t.Errorf("deviation %q names no scenario", name)
		}
		if strings.TrimSpace(dev.Why) == "" || len(dev.Bugs) == 0 || len(dev.Streams) == 0 {
			t.Errorf("deviation %q must name its bugs, its streams and why", name)
		}
	}
}

// TestEveryScenarioHasARecordedGolden stops a scenario being added without being captured: a
// directory of goldens with nothing replaying it is as bad as the reverse.
func TestEveryScenarioHasARecordedGolden(t *testing.T) {
	var f scenarioFile
	decodeFile(t, "scenarios.json", &f)
	entries, err := fs.ReadDir(testdata, "golden")
	if err != nil {
		t.Fatal(err)
	}
	recorded := map[string]bool{}
	for _, e := range entries {
		recorded[e.Name()] = true
	}
	for _, s := range f.Scenarios {
		if !recorded[s.Name] {
			t.Errorf("scenario %q has no golden: run golden-capture/capture.py", s.Name)
		}
		delete(recorded, s.Name)
	}
	for name := range recorded {
		t.Errorf("golden %q has no scenario", name)
	}
}
