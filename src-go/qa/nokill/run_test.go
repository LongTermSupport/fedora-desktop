package nokill

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	jsReport     = "extensions/fedora-desktop@fedora-desktop/containerReport.js"
	jsContainers = "extensions/fedora-desktop@fedora-desktop/sections/containers.js"
	containment  = "helpers/containerwatch/containment.py"
)

// world is a fake tree: files by path, the list git would publish, and the captured streams.
type world struct {
	files     map[string]string
	published []string
	gitErr    error
	out, err  strings.Builder
}

// cleanWorld has the watchdog's usual files, all clean.
func cleanWorld() *world {
	w := &world{files: map[string]string{
		"helpers/containerwatch/core.py": "def f():\n    return 1\n",
		containment:                      "def build_stop_argv(engine, cid):\n    return [engine, \"stop\", \"--time\", \"10\", cid]\n",
		jsReport:                         "function a() {}\n",
		jsContainers:                     "function b() {}\n",
	}}
	w.publishAll()
	return w
}

func (w *world) publishAll() {
	w.published = w.published[:0]
	for p := range w.files {
		w.published = append(w.published, p)
	}
	slices.Sort(w.published)
}

func (w *world) set(path, text string) {
	w.files[path] = text
	w.publishAll()
}

func (w *world) unpublish(path string) {
	w.published = slices.DeleteFunc(w.published, func(p string) bool { return p == path })
}

func (w *world) run(args ...string) ExitCode {
	return Main(args, w.env())
}

func (w *world) env() Env {
	mem := fstest.MapFS{}
	for p, text := range w.files {
		mem[p] = &fstest.MapFile{Data: []byte(text)}
	}
	return Env{
		Stdout:  &w.out,
		Stderr:  &w.err,
		Cwd:     "/repo",
		Resolve: func(p string) (string, error) { return p, nil },
		Open:    func(string) fs.FS { return mem },
		Published: func(string) ([]string, error) {
			return w.published, w.gitErr
		},
	}
}

func TestE1HelpUsageAndRoot(t *testing.T) {
	w := cleanWorld()
	if code := w.run("--help"); code != ExitOK || !strings.Contains(w.out.String(), "usage: qa-nokill-containerwatch") {
		t.Fatalf("--help: %d %q", code, w.out.String())
	}
	for _, args := range [][]string{{"--self-test"}, {"--nope"}, {"extra"}} {
		w := cleanWorld()
		code := w.run(args...)
		if code != ExitCannotRun || !strings.Contains(w.err.String(), "ERROR: unknown argument '"+args[0]+"'") ||
			!strings.Contains(w.err.String(), "usage:") || w.out.String() != "" {
			t.Fatalf("%v: %d out=%q err=%q", args, code, w.out.String(), w.err.String())
		}
	}
	for _, args := range [][]string{{"--root"}, {"--scripts"}, {"--root", "--scripts", "x"}} {
		w := cleanWorld()
		if code := w.run(args...); code != ExitCannotRun || !strings.Contains(w.err.String(), "ERROR: ") {
			t.Fatalf("%v: %d %q", args, code, w.err.String())
		}
	}
}

func TestE1RootFromScriptsParentAndRootFlag(t *testing.T) {
	var roots []string
	run := func(args ...string) {
		w := cleanWorld()
		env := w.env()
		env.Published = func(root string) ([]string, error) {
			roots = append(roots, root)
			return w.published, nil
		}
		if code := Main(args, env); code != ExitOK {
			t.Fatalf("%v: %d %s", args, code, w.err.String())
		}
	}
	run()
	run("--root", "/some/root")
	run("--scripts", "/some/root/scripts")
	want := []string{"/repo", "/some/root", "/some/root"}
	if !slices.Equal(roots, want) {
		t.Fatalf("roots %q want %q", roots, want)
	}
}

