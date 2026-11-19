package picker

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/lxcsession"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
)

// noLxc is a host whose LXC cannot be listed, so the flow ends at once and says why.
type noLxc struct{}

func (noLxc) Containers(context.Context) ([]string, []string, error) {
	return nil, nil, errors.New("LXC is not installed on this host (missing: lxc-ls)")
}

func (noLxc) LxcPath(context.Context) (string, error) { return "", errors.New("not asked") }

func (noLxc) Users(context.Context, string) ([]string, error) { return nil, errors.New("not asked") }

func (noLxc) ReadCheckout(context.Context, string, string, string, string, string) (string, error) {
	return "", errors.New("not asked")
}

func TestEveryRowsMenuOffersAddingAnLxcContainer(t *testing.T) {
	h := newRig(t)
	r := h.run1(t)
	for _, row := range r.snap.Rows {
		labels := h.labels(r, row)
		if !slices.Contains(labels, "Add an LXC container's agent to alpha") {
			t.Fatalf("%s: menu %q lacks the LXC action", row.Member, labels)
		}
		if footer := r.footerFor(row); !strings.Contains(footer, "add an LXC container to alpha") {
			t.Fatalf("%s: footer %q", row.Member, footer)
		}
	}
}

func TestChoosingTheLxcActionRunsLxcAddForTheRowsTeam(t *testing.T) {
	h := newRig(t)
	h.p.Lxc.Lister = noLxc{}
	h.p.Lxc.SudoReady = func(context.Context, string) bool { return true }
	r := h.run1(t)
	if _, err := r.do(context.Background(), actLxc, h.row(t, r.snap, board.NoDescription)); err != nil {
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
	for _, want := range []string{"Add an LXC container's agent", "Start its session in its LXC container"} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("help lacks %q:\n%s", want, h.ErrOut.String())
		}
	}
}

// Plan 00257 (owner): an LXC member's session never starts with its container; its row offers
// the one start, a named line, and only an LXC member's row does.
func TestOnlyAnLxcMembersRowOffersStartingItsSession(t *testing.T) {
	h := newRig(t)
	r := h.run1(t)
	for _, row := range r.snap.Rows {
		offers := slices.Contains(h.labels(r, row), "Start its session in its LXC container")
		if offers != (row.Kind == lxcMemberKind && row.Handle != "" && row.Handle != "-") {
			t.Fatalf("%s (%s): offers %v, menu %q", row.Member, row.Kind, offers, h.labels(r, row))
		}
	}
	if footer := r.footerFor(h.row(t, r.snap, "proj.box+role.lxc")); !strings.Contains(footer, "start its session") {
		t.Fatalf("footer %q", footer)
	}
}

// Choosing it starts the member's unit inside its container, read from the play's record.
func TestChoosingStartItsSessionStartsTheUnitInItsContainer(t *testing.T) {
	h := newRig(t)
	teamtest.MustWrite(t, filepath.Join(h.Host.AppliedDir, "lxc", "alpha.json"),
		`{"version": 1, "team": "alpha", "members": [{"container": "box", "user": "agent", "state": "present", `+
			`"status": "joined", "handle": "proj.box+role.lxc", "checkout": "/srv/proj", "why": "", "since": 1}]}`)
	h.On("sudo -n -v", "", 0)
	h.On("sudo -n lxc-config lxc.lxcpath", "/var/lib/lxc\n", 0)
	h.On("sudo -n lxc-ls -1 --running", "box\n", 0)
	h.On("sudo -n lxc-ls -1", "box\n", 0)
	h.On("sudo -n lxc-attach", "", 0)
	h.Host.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	h.p.LxcSession = lxcsession.New(h.Host, h.p.Kit)
	r := h.run1(t)
	if _, err := r.do(context.Background(), actStartLxc, h.row(t, r.snap, "proj.box+role.lxc")); err != nil {
		t.Fatal(err)
	}
	if !h.Fake.Called("sudo -n lxc-attach -P /var/lib/lxc -n box --clear-env -- env -i") ||
		!h.Fake.Called("sudo -n lxc-attach -P /var/lib/lxc -n box --clear-env -- env -i PATH=/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin systemctl start -- agent-bus-claude-agent.service") {
		t.Fatalf("calls %v", h.Fake.Calls())
	}
	if !strings.Contains(h.ErrOut.String(), "Started agent-bus-claude-agent.service in container box") {
		t.Fatalf("stderr:\n%s", h.ErrOut.String())
	}
}
