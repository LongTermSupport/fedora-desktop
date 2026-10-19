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

func TestFileColumnsIsZeroForWhatIsNotATerminal(t *testing.T) {
	if FileColumns(&bytes.Buffer{}) != 0 {
		t.Error("a buffer has no width")
	}
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	if FileColumns(f) != 0 {
		t.Error("a regular file has no width")
	}
}

func TestFileIsTerminalIsFalseForDevNullAndPipes(t *testing.T) {
	if FileIsTerminal("not a file") {
		t.Fatal("only a file can be a terminal")
	}
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	if FileIsTerminal(null) {
		t.Fatal("/dev/null is a character device but not a terminal")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if FileIsTerminal(reader) {
		t.Fatal("a pipe is not a terminal")
	}
	if err = errors.Join(null.Close(), reader.Close(), writer.Close()); err != nil {
		t.Fatal(err)
	}
}

func TestPrinterKeepsTheFirstFailedWrite(t *testing.T) {
	var p Printer
	var ok bytes.Buffer
	p.Print(&ok, "fine\n")
	if p.Err() != nil || ok.String() != "fine\n" {
		t.Fatalf("a good write: %v %q", p.Err(), ok.String())
	}
	p.Print(failWriter{}, "x")
	p.Print(&ok, "still written\n")
	if p.Err() == nil || p.Err().Error() != "broken pipe" || ok.String() != "fine\nstill written\n" {
		t.Fatalf("after a failed write: %v %q", p.Err(), ok.String())
	}
}

func TestBoldAndResetAreTheEscapeCodes(t *testing.T) {
	if Bold != "\x1b[1m" || Reset != "\x1b[0m" {
		t.Fatalf("%q %q", Bold, Reset)
	}
}

func TestYesNoIsTheRecordFilesWord(t *testing.T) {
	if YesNo(true) != "yes" || YesNo(false) != "no" {
		t.Fatalf("YesNo: %q %q", YesNo(true), YesNo(false))
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestPrintfKeepsFirstWriteError(t *testing.T) {
	tm := &Term{Err: failWriter{}, IsTerminal: func(any) bool { return false }, Getenv: func(string) string { return "" }}
	tm.Say("a")
	tm.Say("b")
	if tm.WriteFailed() == nil || tm.WriteFailed().Error() != "broken pipe" {
		t.Errorf("got %v", tm.WriteFailed())
	}
}

// The numeric values are the tools' exit statuses, which scripts and the parity suites read.
func TestExitCodesKeepTheirStatuses(t *testing.T) {
	for code, want := range map[ExitCode]int{ExitOK: 0, ExitFailure: 1, ExitUsage: 64, ExitCancelled: 130} {
		if int(code) != want {
			t.Errorf("exit code %v is %d, want %d", code, int(code), want)
		}
	}
}

func TestFinishKeepsAFailureAndFailsAnUnwrittenSuccess(t *testing.T) {
	tm, buf := newTerm(false, "")
	if got := tm.Finish(ExitUsage, errors.New("pipe")); got != ExitUsage {
		t.Errorf("a run that failed keeps its code, got %d", got)
	}
	if got := tm.Finish(ExitOK, nil); got != ExitOK {
		t.Errorf("a clean run is ok, got %d", got)
	}
	if got := tm.Finish(ExitOK, errors.New("pipe")); got != ExitFailure || !strings.Contains(buf.String(), "could not write the output: pipe") {
		t.Errorf("an unwritten success fails, got %d with %q", got, buf.String())
	}
}

func TestNewUsesProcessEnvironment(t *testing.T) {
	tm := New()
	if tm.Err == nil || tm.IsTerminal == nil || tm.Getenv == nil {
		t.Fatal("New leaves a seam unset")
	}
}
