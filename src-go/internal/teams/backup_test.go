package teams

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestDispatchBackupAndRestoreAreNotBuiltYet(t *testing.T) {
	for _, name := range []string{"backup", "restore"} {
		h := newHarness(t)
		if code := h.app.Run([]string{name, "alpha"}); code != term.ExitFailure {
			t.Fatalf("%s: exit %d", name, code)
		}
		if want := "ccy-teams: " + name + " is not built yet in the Go port.\n"; h.errOut.String() != want {
			t.Fatalf("%s: stderr %q, want %q", name, h.errOut.String(), want)
		}
	}
}
