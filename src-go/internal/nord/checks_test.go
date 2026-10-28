package nord

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const playPath = "./playbooks/imports/optional/common/play-nordvpn-openvpn.yml"

// E8, E9: connect reports every missing piece, not only the first, and runs nothing.
func TestConnectReportsEveryMissingPiece(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	delete(w.installed, "openvpn")
	w.nmRunning = false
	if err := os.Remove(filepath.Join(w.home, ".config/nordvpn/.credentials")); err != nil {
		t.Fatal(err)
	}
	wantCode(t, w.run("connect", "uk1"), term.ExitFailure)
	w.wantErr("nord: NetworkManager is not running; nord manages its connections through it.\n",
		"nord: openvpn is not installed; run "+playPath,
		"nord: ~/.config/nordvpn/.credentials: missing",
	)
	wantChanges(t, w)
}

// E9: no nmcli; systemd that cannot be asked.
func TestNmcliMissingAndSystemdUnreachable(t *testing.T) {
	w := newWorld(t)
	delete(w.installed, "nmcli")
	w.systemctlFails = true
	wantCode(t, w.run("status"), term.ExitFailure)
	w.wantErr("nord: nmcli is not installed; nord manages its connections through NetworkManager.\n",
		"nord: could not ask systemd whether NetworkManager is running: ")
	if w.stdout.Len() != 0 {
		t.Errorf("stdout %q", w.stdout.String())
	}
}

// E8, B6: status needs neither openvpn nor the configs.
func TestStatusNeedsOnlyNetworkManager(t *testing.T) {
	w := newWorld(t)
	delete(w.installed, "openvpn")
	if err := os.RemoveAll(filepath.Join(w.home, ".config/nordvpn")); err != nil {
		t.Fatal(err)
	}
	wantCode(t, w.run("status"), term.ExitFailure)
	if got := w.stdout.String(); got != "Not connected\n" {
		t.Errorf("stdout %q", got)
	}
	w.wantNoErr("openvpn", "configs")
}

// E10: credentials that are wrong in each way fail before anything is imported.
func TestBadCredentials(t *testing.T) {
	const file = "nord: ~/.config/nordvpn/.credentials: "
	cases := map[string]string{
		"":                       file + "the username (line 1) is empty",
		"user\n":                 file + "the password (line 2) is empty",
		"user":                   file + "the password (line 2) is empty",
		"\npass\n":               file + "the username (line 1) is empty",
		"user\npass\nmore\n":     file + "a third line; expected the username on line 1 and the password on line 2 only",
		"user\n\n":               file + "the password (line 2) is empty",
		"user\nhunter2\n\n \n\n": "",
		"user\r\npass word\r\n":  "",
	}
	for content, want := range cases {
		w := newWorld(t)
		w.config("uk1.ovpn")
		w.file(".config/nordvpn/.credentials", content)
		code := w.run("connect", "uk1")
		if want == "" {
			if code != term.ExitOK {
				t.Errorf("%q: exit %d, want 0:\n%s", content, code, w.stderr.String())
			}
			continue
		}
		wantCode(t, code, term.ExitFailure)
		w.wantErr(want, playPath)
		wantChanges(t, w)
	}
}

// E10, E14: the values read are exactly the lines, less the line ending, and the password goes
// to nmcli's editor on its standard input, to be stored in the profile.
func TestCredentialsAreTheLines(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	w.file(".config/nordvpn/.credentials", testUser+"\r\n"+testPassword+"\r\n")
	wantCode(t, w.run("connect", "uk1"), term.ExitOK)
	if len(w.profiles) != 1 || w.profiles[0].username != testUser {
		t.Errorf("profiles %+v", w.profiles)
	}
	if len(w.fed) != 1 || w.fed[0] != editFeed("<password>") {
		t.Errorf("fed %q", w.fed)
	}
	w.wantNoPassword()
}

// E13: the password is stored in nmcli's VPN secrets setting, under the key password.
func TestPasswordSettingIsTheVPNSecrets(t *testing.T) {
	if vpnSetting != "vpn" || vpnSecretsProperty != "secrets" || vpnPasswordKey != "password" {
		t.Errorf("setting %q.%q key %q", vpnSetting, vpnSecretsProperty, vpnPasswordKey)
	}
}
