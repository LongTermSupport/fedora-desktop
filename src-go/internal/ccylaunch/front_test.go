package ccylaunch

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const fixtureVersion = "9.8.7"

// fixtureBash is a bash launcher's text as far as the front reads it.
const fixtureBash = "#!/bin/bash\nCCY_VERSION=\"" + fixtureVersion + "\"  # a release\n" +
	"CCY_LIBS=(token-handoff common host-paths)\nCCY_HASH=$(\n    hashing\n)\n"

// install lays out a front's directory: the bash launcher beside it and its libraries.
func install(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		BashName:                    fixtureBash,
		"lib/token-handoff.bash":    "handoff\n",
		"lib/common.bash":           "common\n",
		"lib/common-pure.bash":      "pure\n",
		"lib/host-paths.bash":       "paths\n",
		"lib/ssh_agent_filter.py":   "agent\n",
		"lib/README-not-hashed.txt": "x\n",
	})
	return dir
}

type run struct {
	front          Front
	stdout, stderr *bytes.Buffer
	runner         *execx.Fake
	execer         *execx.Fake
}

// newRun is the front of the install in dir, given args, with podman and every tool the bash
// runs first on PATH.
func newRun(dir string, args ...string) *run {
	r := &run{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, runner: &execx.Fake{}, execer: &execx.Fake{}}
	r.front = Front{
		Version:  fixtureVersion,
		Self:     filepath.Join(dir, "claude-yolo"),
		Args:     args,
		Environ:  []string{"HOME=/nonexistent", "PATH=/usr/bin"},
		LookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
		Runner:   r.runner,
		Execer:   r.execer,
		Stdout:   r.stdout,
		Stderr:   r.stderr,
	}
	return r
}

func (r *run) env(kv ...string) *run {
	r.front.Environ = append(r.front.Environ, kv...)
	return r
}

func (r *run) start(t *testing.T) term.ExitCode {
	t.Helper()
	return r.front.Run(context.Background())
}

// handedOver asserts the run exec'd the bash launcher beside the front with the arguments as
// given and the environment untouched.
func (r *run) handedOver(t *testing.T, dir string) {
	t.Helper()
	execs := r.execer.Execs()
	if len(execs) != 1 {
		t.Fatalf("exec'd %d programs, want the bash launcher once (stdout %q)", len(execs), r.stdout)
	}
	c := execs[0]
	if c.Name != filepath.Join(dir, BashName) || !slices.Equal(c.Args, r.front.Args) {
		t.Fatalf("exec'd %s %q, want %s %q", c.Name, c.Args, filepath.Join(dir, BashName), r.front.Args)
	}
	if c.Env != nil || c.Unset != nil || c.Secrets != nil || c.Dir != "" {
		t.Fatalf("the hand-over changed the environment or directory: %+v", c)
	}
	if r.stdout.Len() != 0 || len(r.runner.Calls()) != 0 {
		t.Fatalf("the front printed %q or ran %d commands before handing over", r.stdout, len(r.runner.Calls()))
	}
}

func fixtureHash(t *testing.T, dir string) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(dir, BashName))
	if err != nil {
		t.Fatal(err)
	}
	h, _, err := IntegrityHash(text, filepath.Join(dir, "lib"))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// F1: --version and -v are answered by the front: its version and the launcher's hash.
func TestVersionIsAnswered(t *testing.T) {
	dir := install(t)
	for _, flag := range []string{"--version", "-v"} {
		r := newRun(dir, flag, "ignored")
		if code := r.start(t); code != term.ExitOK {
			t.Fatalf("%s: exit %d, stderr %q", flag, code, r.stderr)
		}
		want := "ccy version " + fixtureVersion + "\nHash: " + fixtureHash(t, dir) + "\n"
		if r.stdout.String() != want || r.stderr.Len() != 0 {
			t.Fatalf("%s printed %q / %q, want %q", flag, r.stdout, r.stderr, want)
		}
		if len(r.execer.Execs()) != 0 || len(r.runner.Calls()) != 0 {
			t.Fatalf("%s ran or exec'd a program", flag)
		}
	}
}

// F2: everything else is the bash launcher's, with the same arguments and environment.
func TestEverythingElseIsHandedOver(t *testing.T) {
	dir := install(t)
	for _, args := range [][]string{nil, {"--token", "work"}, {"--list-tokens"}, {"fix --version"}, {"-V"}, {"--", "--help"}} {
		r := newRun(dir, args...)
		if code := r.start(t); code != term.ExitOK {
			t.Fatalf("%q: exit %d", args, code)
		}
		r.handedOver(t, dir)
	}
}

// F3: CCY_LEGACY sends every run to the bash launcher, the handled flags included; 0 or empty
// does not.
func TestLegacyRoutesEverythingToBash(t *testing.T) {
	dir := install(t)
	for _, v := range []string{"1", "yes"} {
		r := newRun(dir, "--version").env(LegacyEnv + "=" + v)
		r.start(t)
		r.handedOver(t, dir)
	}
	for _, v := range []string{"0", ""} {
		r := newRun(dir, "--version").env(LegacyEnv + "=" + v)
		if code := r.start(t); code != term.ExitOK || len(r.execer.Execs()) != 0 {
			t.Fatalf("CCY_LEGACY=%q: exit %d, exec'd %d", v, code, len(r.execer.Execs()))
		}
	}
}

