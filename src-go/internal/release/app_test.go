package release_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/release"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// appGit adds the one file question the app asks beyond Decide's.
type appGit struct {
	*fakeGit

	files   map[string]string // "<ref>:<path>" to its text
	fileErr error
}

func (g *appGit) FileAtRef(_ context.Context, _, ref, path string) (string, bool, error) {
	if g.fileErr != nil {
		return "", false, g.fileErr
	}
	text, ok := g.files[ref+":"+path]
	return text, ok, nil
}

// memFiles is the working tree's changelog in memory.
type memFiles struct {
	text     string
	present  bool
	written  string
	wrote    bool
	readErr  error
	writeErr error
}

func (m *memFiles) Read(string) (string, bool, error) { return m.text, m.present, m.readErr }

func (m *memFiles) Write(_, data string) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.written, m.wrote = data, true
	return nil
}

type harness struct {
	out, errOut bytes.Buffer
	git         *appGit
	pulls       *fakePulls
	files       *memFiles
}

func newHarness() *harness {
	return &harness{
		git:   &appGit{fakeGit: &fakeGit{tags: []string{"44.0.0"}}, files: map[string]string{}},
		pulls: &fakePulls{},
		files: &memFiles{},
	}
}

func (h *harness) run(args ...string) term.ExitCode {
	app := release.App{
		Out:    &h.out,
		Err:    &h.errOut,
		Git:    h.git,
		Pulls:  func(string) release.Pulls { return h.pulls },
		Dir:    "/repo",
		Remote: "origin",
		Now:    func() time.Time { return time.Date(2026, 11, 4, 23, 59, 0, 0, time.FixedZone("far", 14*3600)) },
		Files:  h.files,
	}
	return app.Run(context.Background(), args)
}

func TestDecideAnswersAsKeyValueLinesForTheWorkflow(t *testing.T) {
	h := newHarness()
	h.git.log = []gitx.LogEntry{{SHA: sha1, Parents: 1, Message: "feat: a thing"}}
	if code := h.run("decide", "--branch", "F44"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.errOut.String())
	}
	want := "action=prepare\nversion=44.1.0\ncommit=\nreason=\n"
	if h.out.String() != want {
		t.Fatalf("got %q, want %q", h.out.String(), want)
	}
}

func TestDecideNamesTheCommitToTagAndWhyNothingIsDone(t *testing.T) {
	h := newHarness()
	h.git.subjects = []gitx.Subject{{SHA: sha2, Subject: "Release 44.1.0"}}
	h.pulls.merged = map[string]bool{sha2: true}
	h.run("decide", "--branch", "F44")
	if want := "action=tag\nversion=44.1.0\ncommit=" + sha2 + "\nreason=\n"; h.out.String() != want {
		t.Fatalf("got %q, want %q", h.out.String(), want)
	}

	h = newHarness()
	h.git.log = []gitx.LogEntry{{SHA: sha1, Parents: 1, Message: "docs: words"}}
	h.run("decide", "--branch", "F44")
	if !strings.HasPrefix(h.out.String(), "action=none\nversion=\ncommit=\nreason=no releasable change") {
		t.Fatalf("got %q", h.out.String())
	}
	if strings.Count(h.out.String(), "\n") != 4 {
		t.Fatalf("every value must stay on its own line: %q", h.out.String())
	}
}

// A refusal is an exit and words on stderr, never a line that could read as an answer.
func TestDecideRefusesABreakingChangeAndAFailedQuestion(t *testing.T) {
	h := newHarness()
	h.git.log = []gitx.LogEntry{{SHA: sha1, Parents: 1, Message: "feat!: a break"}}
	if code := h.run("decide", "--branch", "F44"); code != term.ExitFailure {
		t.Fatalf("exit %d, want a refusal", code)
	}
	if h.out.Len() != 0 || !strings.Contains(h.errOut.String(), "breaking") {
		t.Fatalf("stdout %q, stderr %q", h.out.String(), h.errOut.String())
	}

	h = newHarness()
	h.git.tagsErr = errors.New("boom")
	if code := h.run("decide", "--branch", "F44"); code != term.ExitFailure || h.out.Len() != 0 {
		t.Fatalf("exit %d, stdout %q", code, h.out.String())
	}
}

func TestUsageErrors(t *testing.T) {
	cases := [][]string{
		nil,
		{"frobnicate"},
		{"decide"},
		{"decide", "--branch"},
		{"decide", "--branch", "F44", "--extra", "x"},
		{"decide", "stray", "--branch", "F44"},
		{"prepare", "--branch", "F44"},
		{"prepare", "--version", "44.1.0"},
		{"notes", "--version", "44.1.0"},
		{"notes", "--commit", sha1},
	}
	for _, args := range cases {
		h := newHarness()
		if code := h.run(args...); code != term.ExitUsage {
			t.Fatalf("%q: exit %d, want usage", args, code)
		}
		if h.out.Len() != 0 || !strings.Contains(h.errOut.String(), "usage") {
			t.Fatalf("%q: stdout %q, stderr %q", args, h.out.String(), h.errOut.String())
		}
	}
}

