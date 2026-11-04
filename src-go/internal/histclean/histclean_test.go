package histclean

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const historyText = "#1700000001\nls -la\n" +
	"#1700000002\necho hunter2 > note\n" +
	"#1700000003\nfor i in 1 2; do\n  echo secret-$i\ndone\n" +
	"#1700000004\ngit status\n"

// contextText is the recorder's NUL-terminated "epoch TAB status TAB dir TAB command" records.
const contextText = "1700000001\t0\t/home/u\tls -la\x00" +
	"1700000002\t0\t/srv/hunter2\techo ok\x00" +
	"1700000002\t0\t/home/u\techo hunter2 > note\x00" +
	"1700000003\t1\t/home/u\tfor i in 1 2; do\n  echo secret-$i\ndone\x00"

type world struct {
	dir              string
	history, context string
	stdout, stderr   bytes.Buffer
	interactive      bool
	stdin            string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	dir := t.TempDir()
	w := &world{dir: dir, history: filepath.Join(dir, "history"), context: filepath.Join(dir, "context"), interactive: true}
	write(t, w.history, historyText)
	write(t, w.context, contextText)
	return w
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (w *world) run(args ...string) term.ExitCode {
	app := App{
		HistoryPath: w.history, ContextPath: w.context,
		Stdin: strings.NewReader(w.stdin), Interactive: w.interactive,
		Stdout: &w.stdout, Stderr: &w.stderr,
	}
	return app.Run(context.Background(), args)
}

// H1: a fixed substring matches history entries and context records; yes removes them from both.
func TestYesRemovesMatchesFromBothFiles(t *testing.T) {
	w := newWorld(t)
	w.stdin = "y\n"
	if code := w.run("hunter2"); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
	wantHist := strings.Replace(historyText, "#1700000002\necho hunter2 > note\n", "", 1)
	if got := read(t, w.history); got != wantHist {
		t.Errorf("history = %q, want %q", got, wantHist)
	}
	// The directory is not the command: a record whose only match is its directory stays.
	wantCtx := strings.Replace(contextText, "1700000002\t0\t/home/u\techo hunter2 > note\x00", "", 1)
	if got := read(t, w.context); got != wantCtx {
		t.Errorf("context = %q, want %q", got, wantCtx)
	}
	if !strings.Contains(w.stderr.String(), "Remove 2 entries? [y/N]") {
		t.Errorf("no question in %q", w.stderr.String())
	}
	if !strings.Contains(w.stderr.String(), "history -d") {
		t.Errorf("no in-memory reminder in %q", w.stderr.String())
	}
	if w.stdout.Len() != 0 {
		t.Errorf("stdout must be empty, got %q", w.stdout.String())
	}
}

// H2: a multi-line history entry and context record go whole, epoch line included.
func TestMultiLineEntryGoesWhole(t *testing.T) {
	w := newWorld(t)
	w.stdin = "y\n"
	if code := w.run("secret-"); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if got, want := read(t, w.history), "#1700000001\nls -la\n#1700000002\necho hunter2 > note\n#1700000004\ngit status\n"; got != want {
		t.Errorf("history = %q, want %q", got, want)
	}
	if strings.Contains(read(t, w.context), "secret-") {
		t.Errorf("context still holds the entry")
	}
}

// H3: the entries are shown, numbered, before the question, with a multi-line one in full.
func TestEntriesShownNumbered(t *testing.T) {
	w := newWorld(t)
	w.stdin = "n\n"
	w.run("secret-")
	out := w.stderr.String()
	for _, want := range []string{"1.", "2.", "echo secret-$i", "done"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from %q", want, out)
		}
	}
	if strings.Index(out, "echo secret-$i") > strings.Index(out, "Remove 2 entries?") {
		t.Errorf("entries must come before the question")
	}
}

// H4: no, Enter and end of input leave both files byte for byte as they were.
func TestDeclineChangesNothing(t *testing.T) {
	for _, in := range []string{"n\n", "\n", ""} {
		w := newWorld(t)
		w.stdin = in
		code := w.run("hunter2")
		if in == "" && code != term.ExitCancelled {
			t.Errorf("input ended: exit %d, want cancelled", code)
		}
		if in != "" && code != term.ExitOK {
			t.Errorf("%q: exit %d", in, code)
		}
		if read(t, w.history) != historyText || read(t, w.context) != contextText {
			t.Errorf("%q: files changed", in)
		}
	}
}