// F4: wherever the bash would not reach its --version line as the front models it, the front
// hands over and the bash says what it says.
func TestHandsOverWhereTheBashWouldNotAnswerAsModelled(t *testing.T) {
	env := func(kv string) func(string, *run) error {
		return func(_ string, r *run) error { r.env(kv); return nil }
	}
	missing := func(tool, kv string) func(string, *run) error {
		return func(_ string, r *run) error {
			r.env(kv)
			r.front.LookPath = func(name string) (string, error) {
				if name == tool {
					return "", exec.ErrNotFound
				}
				return "/usr/bin/" + name, nil
			}
			return nil
		}
	}
	remove := func(rel string) func(string, *run) error {
		return func(dir string, _ *run) error { return os.Remove(filepath.Join(dir, filepath.FromSlash(rel))) }
	}
	rewrite := func(text string) func(string, *run) error {
		return func(dir string, _ *run) error {
			return os.WriteFile(filepath.Join(dir, BashName), []byte(text), 0o600)
		}
	}
	cases := []struct {
		name  string
		setup func(dir string, r *run) error
	}{
		{"a GitHub token hand-off to take", env("CCY_GH_TOKEN_HANDOFF=/run/user/1/ccy-gh-token.abcdef/token")},
		{"BASH_ENV", env("BASH_ENV=/x")},
		{"SHELLOPTS", env("SHELLOPTS=xtrace")},
		{"an exported function", env("BASH_FUNC_podman%%=() { :; }")},
		{"an engine ccy does not drive", env("CCY_CONTAINER_ENGINE=nerdctl")},
		{"an engine not on PATH", missing("podman", "X=1")},
		{"a tool the bash runs first not on PATH", missing("md5sum", "X=1")},
		{"realpath missing with an override set", missing("realpath", "CCY_ZONEINFO_DIR=/usr/share/zoneinfo")},
		{"an override the library refuses", env("CCY_GPU_DEVICE_DIR=/dev")},
		{"a library missing", remove("lib/host-paths.bash")},
		{"common-pure missing", remove("lib/common-pure.bash")},
		{"the bash launcher missing", remove(BashName)},
		{"no CCY_LIBS line", rewrite(strings.Replace(fixtureBash, "CCY_LIBS=", "LIBS=", 1))},
		{"no CCY_VERSION line", rewrite(strings.Replace(fixtureBash, "CCY_VERSION=", "VERSION=", 1))},
		{"a NUL in the launcher", rewrite(fixtureBash + "\x00")},
	}
	for _, c := range cases {
		for _, flag := range []string{"--version", "--help"} {
			t.Run(c.name+" "+flag, func(t *testing.T) {
				dir := install(t)
				r := newRun(dir, flag)
				if err := c.setup(dir, r); err != nil {
					t.Fatal(err)
				}
				if code := r.start(t); code != term.ExitOK {
					t.Fatalf("exit %d, stderr %q", code, r.stderr)
				}
				r.handedOver(t, dir)
			})
		}
	}
}

// F5: the bash's own conditions, met, are answered: docker chosen by the variable, an empty
// variable, an override the library accepts.
func TestAnswersWhereTheBashWould(t *testing.T) {
	dir := install(t)
	for _, kv := range []string{
		"CCY_CONTAINER_ENGINE=docker", "CCY_CONTAINER_ENGINE=", "CCY_GPU_DEVICE_DIR=/dev/dri",
		"CCY_DOCKERFILE_DIR=" + dir, "CCY_GH_TOKEN_HANDOFF=", "BASH_ENV=",
	} {
		env := []string{kv}
		r := newRun(dir, "-v").env(env...)
		if code := r.start(t); code != term.ExitOK || len(r.execer.Execs()) != 0 {
			t.Fatalf("%q: exit %d, handed over %d", env, code, len(r.execer.Execs()))
		}
	}
}

// F6: a front and a bash launcher of different versions are two checkouts' halves: refused,
// naming both, and so is a front built without its version.
func TestVersionMismatchIsRefused(t *testing.T) {
	dir := install(t)
	r := newRun(dir, "--version")
	r.front.Version = "9.8.8"
	if code := r.start(t); code != term.ExitFailure {
		t.Fatalf("exit %d, want 1", code)
	}
	for _, want := range []string{"9.8.8", fixtureVersion, filepath.Join(dir, BashName), "play-claude-yolo.yml"} {
		if !strings.Contains(r.stderr.String(), want) {
			t.Fatalf("the refusal %q does not name %q", r.stderr, want)
		}
	}
	if r.stdout.Len() != 0 || len(r.execer.Execs()) != 0 {
		t.Fatal("a mismatched front answered or handed over")
	}
	r = newRun(dir, "--help")
	r.front.Version = ""
	if code := r.start(t); code != term.ExitFailure || !strings.Contains(r.stderr.String(), "main.Version") {
		t.Fatalf("a front with no version: exit %d, stderr %q", code, r.stderr)
	}
}

