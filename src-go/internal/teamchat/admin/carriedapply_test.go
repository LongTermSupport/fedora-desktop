package admin

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// SPEC-carry SG6, O6: `team apply` of a team declared as carried, on a host where it was never
// applied or imported, exits 70 naming team import, with nothing of the team made; the same
// team unmarked applies.
func TestApplyOfACarriedTeamBeforeItsImport(t *testing.T) {
	w := newWorld(t)
	doc := alphaDoc()
	docTeam(doc)["carried_from"] = "the bus guest before the rebuild"
	in := encodeDoc(t, doc)
	w.ok(t, in, "team", "approve", "--team", "alpha")
	out := w.verb(in, "team", "apply")
	if out.code != ExitRefused || !strings.Contains(out.stderr, "team alpha is declared as carried: run team import first, then team apply") {
		t.Fatalf("code %d\nstdout %s\nstderr %s", out.code, out.stdout, out.stderr)
	}
	if _, err := os.Lstat(w.root + paths.AdminState(alpha(t))); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the refused apply made the team's admin state: %v", err)
	}
	w.applied(t)
}
