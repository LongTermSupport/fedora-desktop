package reclaim

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// SPEC-reclaim.md section 6: a command no test scripted fails instead of being answered, and the
// oracle records every command that may change the machine, not a list of known ones.
func TestOracleSeesUnlistedChanges(t *testing.T) {
	w := newWorld(t)
	w.install("podman", "dnf")
	env := w.envFor()
	ctx := context.Background()
	if _, err := env.Runner.Run(ctx, execx.Cmd{Name: "podman", Args: []string{"system", "reset", "-f"}}); !errors.Is(err, execx.ErrUnexpected) {
		t.Errorf("an unscripted podman system reset answered %v", err)
	}
	if err := env.Sudo.Run(ctx, "dnf", "remove", "pkg-a"); !errors.Is(err, execx.ErrUnexpected) {
		t.Errorf("an unscripted root dnf remove answered %v", err)
	}
	want := []string{"podman system reset -f", "root dnf remove pkg-a"}
	if got := w.changes(); !slices.Equal(got, want) {
		t.Errorf("changes %q, want %q", got, want)
	}
	if len(w.unscripted) != 2 {
		t.Errorf("unscripted %q", w.unscripted)
	}
	w.unscripted = nil
}
