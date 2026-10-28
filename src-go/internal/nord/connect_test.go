package nord

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const configsDir = ".config/nordvpn/configs/"

// E13, E14: a first connect imports, names the profile, stores the username and the password
// (fed to nmcli's editor on stdin), then brings it up.
func TestFirstConnectImports(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.nordvpn.com.udp.ovpn")
	wantCode(t, w.run("connect", "uk1"), term.ExitOK)
	uuid := "00000000-0000-4000-8000-000000000001"
	wantChanges(t, w,
		"nmcli connection import type openvpn file "+w.home+"/"+configsDir+"uk1.nordvpn.com.udp.ovpn",
		"nmcli connection modify uuid "+uuid+" connection.id nordvpn-uk1 +vpn.data username="+testUser,
		"nmcli connection edit uuid "+uuid,
		"nmcli connection up uuid "+uuid,
	)
	w.wantErr("Importing uk1...\n", "Imported nordvpn-uk1.\n", "Connecting to uk1...\n", "Connected to uk1.\n")
	if p := w.profileAt(0); p.name != "nordvpn-uk1" || p.active != "activated" || !p.stored {
		t.Errorf("profile %+v", p)
	}
	if w.stdout.Len() != 0 {
		t.Errorf("stdout %q (E23)", w.stdout.String())
	}
	w.wantNoPassword()
}

// E13: an imported profile is brought up without another import.
func TestConnectUsesTheImportedProfile(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	p := w.imported("nordvpn-uk1")
	wantCode(t, w.run("connect", "uk1"), term.ExitOK)
	wantChanges(t, w, "nmcli connection up uuid "+p.uuid)
	w.wantNoErr("Importing")
}

// E11: a connection of another type, or without the prefix, is not nord's.
func TestOnlyPrefixedVPNProfilesAreNords(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	wifi := w.imported("nordvpn-uk1")
	wifi.kind = "802-11-wireless"
	w.imported("uk1")
	wantCode(t, w.run("connect", "uk1"), term.ExitOK)
	if len(w.profiles) != 3 || w.changes()[0] != "nmcli connection import type openvpn file "+w.home+"/"+configsDir+"uk1.ovpn" {
		t.Errorf("changes %q", w.changes())
	}
}

// E11: the terse listing is unescaped.
func TestTerseFieldsAreUnescaped(t *testing.T) {
	w := newWorld(t)
	w.imported(`nordvpn-a:b\c`).active = "activated"
	wantCode(t, w.run("list-active"), term.ExitOK)
	w.wantOut(`  a:b\c (connected)` + "\n")
}

// E13: a failed import shows nmcli's output and changes nothing more.
func TestFailedImport(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	w.fail["import"] = failure{stderr: "Error: failed to import: plugin missing\n", exit: 1}
	wantCode(t, w.run("connect", "uk1"), term.ExitFailure)
	w.wantErr("nord: could not import uk1: ", "plugin missing")
	if len(w.changes()) != 1 {
		t.Errorf("changes %q", w.changes())
	}
}

// E13: an import whose output names no UUID fails, naming what nmcli said.
func TestImportWithoutUUID(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	odd := "Connection imported.\n"
	w.importOut = &odd
	wantCode(t, w.run("connect", "uk1"), term.ExitFailure)
	w.wantErr("nord: nmcli imported uk1 but named no connection UUID: Connection imported.")
	if len(w.changes()) != 1 {
		t.Errorf("changes %q", w.changes())
	}
}

// E13, B9: a failed configuration deletes the imported profile.
func TestFailedModifyDeletesTheImport(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	w.fail["modify"] = failure{stderr: "Error: invalid property\n", exit: 2}
	wantCode(t, w.run("connect", "uk1"), term.ExitFailure)
	uuid := "00000000-0000-4000-8000-000000000001"
	wantChanges(t, w,
		"nmcli connection import type openvpn file "+w.home+"/"+configsDir+"uk1.ovpn",
		"nmcli connection modify uuid "+uuid+" connection.id nordvpn-uk1 +vpn.data username="+testUser,
		"nmcli connection delete uuid "+uuid,
	)
	w.wantErr("nord: could not configure the imported uk1: ", "invalid property", "Removed the half-imported connection.")
	if len(w.profiles) != 0 {
		t.Errorf("profiles left %+v", w.profiles)
	}
}

// E13: a rollback that fails is named too.
func TestFailedRollbackIsNamed(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	w.fail["modify"] = failure{stderr: "Error: invalid property\n", exit: 2}
	w.fail["delete"] = failure{stderr: "Error: busy\n", exit: 1}
	wantCode(t, w.run("connect", "uk1"), term.ExitFailure)
	w.wantErr("nord: could not remove the half-imported connection 00000000-0000-4000-8000-000000000001: ", "busy")
}

