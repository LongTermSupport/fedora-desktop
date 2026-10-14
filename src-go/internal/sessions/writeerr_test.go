package sessions

import (
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

type unwritable struct{}

func (unwritable) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestUnwritableStdoutIsAFailureExit(t *testing.T) {
	h := newHarness(t)
	h.app.Stdout = unwritable{}
	if code := h.app.Run([]string{"--help"}); code != term.ExitFailure {
		t.Fatalf("exit %d, want %d", code, term.ExitFailure)
	}
	if !strings.Contains(h.errOut.String(), "could not write") || !strings.Contains(h.errOut.String(), "disk full") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}

func TestUnwritableTerminalLineIsAFailureExit(t *testing.T) {
	h := newHarness(t)
	h.app.Term.Err = unwritable{}
	h.app.Term.Say("a line nobody can read")
	if code := h.app.Run([]string{"--help"}); code != term.ExitFailure {
		t.Fatalf("exit %d, want %d", code, term.ExitFailure)
	}
}

func TestAUsageExitIsNotMaskedByTheWriteError(t *testing.T) {
	h := newHarness(t)
	h.app.Stdout = unwritable{}
	if code := h.app.Run([]string{"--bogus"}); code != term.ExitUsage {
		t.Fatalf("exit %d, want %d", code, term.ExitUsage)
	}
}
