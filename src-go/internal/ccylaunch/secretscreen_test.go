package ccylaunch

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// screenRun is a --secret-screen run reading text, its stdout a terminal or not, Enter read
// from enter (an error opening the terminal when enter is nil).
func screenRun(t *testing.T, text string, terminal bool, enter io.Reader, args ...string) *run {
	t.Helper()
	r := newRun(install(t), append([]string{altScreenFlag}, args...)...)
	r.front.Stdin = strings.NewReader(text)
	r.front.IsTerminal = func(any) bool { return terminal }
	r.front.OpenTTY = func() (io.ReadCloser, error) {
		if enter == nil {
			return nil, errors.New("no terminal")
		}
		return io.NopCloser(enter), nil
	}
	return r
}

func TestSecretScreenNotOnATerminalIsThePayloadByteForByte(t *testing.T) {
	for _, text := range []string{"sk-ant-oat01-x\n", "two\nlines\n\n", "no newline", ""} {
		r := screenRun(t, text, false, nil)
		if code := r.start(t); code != term.ExitOK || r.stdout.String() != text || r.stderr.Len() != 0 {
			t.Fatalf("%q: exit %d, stdout %q, stderr %q", text, code, r.stdout, r.stderr)
		}
	}
}

func TestSecretScreenOnATerminalShowsTheTextOnTheAlternateScreenUntilEnter(t *testing.T) {
	r := screenRun(t, "the value\n", true, strings.NewReader("\n"))
	if code := r.start(t); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, r.stderr)
	}
	want := term.AltScreenOn + "the value\n" + altScreenPrompt + term.AltScreenOff
	if r.stdout.String() != want {
		t.Fatalf("stdout %q, want %q", r.stdout, want)
	}
}

func TestSecretScreenWithNoEnterStillLeavesTheScreenAndFails(t *testing.T) {
	r := screenRun(t, "the value\n", true, strings.NewReader(""))
	if code := r.start(t); code != term.ExitFailure {
		t.Fatalf("exit %d, want %d", code, term.ExitFailure)
	}
	if out := r.stdout.String(); !strings.HasSuffix(out, term.AltScreenOff) {
		t.Fatalf("stdout %q does not leave the alternate screen", out)
	}
	if !strings.Contains(r.stderr.String(), "Enter") {
		t.Fatalf("stderr %q does not say the Enter was not read", r.stderr)
	}
}

func TestSecretScreenWithNoTerminalToReadEnterFromLeavesTheScreenAndFails(t *testing.T) {
	r := screenRun(t, "the value\n", true, nil)
	if code := r.start(t); code != term.ExitFailure || !strings.HasSuffix(r.stdout.String(), term.AltScreenOff) {
		t.Fatalf("exit %d, stdout %q", code, r.stdout)
	}
}

func TestSecretScreenTakesNoArguments(t *testing.T) {
	r := screenRun(t, "x", false, nil, "extra")
	if code := r.start(t); code != term.ExitUsage || r.stdout.Len() != 0 {
		t.Fatalf("exit %d, stdout %q", code, r.stdout)
	}
}

func TestSecretScreenIsAnsweredUnderLegacyToo(t *testing.T) {
	r := screenRun(t, "x\n", false, nil)
	r.env(LegacyEnv + "=1")
	if code := r.start(t); code != term.ExitOK || r.stdout.String() != "x\n" || len(r.execer.Execs()) != 0 {
		t.Fatalf("exit %d, stdout %q, execs %d", code, r.stdout, len(r.execer.Execs()))
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read broke") }

func TestSecretScreenWhoseInputBreaksLeavesTheScreenAndFails(t *testing.T) {
	r := screenRun(t, "", true, strings.NewReader("\n"))
	r.front.Stdin = failingReader{}
	if code := r.start(t); code != term.ExitFailure || !strings.HasSuffix(r.stdout.String(), term.AltScreenOff) {
		t.Fatalf("exit %d, stdout %q", code, r.stdout)
	}
	if !strings.Contains(r.stderr.String(), "read broke") {
		t.Fatalf("stderr %q", r.stderr)
	}
}
