package teams

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestBackupAndRestoreAnswerTheirUsageErrors(t *testing.T) {
	h := newHarness(t)
	if code := h.app.backup(t.Context(), nil); code != term.ExitUsage {
		t.Fatalf("backup exit %d", code)
	}
	if code := h.app.restore(t.Context(), []string{"--nope"}); code != term.ExitUsage {
		t.Fatalf("restore exit %d", code)
	}
	if !strings.Contains(h.errOut.String(), "'backup' takes one team") ||
		!strings.Contains(h.errOut.String(), "unknown restore option '--nope'") {
		t.Fatalf("stderr %q", h.errOut.String())
	}
}