// H5: a typo is asked again; three bad answers give up with nothing removed.
func TestTypoReprompts(t *testing.T) {
	w := newWorld(t)
	w.stdin = "yse\ny\n"
	if code := w.run("hunter2"); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(read(t, w.history), "hunter2") {
		t.Errorf("not removed after the retry")
	}
	w = newWorld(t)
	w.stdin = "x\nx\nx\ny\n"
	if code := w.run("hunter2"); code != term.ExitFailure {
		t.Errorf("gave up: exit %d, want failure", code)
	}
	if read(t, w.history) != historyText {
		t.Errorf("history changed after giving up")
	}
}

// H6: no match says so and exits 0 without asking.
func TestNoMatch(t *testing.T) {
	w := newWorld(t)
	if code := w.run("absent-text"); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stderr.String(), "No entries contain") || strings.Contains(w.stderr.String(), "[y/N]") {
		t.Errorf("stderr = %q", w.stderr.String())
	}
}

// H7: without a terminal and without --yes nothing is removed.
func TestNonTerminalRefused(t *testing.T) {
	w := newWorld(t)
	w.interactive = false
	w.stdin = "y\n"
	if code := w.run("hunter2"); code != term.ExitFailure {
		t.Errorf("exit %d, want failure", code)
	}
	if !strings.Contains(w.stderr.String(), "--yes") {
		t.Errorf("error does not name --yes: %q", w.stderr.String())
	}
	if read(t, w.history) != historyText {
		t.Errorf("history changed")
	}
}

// H8: --yes removes without a prompt, also off a terminal.
func TestYesFlag(t *testing.T) {
	w := newWorld(t)
	w.interactive = false
	if code := w.run("--yes", "hunter2"); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr.String())
	}
	if strings.Contains(read(t, w.history), "hunter2") || strings.Contains(w.stderr.String(), "[y/N]") {
		t.Errorf("not removed silently: %q", w.stderr.String())
	}
}

// H9: -e takes a Go regexp; the default is a fixed string, so metacharacters are literal.
func TestRegexpAndFixed(t *testing.T) {
	w := newWorld(t)
	w.stdin = "y\n"
	if code := w.run("-e", `secret-\$i|git st`); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if got := read(t, w.history); strings.Contains(got, "git status") || strings.Contains(got, "secret-") {
		t.Errorf("regexp did not match: %q", got)
	}
	w = newWorld(t)
	w.stdin = "y\n"
	w.run(".*")
	if read(t, w.history) != historyText {
		t.Errorf("fixed pattern .* must be literal")
	}
	w = newWorld(t)
	if code := w.run("-e", "("); code != term.ExitUsage {
		t.Errorf("bad regexp: exit %d, want usage", code)
	}
}

// H10: usage errors and help.
func TestUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"a", "b"}, {"--nope", "x"}, {""}} {
		w := newWorld(t)
		if code := w.run(args...); code != term.ExitUsage {
			t.Errorf("%q: exit %d, want usage", args, code)
		}
	}
	w := newWorld(t)
	if code := w.run("--help"); code != term.ExitOK {
		t.Errorf("help: exit %d", code)
	}
	if !strings.Contains(w.stdout.String(), "fixed substring") {
		t.Errorf("help does not say the pattern is a fixed substring: %q", w.stdout.String())
	}
	w = newWorld(t)
	w.stdin = "n\n"
	if code := w.run("--", "-e"); code != term.ExitOK {
		t.Errorf("-- ends options: exit %d", code)
	}
}

// H11: a file that is not there is skipped and said so; neither there is an error.
func TestMissingFiles(t *testing.T) {
	w := newWorld(t)
	if err := os.Remove(w.context); err != nil {
		t.Fatal(err)
	}
	w.stdin = "y\n"
	if code := w.run("hunter2"); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(w.context); err == nil {
		t.Errorf("a missing file must not be created")
	}
	if err := os.Remove(w.history); err != nil {
		t.Fatal(err)
	}
	if code := w.run("hunter2"); code != term.ExitFailure {
		t.Errorf("both missing: exit %d, want failure", code)
	}
}

// H12: the rewrite keeps mode 0600, leaves no temporary behind and replaces by rename.
func TestRewriteIsAtomicAndPrivate(t *testing.T) {
	w := newWorld(t)
	if err := os.Chmod(w.history, 0o400); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(w.history)
	if err != nil {
		t.Fatal(err)
	}
	w.stdin = "y\n"
	w.run("hunter2")
	after, err := os.Stat(w.history)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", after.Mode().Perm())
	}
	if os.SameFile(before, after) {
		t.Errorf("the file was rewritten in place, not replaced")
	}
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("leftover files in %s: %v", w.dir, entries)
	}
}

