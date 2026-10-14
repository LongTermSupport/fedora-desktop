package teams

import (
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
	if !strings.Contains(h.errOut.String(), "could not write") || !strings.Contains(h.errOut.String(), "disk full") {
		t.Fatalf("stderr %q", h.errOut.String())
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
