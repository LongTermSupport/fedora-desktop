package lxcattach

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// world is a fake tree, its tracked list and the streams of one run.
type world struct {
	mem        fstest.MapFS
	tracked    []string
	trackedErr error
	out, err   strings.Builder
}

func fixtureText(t *testing.T) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("..", "..", "..", FixturePath))
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

// newWorld holds the real fixture and the scripts, every one tracked.
func newWorld(t *testing.T, scripts map[string]string) *world {
	t.Helper()
	w := &world{mem: fstest.MapFS{FixturePath: &fstest.MapFile{Data: []byte(fixtureText(t))}}}
	for p, text := range scripts {
		w.mem[p] = &fstest.MapFile{Data: []byte(text)}
		w.tracked = append(w.tracked, p)
	}
	return w
}

func (w *world) run(args ...string) ExitCode {
	env := Env{
		Stdout:  &w.out,
		Stderr:  &w.err,
		Cwd:     "/repo",
		Resolve: func(p string) (string, error) { return p, nil },
		Open:    func(string) fs.FS { return w.mem },
		Tracked: func(root string) ([]string, error) {
			if root != "/repo" {
				return nil, errors.New("wrong root " + root)
			}
			return w.tracked, w.trackedErr
		},
	}
	return Main(args, env)
}

func expect(t *testing.T, w *world, want ExitCode, args ...string) {
	t.Helper()
	if got := w.run(args...); got != want {
		t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, w.out.String(), w.err.String())
	}
}

func contains(t *testing.T, stream, want string) {
	t.Helper()
	if !strings.Contains(stream, want) {
		t.Errorf("%q not in:\n%s", want, stream)
	}
}

const (
	lib = "CLAUDE/Plan/00001-x/acceptance.bash"
	inc = "CLAUDE/Plan/00001-x/_stage.inc.bash"
)

func TestACleanTreePassesWithItsCount(t *testing.T) {
	w := newWorld(t, map[string]string{lib: "#!/usr/bin/env bash\nLXC=(sudo lxc-attach)\n", "scripts/a.bash": "true >x\n"})
	expect(t, w, ExitOK)
	if w.out.String() != "LXC-ATTACH-STREAMS-OK: 2 shell script(s) examined, no lxc-attach stream on a file\n" || w.err.String() != "" {
		t.Fatalf("%q %q", w.out.String(), w.err.String())
	}
}

// The originating shape: a wrapper defined in one script of a plan, called in another with
// stderr on a file.
func TestAFindingAcrossScriptsFailsNamingTheRule(t *testing.T) {
	w := newWorld(t, map[string]string{
		lib: "#!/usr/bin/env bash\nLXC=(sudo -n lxc-attach)\nin_guest() { \"${LXC[@]}\" -n \"$1\" -- \"${@:2}\"; }\n",
		inc: "session_run() { in_guest c \"$@\"; }\nask() {\n  out=\"$(session_run pingbus ask-human 2>\"$err\")\" || rc=$?\n}\n",
	})
	expect(t, w, ExitFailed)
	if w.out.String() != "" {
		t.Fatalf("stdout %q", w.out.String())
	}
	contains(t, w.err.String(), "ERROR: 1 lxc-attach stream(s) on a file across 2 shell script(s) examined:")
	contains(t, w.err.String(), "  "+inc+":3: lxc-attach-file-stream: stderr is a file (redirected on line 3), and lxc-attach makes it root's")
	contains(t, w.err.String(), `CLAUDE/QA.md "lxc-attach-file-stream"`)
}

func TestCompletedPlansAreFrozenHistoryAndNotJudged(t *testing.T) {
	w := newWorld(t, map[string]string{
		"CLAUDE/Plan/Completed/00002-y/a.bash": "lxc-attach -n c -- true 2>e\n",
		"scripts/a.bash":                       "true\n",
	})
	expect(t, w, ExitOK)
	contains(t, w.out.String(), "1 shell script(s) examined")
}

func TestNoScriptToJudgeCannotPass(t *testing.T) {
	w := newWorld(t, nil)
	expect(t, w, ExitCannotRun)
	contains(t, w.err.String(), "ERROR: no tracked shell script to judge; this gate would certify nothing")
}

func TestATrackedListThatFailsCannotRun(t *testing.T) {
	w := newWorld(t, map[string]string{"a.bash": "true\n"})
	w.trackedErr = errors.New("git broke")
	expect(t, w, ExitCannotRun)
	contains(t, w.err.String(), "ERROR: git broke")
}

func TestAnUnreadableOrUnparsableScriptCannotRun(t *testing.T) {
	w := newWorld(t, map[string]string{"a.bash": "if then (\n"})
	expect(t, w, ExitCannotRun)
	contains(t, w.err.String(), "ERROR: cannot parse a.bash")
	w = newWorld(t, nil)
	w.tracked = []string{"gone.bash"}
	expect(t, w, ExitCannotRun)
	contains(t, w.err.String(), "ERROR: cannot read gone.bash")
}

// A rule that stops matching its fixture is a failure, never a clean pass.
func TestTheFixtureIsProvenOnEveryRun(t *testing.T) {
	w := newWorld(t, map[string]string{"a.bash": "true\n"})
	w.mem[FixturePath] = &fstest.MapFile{Data: []byte("#!/usr/bin/env bash\n# ruleid: lxc-attach-file-stream\ntrue\n")}
	expect(t, w, ExitFailed)
	contains(t, w.err.String(), "ERROR: the rule failed its fixture "+FixturePath+": the fixture marks lines [3], the rule reports []")
	w = newWorld(t, map[string]string{"a.bash": "true\n"})
	w.mem[FixturePath] = &fstest.MapFile{Data: []byte("#!/usr/bin/env bash\ntrue\n")}
	expect(t, w, ExitFailed)
	contains(t, w.err.String(), "the fixture marks lines [], the rule reports []")
	w = newWorld(t, map[string]string{"a.bash": "true\n"})
	delete(w.mem, FixturePath)
	expect(t, w, ExitCannotRun)
	contains(t, w.err.String(), "ERROR: cannot read "+FixturePath)
	w = newWorld(t, map[string]string{"a.bash": "true\n"})
	w.mem[FixturePath] = &fstest.MapFile{Data: []byte("if then (\n")}
	expect(t, w, ExitCannotRun)
	contains(t, w.err.String(), "ERROR: the rule cannot read its fixture")
}

func TestUsage(t *testing.T) {
	w := newWorld(t, nil)
	expect(t, w, ExitOK, "--help")
	contains(t, w.out.String(), "usage: qa-lxc-attach-streams")
	w = newWorld(t, nil)
	expect(t, w, ExitCannotRun, "--nope")
	contains(t, w.err.String(), "ERROR: unknown argument")
	contains(t, w.err.String(), "usage: qa-lxc-attach-streams")
	w = newWorld(t, nil)
	expect(t, w, ExitCannotRun, "--root")
	contains(t, w.err.String(), "ERROR: ")
}

func TestARootThatCannotResolveCannotRun(t *testing.T) {
	w := newWorld(t, map[string]string{"a.bash": "true\n"})
	var out, errOut strings.Builder
	env := Env{
		Stdout: &out, Stderr: &errOut, Cwd: "/repo",
		Resolve: func(string) (string, error) { return "", errors.New("no such root") },
		Open:    func(string) fs.FS { return w.mem },
		Tracked: func(string) ([]string, error) { return w.tracked, nil },
	}
	if code := Main(nil, env); code != ExitCannotRun || !strings.Contains(errOut.String(), "no such root") {
		t.Fatalf("exit %d %q", code, errOut.String())
	}
}
