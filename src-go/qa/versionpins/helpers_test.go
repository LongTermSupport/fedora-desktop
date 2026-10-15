package versionpins

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// trackedRow is a valid pin whose install state is compared against the host.
const trackedRow = `  - playbook: playbooks/a.yml
    var: a_version
    github: o/r
    installed:
      kind: command
      name: a
`

// untrackedRow is a valid pin declared untracked.
func untrackedRow(playbook, name string) string {
	return "  - playbook: " + playbook + "\n    var: " + name + "\n    github: o/" + name +
		"\n    installed:\n      kind: untracked\n      why: not established\n"
}

// manifestOf is a manifest holding the rows.
func manifestOf(rows ...string) string { return "version_pins:\n" + strings.Join(rows, "") }

// goodFiles is a tree the gate passes: one tracked pin and its playbook.
func goodFiles() map[string]string {
	return map[string]string{
		manifestPath:      manifestOf(trackedRow),
		"playbooks/a.yml": "---\na_version: \"1.0\"\n",
	}
}

// world is a fake tree and the streams of one run.
type world struct {
	mem      fstest.MapFS
	out, err strings.Builder
	open     func(fs.FS) fs.FS
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
		Open: func(string) fs.FS {
			if w.open != nil {
				return w.open(w.mem)
			}
			return w.mem
		},
	}
}

func (w *world) run(args ...string) ExitCode { return Main(args, w.env()) }

// expectExit runs the gate over the files and fails unless it exits with want.
func expectExit(t *testing.T, name string, files map[string]string, want ExitCode) *world {
	t.Helper()
	w := newWorld(files)
	if got := w.run(); got != want {
		t.Fatalf("%s: exit %d, want %d\nstdout:\n%s\nstderr:\n%s", name, got, want, w.out.String(), w.err.String())
	}
	return w
}

// withManifest is the good tree with a different manifest.
func withManifest(text string) map[string]string {
	files := goodFiles()
	files[manifestPath] = text
	return files
}

// withPlaybook is the good tree with a different a.yml.
func withPlaybook(text string) map[string]string {
	files := goodFiles()
	files["playbooks/a.yml"] = text
	return files
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

// countingFS counts the reads of each path.
type countingFS struct {
	fs.FS

	reads map[string]int
}

func (c *countingFS) ReadFile(name string) ([]byte, error) {
	c.reads[name]++
	return fs.ReadFile(c.FS, name)
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed pipe") }
