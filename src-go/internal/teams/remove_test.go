package teams

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestDispatchRemoveIsNotBuiltYet(t *testing.T) {
	h := newHarness(t)
	if code := h.app.Run([]string{"remove", "alpha"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if want := "ccy-teams: remove is not built yet in the Go port.\n"; h.errOut.String() != want {
		t.Fatalf("stderr %q, want %q", h.errOut.String(), want)
	}
	if h.out.String() != "" {
		t.Fatalf("stdout %q", h.out.String())
	}
}
