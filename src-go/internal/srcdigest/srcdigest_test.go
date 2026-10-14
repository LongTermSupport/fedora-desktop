package srcdigest

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestOnlyVersionAloneIsAnsweredAndPrintsTheDigest(t *testing.T) {
	var out bytes.Buffer
	ran := false
	next := func([]string) term.ExitCode { ran = true; return 7 }
	if code := Run([]string{"--version"}, "abc123", &out, io.Discard, next); code != term.ExitOK || out.String() != "abc123\n" || ran {
		t.Fatalf("code %d, out %q, ran %v", code, out.String(), ran)
	}
}

func TestEveryOtherCommandLineGoesToTheTool(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"--version", "x"}, {"x", "--version"}, {"-version"}} {
		var out bytes.Buffer
		var got []string
		next := func(a []string) term.ExitCode { got = a; return 7 }
		if code := Run(args, "abc123", &out, io.Discard, next); code != 7 || out.Len() != 0 || strings.Join(got, " ") != strings.Join(args, " ") {
			t.Errorf("%v: code %d, out %q, passed %v", args, code, out.String(), got)
		}
	}
}

func TestABuildWithoutADigestSaysSoRatherThanPrintingNothing(t *testing.T) {
	var out, errw bytes.Buffer
	next := func([]string) term.ExitCode { return 0 }
	if code := Run([]string{"--version"}, "", &out, &errw, next); code != term.ExitFailure || out.Len() != 0 ||
		!strings.Contains(errw.String(), "-X main.SourceDigest") {
		t.Fatalf("code %d, out %q, err %q", code, out.String(), errw.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestAVersionThatCannotBeWrittenFails(t *testing.T) {
	next := func([]string) term.ExitCode { return 0 }
	if code := Run([]string{"--version"}, "abc", failingWriter{}, io.Discard, next); code != term.ExitFailure {
		t.Fatalf("code %d", code)
	}
	if code := Run([]string{"--version"}, "", io.Discard, failingWriter{}, next); code != term.ExitFailure {
		t.Fatalf("code %d", code)
	}
}