// F7: a hand-over that cannot exec fails, naming the bash launcher's path and why.
func TestAFailedHandOverNamesThePath(t *testing.T) {
	dir := install(t)
	r := newRun(dir, "--token", "work")
	r.execer.ExecError(errors.New("exec: permission denied"))
	if code := r.start(t); code != term.ExitFailure {
		t.Fatalf("exit %d, want 1", code)
	}
	for _, want := range []string{filepath.Join(dir, BashName), "permission denied", LegacyEnv} {
		if !strings.Contains(r.stderr.String(), want) {
			t.Fatalf("stderr %q does not name %q", r.stderr, want)
		}
	}
}

// The real exec of a launcher that is not there returns, so the message is printed.
func TestAMissingBashLauncherIsReported(t *testing.T) {
	dir := t.TempDir()
	r := newRun(dir, "--version")
	r.front.Execer = execx.OS{}
	if code := r.start(t); code != term.ExitFailure {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(r.stderr.String(), filepath.Join(dir, BashName)) {
		t.Fatalf("stderr %q does not name the missing launcher", r.stderr)
	}
}

// F8: --help prints the help, then what claude --help says in the image, or why it cannot.
func TestHelp(t *testing.T) {
	dir := install(t)
	inspect := []string{"podman", "image", "inspect", "claude-yolo:latest"}
	claudeHelp := []string{"podman", "run", "--rm", "--entrypoint", "claude", "claude-yolo:latest", "--help"}
	cases := []struct {
		name  string
		setup func(f *execx.Fake)
		tail  string
	}{
		{"no image", func(f *execx.Fake) {
			f.On(inspect, execx.Result{ExitCode: 1})
		}, helpRule + "Claude Code Help:\n" + helpRule + "\n" +
			"Container image not built yet. Run 'ccy --rebuild' first to build the container,\n" +
			"then run 'ccy --help' again to see Claude Code options.\n"},
		{"the image's help", func(f *execx.Fake) {
			f.On(inspect, execx.Result{Stdout: []byte("[]\n")})
			f.On(claudeHelp, execx.Result{Stdout: []byte("Usage: claude\n\n\n")})
		}, helpRule + "Claude Code Help (from container):\n" + helpRule + "\nUsage: claude\n"},
		{"the image's help fails", func(f *execx.Fake) {
			f.On(inspect, execx.Result{})
			f.On(claudeHelp, execx.Result{ExitCode: 126, Stdout: []byte("Error: no such entrypoint\n")})
		}, helpRule + "Claude Code Help (from container):\n" + helpRule + "\n" +
			"Note: the image exists, but 'claude --help' would not run inside it.\n" +
			"  podman said: Error: no such entrypoint\n"},
		{"the image's help fails silently", func(f *execx.Fake) {
			f.On(inspect, execx.Result{})
			f.On(claudeHelp, execx.Result{ExitCode: 1})
		}, helpRule + "Claude Code Help (from container):\n" + helpRule + "\n" +
			"Note: the image exists, but 'claude --help' would not run inside it.\n" +
			"  podman said: no output\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, flag := range []string{"--help", "-h"} {
				r := newRun(dir, flag)
				c.setup(r.runner)
				if code := r.start(t); code != term.ExitOK {
					t.Fatalf("exit %d, stderr %q", code, r.stderr)
				}
				if want := helpText + c.tail; r.stdout.String() != want {
					t.Fatalf("stdout tail %q, want %q", strings.TrimPrefix(r.stdout.String(), helpText), c.tail)
				}
			}
		})
	}
}

// The engine the variable names is the one asked, and named when it fails.
func TestHelpAsksTheChosenEngine(t *testing.T) {
	dir := install(t)
	r := newRun(dir, "--help").env("CCY_CONTAINER_ENGINE=docker")
	r.runner.On([]string{"docker", "image", "inspect"}, execx.Result{})
	r.runner.On([]string{"docker", "run"}, execx.Result{ExitCode: 1, Stdout: []byte("boom")})
	if code := r.start(t); code != term.ExitOK || !strings.HasSuffix(r.stdout.String(), "  docker said: boom\n") {
		t.Fatalf("exit %d, stdout tail %q", code, strings.TrimPrefix(r.stdout.String(), helpText))
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// Output nobody could read is a failure.
func TestAnUnwritableStdoutFails(t *testing.T) {
	dir := install(t)
	for _, flag := range []string{"--version", "--help"} {
		r := newRun(dir, flag)
		r.runner.On([]string{"podman"}, execx.Result{ExitCode: 1})
		r.front.Stdout = failingWriter{}
		if code := r.start(t); code != term.ExitFailure {
			t.Fatalf("%s: exit %d, want 1", flag, code)
		}
	}
}
