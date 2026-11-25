package teams

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatmodel"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Register R39, through the App: `ccy-teams seats model` sets a team's default and a seat's own
// model in the owner's seat store, the seat found in the working directory's checkout's pool by
// its kind, and lists them on stdout.
func TestSeatsModelThroughTheApp(t *testing.T) {
	h := newHarness(t)
	if err := os.MkdirAll(h.Checkout(), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(h.Checkout())
	store, key, _, err := system.Here(h.Host.Getenv, os.Getwd)
	if err != nil {
		t.Fatal(err)
	}
	alpha, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	for _, seat := range []string{"proj.2+local.podman", "proj.2+local.host"} {
		if err := os.MkdirAll(filepath.Join(store.PoolTeam(key, alpha), seat), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"alpha", "opus"}, {"2@alpha", "sonnet"}, {"--host", "2@alpha", "haiku"}} {
		if code := h.app.Run(append([]string{"seats", "model"}, args...)); code != term.ExitOK {
			t.Fatalf("%q: exit %d: %s", args, code, h.ErrOut.String())
		}
	}
	h.Out.Reset()
	if code := h.app.Run([]string{"seats", "model"}); code != term.ExitOK {
		t.Fatalf("list: exit %d: %s", code, h.ErrOut.String())
	}
	want := "alpha team opus\nalpha proj.2+local.host haiku\nalpha proj.2+local.podman sonnet\n"
	if h.Out.String() != want {
		t.Fatalf("stdout %q, want %q", h.Out.String(), want)
	}
	if _, err := os.Stat(seatmodel.File(store)); err != nil {
		t.Fatalf("not in the seat store: %v", err)
	}
	if code := h.app.Run([]string{"seats", "model", "7@alpha", "opus"}); code != term.ExitFailure || !strings.Contains(h.ErrOut.String(), "seat 7") {
		t.Fatalf("no such seat: exit %d: %s", code, h.ErrOut.String())
	}
}
