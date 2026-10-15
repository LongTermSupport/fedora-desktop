package planscriptlogging

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// world is a fake tree and the streams of one run.
type world struct {
	mem      fstest.MapFS
	out, err strings.Builder
}

func newWorld(files map[string]string) *world {
	w := &world{mem: fstest.MapFS{}}
	for p, text := range files {
		w.mem[p] = &fstest.MapFile{Data: []byte(text)}
	}
	return w
}

func (w *world) env() Env {
	return Env{
		Stdout:  &w.out,
		Stderr:  &w.err,
		Cwd:     "/repo",
		Resolve: func(p string) (string, error) { return p, nil },
		Open:    func(string) fs.FS { return w.mem },
	}
}

func (w *world) run(args ...string) ExitCode { return Main(args, w.env()) }

// planScript is the files of a world holding one script in an active plan.
func planScript(body string) map[string]string {
	return map[string]string{"CLAUDE/Plan/00001-x/run.bash": "#!/usr/bin/env bash\n" + body}
}

func expectExit(t *testing.T, name string, files map[string]string, want ExitCode) *world {
	t.Helper()
	w := newWorld(files)
	if got := w.run(); got != want {
		t.Fatalf("%s: exit %d, want %d\nstdout:\n%s\nstderr:\n%s", name, got, want, w.out.String(), w.err.String())
	}
	return w
}

func mustContain(t *testing.T, name, stream, want string) {
	t.Helper()
	if !strings.Contains(stream, want) {
		t.Errorf("%s: %q not in:\n%s", name, want, stream)
	}
}

// unreadableFS refuses to open one path.
type unreadableFS struct {
	fs.FS

	bad string
}

func (u unreadableFS) Open(name string) (fs.File, error) {
	if name == u.bad {
		return nil, fs.ErrPermission
	}
	return u.FS.Open(name)
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed pipe") }
