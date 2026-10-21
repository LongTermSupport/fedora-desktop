package statusline

import (
	"errors"
	"io/fs"
	"syscall"
	"testing"
)

// S28: COLUMNS is usable only as 1 to 6 decimal digits with a value above 0.
func TestTerminalColumnsAcceptsOnlySixDigitPositiveNumbers(t *testing.T) {
	cases := map[string]int{
		"40":      40,
		"040":     40,
		"1":       1,
		"999999":  999999,
		"0":       0,
		"+40":     0,
		" 40":     0,
		"1234567": 0,
		"":        0,
		"four":    0,
	}
	for in, want := range cases {
		if got := terminalColumns(map[string]string{"COLUMNS": in}); got != want {
			t.Errorf("COLUMNS=%q: got %d, want %d", in, got, want)
		}
	}
}

// S27: colour escapes take no columns, the bus label takes two, and combining marks none.
func TestDisplayWidthCountsColumnsLikeThePython(t *testing.T) {
	cases := map[string]int{
		"":                       0,
		"desktop (worker)":       16,
		"\x1b[36mdesktop\x1b[0m": 7,
		"👥":                      2,
		"é":                     1,
		"​":                      0,
	}
	for in, want := range cases {
		if got := displayWidth(in); got != want {
			t.Errorf("displayWidth(%q): got %d, want %d", in, got, want)
		}
	}
}

// S29: a piece joins the row while the row stays within COLUMNS; a later row is indented under
// the first, and a piece wider than the terminal keeps a row of its own.
func TestWrappedBreaksBetweenPiecesAndIndentsLaterRows(t *testing.T) {
	got := wrapped(cyan, []string{"alpha (worker),", "beta (worker)"}, " ", 22)
	want := "\x1b[36m👥 alpha (worker),\x1b[0m\n\x1b[36m   beta (worker)\x1b[0m"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	unbroken := wrapped(cyan, []string{"alpha (worker),", "beta (worker)"}, " ", 0)
	if unbroken != "\x1b[36m👥 alpha (worker), beta (worker)\x1b[0m" {
		t.Fatalf("unbroken row: %q", unbroken)
	}
}

// S20 and S8: the exception names the settings error prints are Python's.
func TestPythonExceptionNameForAnOSError(t *testing.T) {
	cases := map[syscall.Errno]string{
		syscall.EISDIR:  "IsADirectoryError",
		syscall.EACCES:  "PermissionError",
		syscall.EPERM:   "PermissionError",
		syscall.ENOTDIR: "NotADirectoryError",
		syscall.ELOOP:   "OSError",
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
