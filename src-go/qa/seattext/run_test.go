package seattext

import (
	"bytes"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

type world struct {
	files     fstest.MapFS
	listErr   error
	out, err  bytes.Buffer
	published []string
}

func newWorld(files map[string]string) *world {
	w := &world{files: fstest.MapFS{}}
	for p, text := range files {
		w.files[p] = &fstest.MapFile{Data: []byte(text)}
		w.published = append(w.published, p)
	}
	if _, ok := files[allowedFile]; !ok {
		w.files[allowedFile] = &fstest.MapFile{Data: []byte("# none\n")}
	}
	return w
}

func (w *world) run(args ...string) ExitCode {
	w.out.Reset()
	w.err.Reset()
	env := Env{
		Stdout:  &w.out,
		Stderr:  &w.err,
		Cwd:     "/repo",
		Resolve: func(p string) (string, error) { return p, nil },
		Open:    func(string) fs.FS { return w.files },
		Published: func(string) ([]string, error) {
			return w.published, w.listErr
		},
	}
	return Main(args, env)
}

// S2: the usage text, and an unknown argument.
func TestTheCommandLine(t *testing.T) {
	w := newWorld(map[string]string{"docs/a.md": "x\n"})
	if code := w.run("--help"); code != ExitOK || !strings.Contains(w.out.String(), "usage: qa-seat-text") {
		t.Fatalf("--help: %d %q", code, w.out.String())
	}
	if code := w.run("--nope"); code != ExitCannotRun || !strings.Contains(w.err.String(), "usage: qa-seat-text") {
		t.Fatalf("--nope: %d %q", code, w.err.String())
	}
}

// S7: a clean tree passes with one line; a finding is reported and fails.
func TestACleanTreePassesAndAFindingFails(t *testing.T) {
	w := newWorld(map[string]string{
		"docs/a.md":                         "launch with `ccy --teams <team>`\n",
		"src-go/internal/teams/usage.txt":   "  ccy-teams seats\n",
		"files/var/local/claude-code/cc":    "echo ok >&2\n",
		"docs/ccy-changelog.md":             "ccy --teams 4@team-a\n",
		"src-go/internal/teams/seats/x.go":  "\"ccy --teams <seat>@\"\n",
		"files/var/local/other/ignored.txt": "--teams <seat>@x\n",
	})
	if code := w.run(); code != ExitOK {
		t.Fatalf("clean: exit %d %s", code, w.err.String())
	}
	if got := w.out.String(); got != "✓ seat-number-text: 3 file(s) ask a person for no seat number (0 allowed line(s))\n" {
		t.Fatalf("pass line %q", got)
	}
	w.files["docs/a.md"].Data = []byte("ok\nrun `ccy --teams 4@team-a`\n")
	if code := w.run(); code != ExitFailed {
		t.Fatalf("finding: exit %d", code)
	}
	if !strings.Contains(w.err.String(), "docs/a.md:2: "+FindingLaunch+": run `ccy --teams 4@team-a`") ||
		!strings.Contains(w.err.String(), "✗ seat-number-text: 1 finding(s) in 1 file(s)") {
		t.Fatalf("stderr %q", w.err.String())
	}
}

// S6: an allowed line passes; a stale row, and a row covering two lines, fail.
func TestTheAllowlist(t *testing.T) {
	w := newWorld(map[string]string{
		"docs/p.md": "seat retire SEAT@TEAM\n",
		allowedFile: "docs/p.md\tseat retire SEAT@TEAM\tthe bus's host verb, run by the play\n",
	})
	if code := w.run(); code != ExitOK {
		t.Fatalf("allowed: exit %d %s", code, w.err.String())
	}
	w.files["docs/p.md"].Data = []byte("nothing\n")
	if code := w.run(); code != ExitFailed || !strings.Contains(w.err.String(), "stale") {
		t.Fatalf("stale: exit %d %s", code, w.err.String())
	}
	w.files["docs/p.md"].Data = []byte("seat retire SEAT@TEAM\nseat retire SEAT@TEAM\n")
	if code := w.run(); code != ExitFailed || !strings.Contains(w.err.String(), "one row per line") {
		t.Fatalf("two lines: exit %d %s", code, w.err.String())
	}
}

// S7: runs that cannot judge.
func TestARunThatCannotJudge(t *testing.T) {
	w := newWorld(map[string]string{"docs/a.md": "x\n"})
	w.listErr = errors.New("git broke")
	if code := w.run(); code != ExitCannotRun {
		t.Fatalf("git failing: exit %d", code)
	}
	w = newWorld(map[string]string{"README.md": "x\n"})
	if code := w.run(); code != ExitCannotRun || !strings.Contains(w.err.String(), "no file") {
		t.Fatalf("empty scope: exit %d %s", code, w.err.String())
	}
	w = newWorld(map[string]string{"docs/a.md": "x\n", allowedFile: "only two\tfields\n"})
	if code := w.run(); code != ExitCannotRun {
		t.Fatalf("malformed allowlist: exit %d", code)
	}
	w = newWorld(map[string]string{"docs/a.md": "x\n"})
	w.published = append(w.published, "docs/gone.md")
	if code := w.run(); code != ExitCannotRun {
		t.Fatalf("unreadable file: exit %d", code)
	}
}
