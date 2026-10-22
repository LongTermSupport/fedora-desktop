package cli

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/guest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// guestWorld is a guest home the identity seam opens, and the member layout set-aside
// defaults to.
type guestWorld struct {
	world

	home string
}

func newGuestWorld(t *testing.T) *guestWorld {
	t.Helper()
	g := &guestWorld{home: t.TempDir() + "/home"}
	if err := os.Mkdir(g.home, 0o700); err != nil {
		t.Fatal(err)
	}
	identifyWas, layoutWas := identify, memberLayout
	t.Cleanup(func() { identify, memberLayout = identifyWas, layoutWas })
	identify = func(inv Invocation) (guest.Home, error) {
		user, err := paths.ParseGuestUser(inv.flags.value("user"))
		if err != nil {
			return guest.Home{}, err
		}
		root, err := homefs.Open(g.home, homefs.Owned)
		return guest.Home{Root: root, User: user}, err
	}
	memberLayout = func() (paths.Layout, error) { return paths.HomeLayout(g.home, g.home+"/run") }
	return g
}

func (g *guestWorld) runIn(stdin string, args ...string) term.ExitCode {
	return Run(Env{Args: args, Stdin: strings.NewReader(stdin), Stdout: &g.out, Stderr: &g.err, Digest: digest})
}

func desiredFor(t *testing.T, session string) string {
	t.Helper()
	name, err := paths.ParseSessionName(session)
	if err != nil {
		t.Fatal(err)
	}
	data, err := state.Desired{Session: name, WrittenMS: 1, Seats: []state.DesiredSeat{}, Farewell: []state.Farewell{}, Inbound: "accept"}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// GU1, GU4: an op prints only its CHANGED lines on stdout, and nothing when nothing changed.
func TestGuestMembershipPrintsItsChanges(t *testing.T) {
	g := newGuestWorld(t)
	desired := desiredFor(t, "lxc-dev")
	if g.runIn(desired, "guest", "membership", "--user", "dev") != term.ExitOK || g.out.String() != "CHANGED\tdesired.json\n" {
		t.Fatalf("stdout %q stderr %q", g.out.String(), g.err.String())
	}
	g.out.Reset()
	if g.runIn(desired, "guest", "membership", "--user", "dev") != term.ExitOK || g.out.Len() != 0 || g.err.Len() != 0 {
		t.Fatalf("again: stdout %q stderr %q", g.out.String(), g.err.String())
	}
}

// GU3, GU5, SA1: each verb runs its op, and a refusal is the error object on stderr.
func TestGuestVerbsRunTheirOps(t *testing.T) {
	g := newGuestWorld(t)
	for _, c := range []struct {
		stdin string
		args  []string
		exit  term.ExitCode
	}{
		{"{", []string{"guest", "place-bundle", "--user", "dev"}, exits.ExitConfig},
		{"", []string{"guest", "remove", "--user", "dev", "--team", "alpha"}, term.ExitOK},
		{"", []string{"seat", "set-aside", "--team", "alpha"}, term.ExitOK},
		{"", []string{"seat", "set-aside", "--team", "alpha", "--state", g.home}, term.ExitOK},
	} {
		g.out.Reset()
		g.err.Reset()
		if got := g.runIn(c.stdin, c.args...); got != c.exit || g.out.Len() != 0 {
			t.Errorf("%q: exit %d stdout %q stderr %q", c.args, got, g.out.String(), g.err.String())
		}
		if c.exit != term.ExitOK {
			if e := g.lastError(t); e.Code != exits.CodeBundle {
				t.Errorf("%q: %+v", c.args, e)
			}
		}
	}
}

// A refused identity or member layout is reported, and no op runs.
func TestGuestSeamsRefuse(t *testing.T) {
	g := newGuestWorld(t)
	identify = func(Invocation) (guest.Home, error) { return guest.Home{}, errors.New("not this user") }
	memberLayout = func() (paths.Layout, error) { return paths.Layout{}, errors.New("no home") }
	for _, args := range [][]string{
		{"guest", "place-bundle", "--user", "dev"},
		{"guest", "membership", "--user", "dev"},
		{"guest", "remove", "--user", "dev", "--team", "alpha"},
		{"seat", "set-aside", "--team", "alpha"},
	} {
		g.err.Reset()
		if g.runIn("", args...) != exits.ExitInternal || g.out.Len() != 0 {
			t.Errorf("%q: stderr %q", args, g.err.String())
		}
	}
}

// The default seams are the process's own: its home layout, and no identity until GU1 is
// joined in.
func TestDefaultGuestSeams(t *testing.T) {
	if _, err := identifyUnbuilt(Invocation{}); err == nil {
		t.Fatal("the unjoined identity seam answered")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	l, err := processLayout()
	if err != nil || l.State() != home+"/.config/teamchat/seats" {
		t.Fatalf("%v, %v", l, err)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if _, err := processLayout(); err == nil {
		t.Fatal("no runtime directory made a layout")
	}
}
