package picker

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// noLxc is a host whose LXC cannot be listed, so the flow ends at once and says why.
type noLxc struct{}

func (noLxc) Containers(context.Context) ([]string, []string, error) {
	return nil, nil, errors.New("LXC is not installed on this host (missing: lxc-ls)")
}

func (noLxc) Users(context.Context, string) ([]string, error) { return nil, errors.New("not asked") }

func (noLxc) ReadCheckout(context.Context, string, string, string, string, string) (string, error) {
	return "", errors.New("not asked")
}

func TestEveryRowsMenuOffersAddingAnLxcContainerWithNoHiddenKey(t *testing.T) {
	h := newRig(t)
	r := h.run1(t)
	for _, row := range r.snap.Rows {
		labels := h.labels(r, row)
		if !slices.Contains(labels, "Add an LXC container's agent to alpha") {
			t.Fatalf("%s: menu %q lacks the LXC action", row.Member, labels)
		}
		if footer := r.footerFor(row); !strings.Contains(footer, "add an LXC container to alpha via Enter") {
			t.Fatalf("%s: footer %q", row.Member, footer)
		}
	}
	if slices.Contains(pickerKeys, keyLxc) {
		t.Fatalf("%s must be reached from the menu, not a hidden key", keyLxc)
	}
}

func TestChoosingTheLxcActionRunsLxcAddForTheRowsTeam(t *testing.T) {
	h := newRig(t)
	h.On(h.DeclarePrefix("declared"), teamtest.DeclaredAlpha, 0)
	h.p.Lxc.Lister = noLxc{}
	h.p.Lxc.SudoReady = func(context.Context, string) bool { return true }
	r := h.run1(t)
	if _, err := r.do(context.Background(), keyLxc, h.row(t, r.snap, board.NoDescription)); err != nil {
		t.Fatal(err)
	}
	said := h.ErrOut.String()
	for _, want := range []string{"LXC is not installed on this host", "(ccy-teams lxc-add alpha failed, exit 1: see above.)"} {
		if !strings.Contains(said, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, said)
		}
	}
}

func TestTheHelpListsTheLxcAction(t *testing.T) {
	h := newRig(t)
	h.p.showHelp(context.Background())
	if !strings.Contains(h.ErrOut.String(), "Add an LXC container's agent") {
		t.Fatalf("help:\n%s", h.ErrOut.String())
	}
}
