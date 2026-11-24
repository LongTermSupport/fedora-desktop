package seatsweep

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

const (
	// goneBase is a member URL on an address of this machine where nothing listens.
	goneBase   = "http://127.0.0.1:1"
	goneHandle = "repo.1+site.podman"
)

// goneBundle is the pool seat.json of a seat of team at base.
func goneBundle(t *testing.T, team, base string) seat.Bundle {
	t.Helper()
	id := mustTeam(t, team)
	server := paths.ServerName(id)
	digest, err := paths.ParseDigest("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return seat.Bundle{
		V: seat.BundleVersion, Team: id, ServerName: server, UserID: proto.UserID(goneHandle, server),
		AdminID: proto.AdminID(server), MainRoom: "!" + "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ",
		BaseURL: base, DeviceID: paths.SeatDevice, IssuedMS: 1, Issuer: digest,
	}
}

// gone is a GoneTeams over the world's seat store, a status root that exists and holds the
// records in installed, and loopback as the machine's only address.
func (w *world) gone(installed ...string) GoneTeams {
	w.t.Helper()
	root := filepath.Join(w.home, "status")
	if err := os.MkdirAll(root, 0o700); err != nil {
		w.t.Fatal(err)
	}
	return GoneTeams{
		Store: w.store, StatusRoot: root,
		Status: func(team paths.TeamID) (status.Record, bool, error) {
			for _, i := range installed {
				if team.String() == i {
					return status.Record{}, true, nil
				}
			}
			return status.Record{}, false, nil
		},
		Local: func(a netip.Addr) (bool, error) { return a.IsLoopback(), nil },
	}
}

// A seat whose server is on this machine, of a team not installed here and desired by no
// session, is gone; the reason names every source read.
func TestGoneTeamIsGone(t *testing.T) {
	w := newWorld(t)
	w.desire("s1", `{"team":"live","seat":"1"}`)
	why, err := w.gone("live").Gone(goneBundle(t, "dead", goneBase))
	if err != nil || why == "" {
		t.Fatalf("Gone = %q, %v", why, err)
	}
	for _, want := range []string{"dead", "status record", "desired", "127.0.0.1"} {
		if !strings.Contains(why, want) {
			t.Errorf("why %q does not name %q", why, want)
		}
	}
}

// Each local source that knows the team keeps it: its status record here, or any session that
// desires it, whether or not anything else declares it (an ad-hoc team).
func TestAKnownTeamIsNotGone(t *testing.T) {
	w := newWorld(t)
	if why, err := w.gone("dead").Gone(goneBundle(t, "dead", goneBase)); err != nil || why != "" {
		t.Errorf("installed here: %q, %v", why, err)
	}
	w.desire("s1", `{"team":"live","seat":"1"}`)
	w.desire("s2", `{"team":"adhoc","seat":"1"}`)
	if why, err := w.gone().Gone(goneBundle(t, "adhoc", goneBase)); err != nil || why != "" {
		t.Errorf("desired by a session, declared nowhere: %q, %v", why, err)
	}
}

// What the machine cannot judge is not gone: a team on another machine, a host name, a
// machine that serves no teams, and a session still launching (no desired.json yet).
func TestWhatCannotBeJudgedIsNotGone(t *testing.T) {
	w := newWorld(t)
	g := w.gone()
	for name, base := range map[string]string{
		"another machine": "https://192.0.2.1:8448",
		"a host name":     "https://chat.example.org:8448",
	} {
		b := goneBundle(t, "dead", base)
		b.TLSCA = ""
		if why, err := g.Gone(b); err != nil || why != "" {
			t.Errorf("%s: %q, %v", name, why, err)
		}
	}
	noHost := g
	noHost.StatusRoot = filepath.Join(w.home, "none")
	if why, err := noHost.Gone(goneBundle(t, "dead", goneBase)); err != nil || why != "" {
		t.Errorf("no status root: %q, %v", why, err)
	}
	if err := os.MkdirAll(w.store.Control(mustSession(t, "s3")), 0o700); err != nil {
		t.Fatal(err)
	}
	if why, err := g.Gone(goneBundle(t, "dead", goneBase)); err != nil || why != "" {
		t.Errorf("a session with no desired.json: %q, %v", why, err)
	}
}

// A source that cannot be read is an error, never gone: the release is retried.
func TestAnUnreadableSourceIsAnError(t *testing.T) {
	w := newWorld(t)
	b := goneBundle(t, "dead", goneBase)
	g := w.gone()
	g.Status = func(paths.TeamID) (status.Record, bool, error) { return status.Record{}, false, errors.New("denied") }
	if why, err := g.Gone(b); err == nil || why != "" {
		t.Errorf("status unreadable: %q, %v", why, err)
	}
	g = w.gone()
	g.Local = func(netip.Addr) (bool, error) { return false, errors.New("no interfaces") }
	if why, err := g.Gone(b); err == nil || why != "" {
		t.Errorf("addresses unreadable: %q, %v", why, err)
	}
	g = w.gone()
	w.write(w.store.DesiredFile(mustSession(t, "s1")), "not json")
	if why, err := g.Gone(b); err == nil || why != "" {
		t.Errorf("desired.json unreadable: %q, %v", why, err)
	}
}

// The machine's own addresses: loopback is one, an address of the documentation range is not.
func TestLocalAddress(t *testing.T) {
	if local, err := LocalAddress(netip.MustParseAddr("127.0.0.1")); err != nil || !local {
		t.Errorf("loopback: %v, %v", local, err)
	}
	if local, err := LocalAddress(netip.MustParseAddr("192.0.2.1")); err != nil || local {
		t.Errorf("192.0.2.1: %v, %v", local, err)
	}
}

// The sweep releases a seat of a gone team whose server does not answer, without its logout,
// and keeps the seat of a team a session desires.
func TestSweepReleasesAGoneTeamsSeat(t *testing.T) {
	w := newWorld(t)
	w.live["s1"] = true
	w.desire("s1", `{"team":"live","seat":"1"}`)
	key := w.key("/srv/a")
	dead := w.hold(key, "dead", goneHandle, "s1")
	data, err := goneBundle(t, "dead", goneBase).Encode()
	if err != nil {
		t.Fatal(err)
	}
	w.write(dead.Bundle(), string(data))
	w.write(dead.HeldToken(), "tok")
	w.cmd.Parts.Teams = w.gone("live")
	w.cmd.Parts.Log = &w.errOut
	if code := w.run("sweep"); code != 0 {
		t.Fatalf("sweep exit %d: %s", code, w.errOut.String())
	}
	if exists(t, dead.Held()) || !strings.Contains(w.out.String(), "seat "+goneHandle+" of team dead") {
		t.Errorf("the gone team's seat is still held: %s / %s", w.out.String(), w.errOut.String())
	}
	if !strings.Contains(w.errOut.String(), "abandoned") {
		t.Errorf("the abandonment was not logged: %s", w.errOut.String())
	}
}
