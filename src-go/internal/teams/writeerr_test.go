package teams

import (
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestUnwritableStdoutIsAFailureExit(t *testing.T) {
	h := newHarness(t)
	h.app.Stdout = brokenWriter{}
	if code := h.app.Run([]string{"--help"}); code != term.ExitFailure {
		t.Fatalf("exit %d, want %d", code, term.ExitFailure)
	}
	if !strings.Contains(h.ErrOut.String(), "could not write") || !strings.Contains(h.ErrOut.String(), "disk full") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestUnwritableTerminalLineIsAFailureExit(t *testing.T) {
	h := newHarness(t)
	h.app.Term.Err = brokenWriter{}
	h.app.Term.Say("a line nobody can read")
	if code := h.app.Run([]string{"--help"}); code != term.ExitFailure {
		t.Fatalf("exit %d, want %d", code, term.ExitFailure)
	}
}

func TestAFailureExitIsNotMaskedByTheWriteError(t *testing.T) {
	h := newHarness(t)
	h.app.Stderr = brokenWriter{}
	if code := h.app.Run([]string{"--bogus"}); code != term.ExitUsage {
		t.Fatalf("exit %d, want %d", code, term.ExitUsage)
	}
}

// brokenWriter fails every write.
type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }
