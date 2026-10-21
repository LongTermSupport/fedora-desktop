package statusline

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
	got := wrapped(cyan, []string{"alpha (worker),", "beta (worker)"}, 22)
	want := "\x1b[36m👥 alpha (worker),\x1b[0m\n\x1b[36m   beta (worker)\x1b[0m"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	unbroken := wrapped(cyan, []string{"alpha (worker),", "beta (worker)"}, 0)
	if unbroken != "\x1b[36m👥 alpha (worker), beta (worker)\x1b[0m" {
		t.Fatalf("unbroken row: %q", unbroken)
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
