package term

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func newTerm(tty bool, noColor string) (*Term, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return &Term{
		Err:        buf,
		IsTerminal: func(any) bool { return tty },
		Getenv: func(k string) string {
			if k == "NO_COLOR" {
				return noColor
			}
			return ""
		},
	}, buf
}

func TestColourOnNeedsTerminalAndNoNoColor(t *testing.T) {
	cases := []struct {
		tty     bool
		noColor string
		want    bool
	}{
		{true, "", true},
		{false, "", false},
		{true, "1", false},
		{false, "1", false},
	}
	for _, c := range cases {
		tm, buf := newTerm(c.tty, c.noColor)
		if got := tm.ColourOn(buf); got != c.want {
			t.Errorf("tty=%v NO_COLOR=%q: got %v want %v", c.tty, c.noColor, got, c.want)
		}
	}
}

func TestSay(t *testing.T) {
	tm, buf := newTerm(false, "")
	tm.Say("heading")
	if buf.String() != "heading\n" {
		t.Errorf("plain: %q", buf.String())
	}
	tm, buf = newTerm(true, "")
	tm.Say("heading")
	if buf.String() != "\x1b[1mheading\x1b[0m\n" {
		t.Errorf("colour: %q", buf.String())
	}
}

func TestPrintErrorIsLiteralAndColourOnlyOnTerminal(t *testing.T) {
	tm, buf := newTerm(false, "")
	tm.PrintError(`bad path C:\new\tab`)
	if buf.String() != "ERROR: bad path C:\\new\\tab\n" {
		t.Errorf("plain: %q", buf.String())
	}
	tm, buf = newTerm(true, "")
	tm.PrintError("boom")
	if buf.String() != "\x1b[0;31mERROR:\x1b[0m boom\n" {
		t.Errorf("colour: %q", buf.String())
	}
	tm, buf = newTerm(true, "1")
	tm.PrintError("boom")
	if strings.Contains(buf.String(), "\x1b") {
		t.Errorf("NO_COLOR ignored: %q", buf.String())
	}
}

func TestStepCountsAndAnswered(t *testing.T) {
	tm, buf := newTerm(false, "")
	tm.Steps = 3
	tm.Step("Name")
	tm.Step("Org")
	want := "\n1/3  Name\n\n2/3  Org\n"
	if buf.String() != want {
		t.Errorf("got %q want %q", buf.String(), want)
	}
	buf.Reset()
	tm.Answered("alpha")
	if buf.String() != "     ✓ alpha\n" {
		t.Errorf("answered: %q", buf.String())
	}

	tm, buf = newTerm(true, "")
	tm.Steps = 1
	tm.Step("Name")
	tm.Answered("alpha")
	want = "\n\x1b[1m\x1b[33m 1/1  Name\x1b[0m\n     \x1b[32m✓\x1b[0m alpha\n"
	if buf.String() != want {
		t.Errorf("colour: got %q want %q", buf.String(), want)
	}
}

func TestAbbrevHome(t *testing.T) {
	cases := []struct{ path, home, want string }{
		{"/home/u/.local/x.log", "/home/u", "~/.local/x.log"},
		{"/home/u", "/home/u", "~"},
		{"/home/u2/x", "/home/u", "/home/u2/x"},
		{"/etc/x", "/home/u", "/etc/x"},
		{"/etc/x", "", "/etc/x"},
	}
	for _, c := range cases {
		if got := AbbrevHome(c.path, c.home); got != c.want {
			t.Errorf("AbbrevHome(%q,%q)=%q want %q", c.path, c.home, got, c.want)
		}
	}
}

func TestFileIsTerminal(t *testing.T) {
	if FileIsTerminal(&bytes.Buffer{}) {
		t.Error("a buffer is not a terminal")
	}
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if FileIsTerminal(f) {
		t.Error("a regular file is not a terminal")
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if FileIsTerminal(f) {
		t.Error("a closed file cannot be stat-ed, so is not a terminal")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestPrintfKeepsFirstWriteError(t *testing.T) {
	tm := &Term{Err: failWriter{}, IsTerminal: func(any) bool { return false }, Getenv: func(string) string { return "" }}
	tm.Say("a")
	tm.Say("b")
	if tm.WriteErr == nil || tm.WriteErr.Error() != "broken pipe" {
		t.Errorf("got %v", tm.WriteErr)
	}
}

func TestNewUsesProcessEnvironment(t *testing.T) {
	tm := New()
	if tm.Err == nil || tm.IsTerminal == nil || tm.Getenv == nil {
		t.Fatal("New leaves a seam unset")
	}
}