// E12: two profiles of one name are refused before anything changes.
func TestDuplicateProfilesAreRefused(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	a, b := w.imported("nordvpn-uk1"), w.imported("nordvpn-uk1")
	wantCode(t, w.run("connect", "uk1"), term.ExitFailure)
	w.wantErr("nord: two NetworkManager connections are named nordvpn-uk1 (" + a.uuid + ", " + b.uuid +
		"); run 'nord cleanup' and connect again.\n")
	wantChanges(t, w)
}

// E14: another nord connection is brought down first.
func TestConnectBringsTheOtherDown(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn", "de2.ovpn")
	de := w.imported("nordvpn-de2")
	de.active = "activated"
	uk := w.imported("nordvpn-uk1")
	wantCode(t, w.run("connect", "uk1"), term.ExitOK)
	wantChanges(t, w,
		"nmcli connection down uuid "+de.uuid,
		"nmcli connection up uuid "+uk.uuid,
	)
	w.wantErr("Disconnecting de2...\n", "Connected to uk1.\n")
}

// E14, B10: a connection that cannot be brought down stops the connect.
func TestFailedDownStopsTheConnect(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn", "de2.ovpn")
	de := w.imported("nordvpn-de2")
	de.active = "activated"
	w.imported("nordvpn-uk1")
	w.fail["down"] = failure{stderr: "Error: not permitted\n", exit: 4}
	wantCode(t, w.run("connect", "uk1"), term.ExitFailure)
	wantChanges(t, w, "nmcli connection down uuid "+de.uuid)
	w.wantErr("nord: could not disconnect de2: ", "not permitted", "; not connecting uk1 beside it.\n")
}

// E14, B11: a failed connect shows nmcli's output and the troubleshooting, never the password.
func TestFailedUp(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	w.imported("nordvpn-uk1")
	w.fail["up"] = failure{stderr: "Error: Connection activation failed: auth failed\n", exit: 4}
	wantCode(t, w.run("connect", "uk1"), term.ExitFailure)
	w.wantErr("nord: could not connect to uk1: ", "auth failed",
		"journalctl -u NetworkManager --no-pager --since '5 minutes ago'",
		"nord cleanup && nord connect uk1")
	w.wantNoErr("cat ", "journalctl -u NetworkManager -f")
	w.wantNoPassword()
}

// E15: an active profile is not brought up again.
func TestAlreadyConnected(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	w.imported("nordvpn-uk1").active = "activated"
	wantCode(t, w.run("connect", "uk1"), term.ExitOK)
	wantChanges(t, w)
	w.wantErr("Already connected to uk1.\n")
}

// E16, B12: switch to a name that does not exist leaves the connection up.
func TestSwitchToNothingKeepsTheConnection(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	w.imported("nordvpn-uk1").active = "activated"
	wantCode(t, w.run("switch", "nosuch"), term.ExitFailure)
	wantChanges(t, w)
}

// E16: switch is connect.
func TestSwitch(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn", "de2.ovpn")
	uk := w.imported("nordvpn-uk1")
	uk.active = "activated"
	wantCode(t, w.run("switch", "de2"), term.ExitOK)
	de := w.profileAt(1)
	wantChanges(t, w,
		"nmcli connection import type openvpn file "+w.home+"/"+configsDir+"de2.ovpn",
		"nmcli connection modify uuid "+de.uuid+" connection.id nordvpn-de2 +vpn.data username="+testUser,
		"nmcli connection edit uuid "+de.uuid,
		"nmcli connection down uuid "+uk.uuid,
		"nmcli connection up uuid "+de.uuid,
	)
}

// E13, B9: an edit session that fails, or does not say it saved, deletes the imported profile.
func TestFailedPasswordStoreDeletesTheImport(t *testing.T) {
	// nmcli's words naming the password are shown with it hidden.
	odd := "Error: invalid value '" + testPassword + "'\n"
	for name, set := range map[string]func(*world){
		"failed":    func(w *world) { w.fail["edit"] = failure{stderr: "Error: not permitted\n", exit: 1} },
		"not saved": func(w *world) { w.editOut = &odd },
	} {
		w := newWorld(t)
		w.config("uk1.ovpn")
		set(w)
		wantCode(t, w.run("connect", "uk1"), term.ExitFailure)
		uuid := "00000000-0000-4000-8000-000000000001"
		got := w.changes()
		if len(got) != 4 || got[2] != "nmcli connection edit uuid "+uuid || got[3] != "nmcli connection delete uuid "+uuid {
			t.Errorf("%s: changes %q", name, got)
		}
		w.wantErr("nord: could not store the password in nordvpn-uk1: ", "Removed the half-imported connection.")
		w.wantNoPassword()
	}
}
