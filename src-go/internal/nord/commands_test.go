package nord

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E17: disconnect brings down every active nord connection, and only those.
func TestDisconnectEveryActive(t *testing.T) {
	w := newWorld(t)
	a := w.imported("nordvpn-uk1")
	a.active = "activated"
	b := w.imported("nordvpn-de2")
	b.active = "activating"
	w.imported("work-vpn").active = "activated"
	wantCode(t, w.run("disconnect"), term.ExitOK)
	wantChanges(t, w, "nmcli connection down uuid "+a.uuid, "nmcli connection down uuid "+b.uuid)
	w.wantErr("Disconnecting uk1...\n", "Disconnected uk1.\n", "Disconnected de2.\n")
}

// E17: nothing active is not an error.
func TestDisconnectNothing(t *testing.T) {
	w := newWorld(t)
	w.imported("nordvpn-uk1")
	wantCode(t, w.run("disconnect"), term.ExitOK)
	wantChanges(t, w)
	w.wantErr("No NordVPN connection is active.\n")
}

// E17, B13: a failure is named, the others still tried, exit 1.
func TestDisconnectFailure(t *testing.T) {
	w := newWorld(t)
	w.imported("nordvpn-uk1").active = "activated"
	w.imported("nordvpn-de2").active = "activated"
	w.fail["down"] = failure{stderr: "Error: not permitted\n", exit: 4}
	wantCode(t, w.run("disconnect"), term.ExitFailure)
	if len(w.changes()) != 2 {
		t.Errorf("changes %q", w.changes())
	}
	w.wantErr("nord: could not disconnect uk1: ", "nord: could not disconnect de2: ")
}

// E11: a listing that fails is an error.
func TestListingFails(t *testing.T) {
	w := newWorld(t)
	w.fail["show"] = failure{stderr: "Error: NetworkManager is not running.\n", exit: 8}
	wantCode(t, w.run("disconnect"), term.ExitFailure)
	w.wantErr("nord: nmcli could not list the connections: ", "is not running")
}

// E18: list-active lists nord's profiles and marks the connected one.
func TestListActive(t *testing.T) {
	w := newWorld(t)
	w.imported("nordvpn-uk1").active = "activated"
	w.imported("nordvpn-de2")
	w.imported("home-wifi")
	wantCode(t, w.run("list-active"), term.ExitOK)
	want := "Imported NetworkManager connections:\n\n  uk1 (connected)\n  de2\n"
	if got := w.stdout.String(); got != want {
		t.Errorf("stdout %q, want %q", got, want)
	}
}

// E18: none.
func TestListActiveNone(t *testing.T) {
	w := newWorld(t)
	wantCode(t, w.run("list-active"), term.ExitOK)
	want := "Imported NetworkManager connections:\n\n  (none)\n\nUse 'nord connect <name>' to import and connect.\n"
	if got := w.stdout.String(); got != want {
		t.Errorf("stdout %q, want %q", got, want)
	}
}

// E19: status names the connection, its device and the public address.
func TestStatusConnected(t *testing.T) {
	w := newWorld(t)
	p := w.imported("nordvpn-uk1")
	p.active, p.device = "activated", "wlp1s0"
	w.curlOut = "203.0.113.7\n"
	wantCode(t, w.run("status"), term.ExitOK)
	want := "Connected: uk1\nDevice: wlp1s0\nPublic IP: 203.0.113.7\n"
	if got := w.stdout.String(); got != want {
		t.Errorf("stdout %q, want %q", got, want)
	}
}

// E19: a connection not yet up is named by its state, and no address is asked.
func TestStatusActivating(t *testing.T) {
	w := newWorld(t)
	w.imported("nordvpn-uk1").active = "activating"
	wantCode(t, w.run("status"), term.ExitOK)
	if got, want := w.stdout.String(), "Connecting: uk1 (activating)\n"; got != want {
		t.Errorf("stdout %q, want %q", got, want)
	}
	if w.fake.Called("curl") {
		t.Error("curl asked while nothing is activated")
	}
}

// E19: an address that cannot be had is unknown, with why; status still exits 0.
func TestStatusAddressUnknown(t *testing.T) {
	cases := map[string]func(*world){
		"curl failed (exit 28): curl: (28) timed out": func(w *world) {
			w.curl = failure{stderr: "curl: (28) timed out\n", exit: 28}
		},
		"not an IP address: \"<html>\"": func(w *world) { w.curlOut = "<html>" },
		"curl is not installed":         func(w *world) { delete(w.installed, "curl") },
	}
	for why, set := range cases {
		w := newWorld(t)
		w.imported("nordvpn-uk1").active = "activated"
		set(w)
		wantCode(t, w.run("status"), term.ExitOK)
		w.wantOut("Public IP: unknown (" + why + ")\n")
	}
}

// E19: not connected.
func TestStatusNotConnected(t *testing.T) {
	w := newWorld(t)
	w.imported("nordvpn-uk1")
	wantCode(t, w.run("status"), term.ExitFailure)
	if got := w.stdout.String(); got != "Not connected\n" {
		t.Errorf("stdout %q", got)
	}
}

// E20: cleanup deletes every nord profile by UUID and counts them.
func TestCleanup(t *testing.T) {
	w := newWorld(t)
	a := w.imported("nordvpn-uk1")
	a.active = "activated"
	b := w.imported("nordvpn-de2")
	w.imported("home-wifi")
	wantCode(t, w.run("cleanup"), term.ExitOK)
	wantChanges(t, w, "nmcli connection delete uuid "+a.uuid, "nmcli connection delete uuid "+b.uuid)
	w.wantErr("Removing all nordvpn-* connections from NetworkManager...\n",
		"  Removing nordvpn-uk1 (connected: this disconnects it)\n", "  Removing nordvpn-de2\n",
		"Removed 2 nordvpn connection(s).\n")
	if len(w.profiles) != 1 {
		t.Errorf("profiles %+v", w.profiles)
	}
}

// E20: none.
func TestCleanupNothing(t *testing.T) {
	w := newWorld(t)
	wantCode(t, w.run("cleanup"), term.ExitOK)
	w.wantErr("No nordvpn connections found.\n")
}

// E20: a delete that fails is counted and the run fails.
func TestCleanupFailure(t *testing.T) {
	w := newWorld(t)
	w.imported("nordvpn-uk1")
	w.imported("nordvpn-de2")
	w.fail["delete"] = failure{stderr: "Error: busy\n", exit: 1}
	wantCode(t, w.run("cleanup"), term.ExitFailure)
	if len(w.changes()) != 2 {
		t.Errorf("changes %q", w.changes())
	}
	w.wantErr("nord: could not remove nordvpn-uk1: ", "busy", "nord: removed 0 connection(s); 2 could not be removed.\n")
}
