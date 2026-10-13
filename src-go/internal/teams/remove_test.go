package teams

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRemoveWithoutATeamIsAUsageError(t *testing.T) {
	h := newHarness(t)
	if code := h.app.remove(t.Context(), nil); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.errOut.String(), "'remove' takes one team") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}