func TestPrepareWritesTheEntryWithTodaysUTCDate(t *testing.T) {
	h := newHarness()
	h.git.log = []gitx.LogEntry{{SHA: sha1, Parents: 1, Message: "fix: one"}, {SHA: sha2, Parents: 1, Message: "feat: two"}}
	h.files.present, h.files.text = true, "# Changelog\n\n"+olderEntry
	if code := h.run("prepare", "--branch", "F44", "--version", "44.1.0"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.errOut.String())
	}
	want := "# Changelog\n\n## 44.1.0 - 2026-11-04\n\n- feat: two\n- fix: one\n\n" + olderEntry
	if h.files.written != want {
		t.Fatalf("wrote %q, want %q", h.files.written, want)
	}
}

func TestPrepareCreatesTheChangelogWhenThereIsNone(t *testing.T) {
	h := newHarness()
	h.git.log = []gitx.LogEntry{{SHA: sha1, Parents: 1, Message: "feat: two"}}
	if code := h.run("prepare", "--branch", "F44", "--version", "44.1.0"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.errOut.String())
	}
	if !strings.HasPrefix(h.files.written, "# Changelog\n\n## 44.1.0 - 2026-11-04\n") {
		t.Fatalf("wrote %q", h.files.written)
	}
}

// What the version step saw and what the release job sees must agree: if the branch moved in
// between, nothing is written.
func TestPrepareRefusesWhatTheBranchNoLongerCallsFor(t *testing.T) {
	cases := map[string]struct {
		mutate  func(*harness)
		version string
	}{
		"another version":    {func(h *harness) { h.git.log = []gitx.LogEntry{{SHA: sha1, Parents: 1, Message: "fix: a"}} }, "44.1.0"},
		"nothing to release": {func(*harness) {}, "44.1.0"},
		"a bad version":      {func(h *harness) { h.git.log = []gitx.LogEntry{{SHA: sha1, Parents: 1, Message: "feat: a"}} }, "44.1"},
		"a tag to make": {func(h *harness) {
			h.git.subjects = []gitx.Subject{{SHA: sha2, Subject: "Release 44.1.0"}}
		}, "44.1.0"},
	}
	for name, c := range cases {
		h := newHarness()
		c.mutate(h)
		code := h.run("prepare", "--branch", "F44", "--version", c.version)
		if code == term.ExitOK || h.files.wrote {
			t.Fatalf("%s: exit %d, wrote %v", name, code, h.files.wrote)
		}
	}
}

func TestPrepareFileFailuresAreErrors(t *testing.T) {
	h := newHarness()
	h.git.log = []gitx.LogEntry{{SHA: sha1, Parents: 1, Message: "feat: a"}}
	h.files.readErr = errors.New("unreadable")
	if code := h.run("prepare", "--branch", "F44", "--version", "44.1.0"); code != term.ExitFailure {
		t.Fatalf("read failure: exit %d", code)
	}
	h = newHarness()
	h.git.log = []gitx.LogEntry{{SHA: sha1, Parents: 1, Message: "feat: a"}}
	h.files.writeErr = errors.New("read-only")
	if code := h.run("prepare", "--branch", "F44", "--version", "44.1.0"); code != term.ExitFailure {
		t.Fatalf("write failure: exit %d", code)
	}
}

func TestNotesAreTheEntryAtTheReleaseCommit(t *testing.T) {
	h := newHarness()
	h.git.files[sha2+":CHANGELOG.md"] = "# Changelog\n\n## 44.1.0 - 2026-11-04\n\n- feat: a thing\n\n" + olderEntry
	if code := h.run("notes", "--version", "44.1.0", "--commit", sha2); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.errOut.String())
	}
	if h.out.String() != "- feat: a thing\n" {
		t.Fatalf("got %q", h.out.String())
	}
}

func TestNotesRefuseWhatCannotBePublished(t *testing.T) {
	cases := map[string]struct {
		prepare func(*harness)
		args    []string
	}{
		"no changelog at the commit": {func(*harness) {}, []string{"--version", "44.1.0", "--commit", sha2}},
		"no entry for the version": {func(h *harness) {
			h.git.files[sha2+":CHANGELOG.md"] = "# Changelog\n\n" + olderEntry
		}, []string{"--version", "44.1.0", "--commit", sha2}},
		"git failing":                {func(h *harness) { h.git.fileErr = errors.New("boom") }, []string{"--version", "44.1.0", "--commit", sha2}},
		"a commit that is an option": {func(*harness) {}, []string{"--version", "44.1.0", "--commit", "--output=x"}},
		"a commit that is a branch":  {func(*harness) {}, []string{"--version", "44.1.0", "--commit", "F44"}},
	}
	for name, c := range cases {
		h := newHarness()
		c.prepare(h)
		code := h.run(append([]string{"notes"}, c.args...)...)
		if code == term.ExitOK || h.out.Len() != 0 {
			t.Fatalf("%s: exit %d, stdout %q", name, code, h.out.String())
		}
	}
}