// H13: lines before the first timestamp are one entry each; a file with no trailing newline survives.
func TestUnstampedLinesAndNoFinalNewline(t *testing.T) {
	w := newWorld(t)
	write(t, w.history, "old one\nold hunter2\nold three\n#1700000001\nkept")
	write(t, w.context, "")
	w.stdin = "y\n"
	if code := w.run("hunter2"); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if got, want := read(t, w.history), "old one\nold three\n#1700000001\nkept"; got != want {
		t.Errorf("history = %q, want %q", got, want)
	}
}

// H14: an entry appended between the scan and the rewrite is not lost.
func TestAppendedDuringPromptSurvives(t *testing.T) {
	w := newWorld(t)
	app := App{
		HistoryPath: w.history, ContextPath: w.context, Interactive: true,
		Stdin:  &appendingReader{path: w.history, then: "y\n", add: "#1700000009\nnewest\n"},
		Stdout: &w.stdout, Stderr: &w.stderr,
	}
	if code := app.Run(context.Background(), []string{"hunter2"}); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	got := read(t, w.history)
	if !strings.HasSuffix(got, "#1700000009\nnewest\n") || strings.Contains(got, "hunter2") {
		t.Errorf("history = %q", got)
	}
}

// appendingReader answers the prompt, and first appends to the file as another shell would.
type appendingReader struct {
	path, then, add string
	done            bool
}

func (r *appendingReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, os.ErrClosed
	}
	r.done = true
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	if _, err := f.WriteString(r.add); err != nil {
		return 0, errJoin(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	return copy(p, r.then), nil
}

func errJoin(a, b error) error {
	if b != nil {
		return b
	}
	return a
}

// H15: stderr that cannot be written is a failure, not a quiet success.
func TestUnwritableStderrIsAFailure(t *testing.T) {
	w := newWorld(t)
	app := App{HistoryPath: w.history, ContextPath: w.context, Stdin: strings.NewReader(""), Stdout: &w.stdout, Stderr: failingWriter{}}
	if code := app.Run(context.Background(), []string{"absent-text"}); code != term.ExitFailure {
		t.Errorf("exit %d, want failure", code)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

// H16: a history path that cannot be read is an error naming it, and nothing is removed.
func TestUnreadableFileIsAnError(t *testing.T) {
	w := newWorld(t)
	w.history = w.dir // a directory
	if code := w.run("hunter2"); code != term.ExitFailure {
		t.Errorf("exit %d, want failure", code)
	}
	if !strings.Contains(w.stderr.String(), "cannot read") {
		t.Errorf("stderr = %q", w.stderr.String())
	}
}

// H17: a long list is cut at a thousand lines, but the question counts them all.
func TestLongListIsCut(t *testing.T) {
	w := newWorld(t)
	var b strings.Builder
	for range 1200 {
		b.WriteString("#1700000000\nbulk-line\n")
	}
	write(t, w.history, b.String())
	write(t, w.context, "")
	w.stdin = "n\n"
	w.run("bulk-line")
	if !strings.Contains(w.stderr.String(), "and 200 more") || !strings.Contains(w.stderr.String(), "Remove 1200 entries?") {
		t.Errorf("stderr lacks the cut or the count")
	}
}

// H18: a line that only looks like a timestamp is command text.
func TestNotATimestamp(t *testing.T) {
	got := parseHistory("#1700000001\n#12ab567890x\nreal\n")
	if len(got) != 1 || got[0].command != "#12ab567890x\nreal" {
		t.Errorf("entries = %+v", got)
	}
}

// H19: a replacement that cannot be made is an error and the original stays.
func TestReplaceFileFailure(t *testing.T) {
	if err := replaceFile(filepath.Join(t.TempDir(), "no-such-dir", "f"), []byte("x")); err == nil {
		t.Error("want an error")
	}
}

// H20: a file removed while the question was open is an error.
func TestFileVanishesDuringPrompt(t *testing.T) {
	w := newWorld(t)
	f := &fileScan{
		name: "history", path: filepath.Join(w.dir, "gone"), exists: true, parse: parseHistory,
		hits: []chunk{{raw: "#1700000001\nx\n", command: "x"}},
	}
	if _, err := rewrite(f, options{match: func(string) bool { return true }}); err == nil {
		t.Error("want an error")
	}
}

// H21: a reader that fails is an error, not a decline.
func TestPromptReadError(t *testing.T) {
	w := newWorld(t)
	app := App{
		HistoryPath: w.history, ContextPath: w.context, Interactive: true,
		Stdin: errReader{}, Stdout: &w.stdout, Stderr: &w.stderr,
	}
	if code := app.Run(context.Background(), []string{"hunter2"}); code == term.ExitOK {
		t.Errorf("exit %d, want non-zero", code)
	}
	if read(t, w.history) != historyText {
		t.Errorf("history changed")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, os.ErrPermission }
