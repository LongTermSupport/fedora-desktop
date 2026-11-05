package statusline

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// S28: COLUMNS is usable only as 1 to 6 decimal digits with a value above 0.
func TestTerminalColumnsAcceptsOnlySixDigitPositiveNumbers(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"40", 40},
		{"040", 40},
		{"1", 1},
		{"999999", 999999},
		{"0", 0},
		{"+40", 0},
		{"\x2040", 0},
		{"1234567", 0},
		{"", 0},
		{"four", 0},
	}
	for _, c := range cases {
		if got := terminalColumns(map[string]string{"COLUMNS": c.in}); got != c.want {
			t.Errorf("COLUMNS=%q: got %d, want %d", c.in, got, c.want)
		}
	}
}

// S29: a piece joins the row while the row stays within COLUMNS; a later row is indented under
// the first, and a piece wider than the terminal keeps a row of its own.
func TestWrappedBreaksBetweenPiecesAndIndentsLaterRows(t *testing.T) {
	got := wrapped(red, []string{"alpha (worker),", "beta (worker)"}, 22)
	want := "\x1b[31m👥 alpha (worker),\x1b[0m\n\x1b[31m   beta (worker)\x1b[0m"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	unbroken := wrapped(red, []string{"alpha (worker),", "beta (worker)"}, 0)
	if unbroken != "\x1b[31m👥 alpha (worker), beta (worker)\x1b[0m" {
		t.Fatalf("unbroken row: %q", unbroken)
	}
}

// C10: each team piece is its own span of its colour, the label and the joining spaces plain;
// the rows break as S29's, measured on the plain pieces.
func TestTeamRowsColourEachPieceAlone(t *testing.T) {
	pieces := []string{"alpha: myrepo.1 (w),", "beta: myrepo.1 (w)"}
	colours := []string{"#e879f9", "#22d3ee"}
	one := "👥 \x1b[38;2;232;121;249malpha: myrepo.1 (w),\x1b[0m \x1b[38;2;34;211;238mbeta: myrepo.1 (w)\x1b[0m"
	if got := teamRows(pieces, colours, 42); got != one {
		t.Fatalf("one row: %q", got)
	}
	two := "👥 \x1b[38;2;232;121;249malpha: myrepo.1 (w),\x1b[0m\n   \x1b[38;2;34;211;238mbeta: myrepo.1 (w)\x1b[0m"
	if got := teamRows(pieces, colours, 41); got != two {
		t.Fatalf("two rows: %q", got)
	}
}

// S8: the exception names the settings error prints are Python's.
func TestPythonExceptionNameForAnOSError(t *testing.T) {
	cases := map[syscall.Errno]string{
		syscall.EISDIR:       "IsADirectoryError",
		syscall.EACCES:       "PermissionError",
		syscall.EPERM:        "PermissionError",
		syscall.ENOTDIR:      "NotADirectoryError",
		syscall.EAGAIN:       "BlockingIOError",
		syscall.ECONNRESET:   "ConnectionResetError",
		syscall.ETIMEDOUT:    "TimeoutError",
		syscall.ELOOP:        "OSError",
		syscall.ENAMETOOLONG: "OSError",
	}
	for errno, want := range cases {
		err := &fs.PathError{Op: "open", Path: "x", Err: errno}
		if got := pythonExceptionName(err); got != want {
			t.Errorf("errno %v: got %q, want %q", errno, got, want)
		}
	}
	if got := pythonExceptionName(errors.New("other")); got != "OSError" {
		t.Errorf("non-errno: got %q", got)
	}
}

// S8: a settings file that cannot be read is a red note naming the Python exception: a
// directory is IsADirectoryError, and (for a user other than root) a mode 000 file is
// PermissionError.
func TestUnreadableSettingsNamesThePythonException(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	path := filepath.Join(dir, "settings.json")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	_, dirErr := innerCommand("", false, map[string]string{"HOME": home}, home)
	if dirErr == nil || dirErr.Error() != path+" cannot be read (IsADirectoryError)" {
		t.Fatalf("a directory: %v", dirErr)
	}
	if os.Getuid() == 0 {
		return
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o000); err != nil {
		t.Fatal(err)
	}
	_, modeErr := innerCommand("", false, map[string]string{"HOME": home}, home)
	if modeErr == nil || modeErr.Error() != path+" cannot be read (PermissionError)" {
		t.Fatalf("mode 000: %v", modeErr)
	}
}