// failWriter fails every write.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestE2UnwritableStreamsAreExit2(t *testing.T) {
	w := cleanWorld()
	env := w.env()
	env.Stdout = failWriter{}
	if code := Main(nil, env); code != ExitCannotRun || !strings.Contains(w.err.String(), "ERROR: cannot write to stdout: disk full") {
		t.Fatalf("stdout: %d %q", code, w.err.String())
	}
	w = cleanWorld()
	w.set("helpers/containerwatch/bad.py", "os.kill(1, 2)\n")
	env = w.env()
	env.Stderr = failWriter{}
	if code := Main(nil, env); code != ExitCannotRun {
		t.Fatalf("stderr: %d", code)
	}
}

func TestE3GitFailureIsExit2(t *testing.T) {
	w := cleanWorld()
	w.gitErr = errors.New("not a git repository")
	code := w.run()
	if code != ExitCannotRun || !strings.Contains(w.err.String(), "ERROR: cannot list the files git would publish under /repo: not a git repository") {
		t.Fatalf("%d %q", code, w.err.String())
	}
}

func TestE3AGitignoredFileIsNeverRead(t *testing.T) {
	w := cleanWorld()
	w.files["helpers/containerwatch/scratch.py"] = "os.kill(1, 9)\n" // on disk, not published
	if code := w.run(); code != ExitOK {
		t.Fatalf("an unpublished file was judged: %d %q", code, w.err.String())
	}
	if !strings.Contains(w.out.String(), "4 container-watch file(s) clean") {
		t.Fatalf("it was counted: %q", w.out.String())
	}
}

func TestE4PythonTargetsAreRecursiveAndOnlyPy(t *testing.T) {
	w := cleanWorld()
	w.set("helpers/containerwatch/deep/er/mod.py", "child.kill()\n")
	w.set("helpers/containerwatch/notes.txt", "os.kill(1, 2)\n")
	w.set("helpers/containerwatch/script.pyc", "os.kill(1, 2)\n")
	w.set("helpers/other/x.py", "os.kill(1, 2)\n")
	code := w.run()
	if code != ExitFailed {
		t.Fatalf("nested module not judged: %d %q", code, w.out.String())
	}
	got := w.err.String()
	if !strings.Contains(got, "helpers/containerwatch/deep/er/mod.py:1: (proc-kill) child.kill()") {
		t.Fatalf("finding missing: %q", got)
	}
	if strings.Contains(got, "notes.txt") || strings.Contains(got, ".pyc") || strings.Contains(got, "helpers/other") {
		t.Fatalf("a file outside the targets was judged: %q", got)
	}
}

func TestE5MissingJavaScriptTargetsAreExit2(t *testing.T) {
	w := cleanWorld()
	delete(w.files, jsReport)
	delete(w.files, jsContainers)
	w.publishAll()
	code := w.run()
	got := w.err.String()
	if code != ExitCannotRun ||
		!strings.Contains(got, "ERROR: declared container-watch JavaScript target is missing: "+jsReport) ||
		!strings.Contains(got, "ERROR: declared container-watch JavaScript target is missing: "+jsContainers) ||
		!strings.Contains(got, "refusing to pass") || strings.Contains(w.out.String(), "clean") {
		t.Fatalf("%d out=%q err=%q", code, w.out.String(), got)
	}
}

func TestE5AGitignoredJavaScriptTargetCountsAsMissing(t *testing.T) {
	w := cleanWorld()
	w.unpublish(jsContainers)
	code := w.run()
	if code != ExitCannotRun || !strings.Contains(w.err.String(), "missing: "+jsContainers) {
		t.Fatalf("%d %q", code, w.err.String())
	}
}

func TestE5JavaScriptTargetsAreJudged(t *testing.T) {
	w := cleanWorld()
	w.set(jsContainers, "function reap(proc) {\n    proc.force_exit();\n}\n")
	code := w.run()
	if code != ExitFailed || !strings.Contains(w.err.String(), jsContainers+":2: (force-exit)     proc.force_exit();") {
		t.Fatalf("%d %q", code, w.err.String())
	}
}

func TestE6NoPythonIsExit2EvenWithJavaScript(t *testing.T) {
	w := cleanWorld()
	delete(w.files, "helpers/containerwatch/core.py")
	delete(w.files, containment)
	w.publishAll()
	code := w.run()
	if code != ExitCannotRun || !strings.Contains(w.err.String(), "ERROR: no Python file under helpers/containerwatch/") {
		t.Fatalf("%d %q", code, w.err.String())
	}
}

