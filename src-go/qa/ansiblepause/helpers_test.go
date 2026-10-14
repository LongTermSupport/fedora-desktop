package ansiblepause

import (
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// world is a fake tree: files by path, the YAML list the discovery would answer, and the streams.
type world struct {
	files    map[string]string
	yaml     []string
	yamlErr  error
	out, err strings.Builder
}

// newWorld holds the given files, all listed as discovered YAML.
func newWorld(files map[string]string) *world {
	w := &world{files: files}
	for p := range files {
		w.yaml = append(w.yaml, p)
	}
	slices.Sort(w.yaml)
	return w
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
		Yaml:    func(string) ([]string, error) { return w.yaml, w.yamlErr },
	}
}

func (w *world) run(args ...string) ExitCode { return Main(args, w.env()) }

// findingsOf is the lines Scan reports for one text.
func findingsOf(text string) []int {
	found := Scan(text)
	lines := make([]int, 0, len(found))
	for _, f := range found {
		lines = append(lines, f.Line)
	}
	return lines
}

func expectLines(t *testing.T, name, text string, want ...int) {
	t.Helper()
	got := findingsOf(text)
	if !slices.Equal(got, want) {
		t.Errorf("%s: findings at lines %v, want %v\n%s", name, got, want, text)
	}
}

func joinLines(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed pipe") }

func symlink(target, link string) error { return os.Symlink(target, link) }