// S8: a relative CLAUDE_CONFIG_DIR is read from the render's working directory, and a file that
// is not UTF-8 or not JSON is named as the Python names it.
func TestSettingsUnderARelativeConfigDir(t *testing.T) {
	cwd := t.TempDir()
	if err := os.Mkdir(filepath.Join(cwd, "conf"), 0o700); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"CLAUDE_CONFIG_DIR": "conf"}
	for content, want := range map[string]string{
		"\xff":   "conf/settings.json cannot be read (UnicodeDecodeError)",
		"{":      "conf/settings.json is not valid JSON",
		"[1, 2]": "",
	} {
		if err := os.WriteFile(filepath.Join(cwd, "conf", "settings.json"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		got := ""
		if _, err := innerCommand("", false, env, cwd); err != nil {
			got = err.Error()
		}
		if got != want {
			t.Errorf("%q: got %q, want %q", content, got, want)
		}
	}
}

// D2: an inner command that cannot be started (here its working directory is gone) is a red
// note, not the end of the render.
func TestAnInnerThatCannotStartIsANote(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "conf"), 0o700); err != nil {
		t.Fatal(err)
	}
	settings := `{"statusLine": {"command": "true"}}`
	if err := os.WriteFile(filepath.Join(root, "conf", "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	in := Input{
		Env:   map[string]string{"CLAUDE_CONFIG_DIR": filepath.Join(root, "conf")},
		Cwd:   filepath.Join(root, "gone"),
		Stdin: []byte(`{}`),
	}
	var stderr bytes.Buffer
	out, code := Run(context.Background(), in, &stderr)
	if code != 0 || string(out) != red+notRun+reset+"\n" {
		t.Fatalf("exit %d, out %q, stderr %q", code, out, stderr.String())
	}
}

// innerRender renders a status line whose inner command is command, with env, and returns the
// stdout text.
func innerRender(t *testing.T, command string, env map[string]string, stdin string) string {
	t.Helper()
	conf := t.TempDir()
	settings := `{"statusLine": {"command": ` + strconv.Quote(command) + `}}`
	if err := os.WriteFile(filepath.Join(conf, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	env["CLAUDE_CONFIG_DIR"] = conf
	var stderr bytes.Buffer
	out, code := Run(context.Background(), Input{Env: env, Cwd: t.TempDir(), Stdin: []byte(stdin)}, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	return string(out)
}

// S9: a payload that names the project runs an inner command that spells $CLAUDE_PROJECT_DIR
// with the variable unset, because the inner sees the project this command resolved.
func TestInnerSeesTheResolvedProjectWhenTheVariableIsUnset(t *testing.T) {
	project := t.TempDir()
	for _, stdin := range []string{
		`{"workspace": {"project_dir": ` + strconv.Quote(project) + `}}`,
		`{"cwd": ` + strconv.Quote(project) + `}`,
	} {
		got := innerRender(t, `printf %s "$CLAUDE_PROJECT_DIR"`, map[string]string{}, stdin)
		if got != project+"\n" {
			t.Errorf("payload %s: got %q, want %q", stdin, got, project+"\n")
		}
	}
}

// S9: a variable that is set to an absolute path is passed on untouched.
func TestInnerKeepsAnAbsoluteProjectVariable(t *testing.T) {
	got := innerRender(t, `printf %s "$CLAUDE_PROJECT_DIR"`,
		map[string]string{"CLAUDE_PROJECT_DIR": "/set/by/claude"}, `{"cwd": "/payload/cwd"}`)
	if got != "/set/by/claude\n" {
		t.Errorf("got %q", got)
	}
}

// S9a: with no project to be found, a command that spells the variable is not run; the note
// names the variable and the payload fields that were empty.
func TestInnerNamingTheVariableWithNoProjectIsANote(t *testing.T) {
	got := innerRender(t, `"$CLAUDE_PROJECT_DIR"/.claude/hooks/status-line`, map[string]string{}, `{}`)
	want := red + noProjectNote + reset + "\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if !strings.Contains(noProjectNote, "CLAUDE_PROJECT_DIR") || !strings.Contains(noProjectNote, "workspace.project_dir") {
		t.Errorf("the note does not name the variable and the fields: %q", noProjectNote)
	}
}

// S9a: a command that does not spell the variable runs whether or not a project is found.
func TestInnerNotNamingTheVariableRunsWithNoProject(t *testing.T) {
	if got := innerRender(t, `echo plain`, map[string]string{}, `{}`); got != "plain\n" {
		t.Errorf("got %q", got)
	}
}