func TestE7UnreadableFileIsExit2(t *testing.T) {
	w := cleanWorld()
	w.published = append(w.published, "helpers/containerwatch/gone.py") // listed by git, not on disk
	code := w.run()
	if code != ExitCannotRun || !strings.Contains(w.err.String(), "ERROR: cannot read helpers/containerwatch/gone.py") ||
		strings.Contains(w.out.String(), "clean") {
		t.Fatalf("%d out=%q err=%q", code, w.out.String(), w.err.String())
	}
}

func TestE11TheExceptionIsExactlyOnePath(t *testing.T) {
	// containment.py may build a stop: clean (cleanWorld). A same-named file elsewhere may not.
	w := cleanWorld()
	w.set("helpers/containerwatch/sub/containment.py", "def f(cid):\n    return [\"podman\", \"stop\", cid]\n")
	code := w.run()
	if code != ExitFailed || !strings.Contains(w.err.String(), "sub/containment.py:2: (engine-literal)") {
		t.Fatalf("nested containment.py was exempt: %d %q", code, w.err.String())
	}
	// ...and the real one is held to the stricter list.
	w = cleanWorld()
	w.set(containment, "def f(engine, cid):\n    return [engine, \"rm\", \"--force\", cid]\n")
	code = w.run()
	got := w.err.String()
	if code != ExitFailed || !strings.Contains(got, containment+":2: (containment-verb)") {
		t.Fatalf("%d %q", code, got)
	}
}

func TestE11AbsentContainmentIsNotAnError(t *testing.T) {
	w := cleanWorld()
	delete(w.files, containment)
	w.publishAll()
	code := w.run()
	if code != ExitOK || strings.Contains(w.out.String(), "stop-only confirmed") || !strings.Contains(w.out.String(), "3 container-watch file(s) clean") {
		t.Fatalf("%d %q", code, w.out.String())
	}
}

func TestE13AllFindingsOfAllFilesAreReportedInPathOrder(t *testing.T) {
	w := cleanWorld()
	w.set(containment, "def f(engine, cid):\n    return [engine, \"rm\", cid]\n")
	w.set("helpers/containerwatch/a.py", "ok\nos.kill(1, 2)\n")
	w.set("helpers/containerwatch/z.py", "proc.terminate()\n")
	w.set(jsReport, "x.force_exit();\n")
	code := w.run()
	if code != ExitFailed || w.out.String() != "" {
		t.Fatalf("%d out=%q", code, w.out.String())
	}
	got := w.err.String()
	order := []string{
		jsReport + ":1: (force-exit) x.force_exit();",
		"helpers/containerwatch/a.py:2: (os-kill) os.kill(1, 2)",
		containment + ":2: (containment-verb)",
		"helpers/containerwatch/z.py:1: (proc-kill) proc.terminate()",
		"✗ no-kill gate: 4 forbidden call site(s) in 4 file(s)",
	}
	pos := 0
	for _, want := range order {
		i := strings.Index(got[pos:], want)
		if i < 0 {
			t.Fatalf("missing or out of order %q in\n%s", want, got)
		}
		pos += i + len(want)
	}
}

func TestE14ViolationEndsWithGuidance(t *testing.T) {
	w := cleanWorld()
	w.set("helpers/containerwatch/a.py", "os.kill(1, 2)\n")
	w.run()
	if !strings.Contains(w.err.String(), "only containment.py may ask") {
		t.Fatalf("%q", w.err.String())
	}
}

func TestE15CleanOutputCountsContainmentToo(t *testing.T) {
	w := cleanWorld()
	code := w.run()
	want := "✓ containment.py: stop-only confirmed (the one audited exception)\n" +
		"✓ no-kill gate: 4 container-watch file(s) clean — no process-termination call site\n"
	if code != ExitOK || w.out.String() != want || w.err.String() != "" {
		t.Fatalf("%d out=%q err=%q", code, w.out.String(), w.err.String())
	}
}
