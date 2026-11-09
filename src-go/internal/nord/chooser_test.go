package nord

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E22: not connected, a numbered server menu under its heading; a choice connects.
func TestChooserConnects(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn", "de2.ovpn")
	w.typed("2")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("nord › Connect", "2 servers", "  1) de2\n", "  2) uk1\n", "  q) Cancel\n",
		"Choose a server [1-2, q]: ", "Connected to uk1.\n")
	if len(w.profiles) != 1 || w.profiles[0].name != "nordvpn-uk1" || w.profiles[0].active != "activated" {
		t.Errorf("profiles %+v", w.profiles)
	}
	if w.stdout.Len() != 0 {
		t.Errorf("stdout %q (E23)", w.stdout.String())
	}
}

// E22: q cancels, changing nothing.
func TestChooserCancel(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	w.typed("q")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("Nothing changed.\n")
	wantChanges(t, w)
}

// E22, B14: a bad key is asked again; three are the end, exit 1.
func TestChooserBadKeys(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	w.typed("9", "", "x")
	wantCode(t, w.run(), term.ExitFailure)
	if n := strings.Count(w.stderr.String(), "Choose one of: 1, q."); n != 3 {
		t.Errorf("%d complaints:\n%s", n, w.stderr.String())
	}
	w.wantErr("nord: no valid choice after 3 tries; nothing changed.\n")
	wantChanges(t, w)
}

// E22: a bad key then a good one connects.
func TestChooserRetryThenChoose(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	w.typed("7", "1")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("Connected to uk1.\n")
}

// E22: the end of input is a cancel, 130.
func TestChooserEndOfInput(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	w.tty = ""
	wantCode(t, w.run(), term.ExitCancelled)
	w.wantErr("The input ended before a choice; nothing changed.\n")
	wantChanges(t, w)
}

// E22: connected, switch, disconnect or cancel; switch marks the connected server.
func TestChooserSwitch(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn", "de2.ovpn")
	uk := w.imported("nordvpn-uk1")
	uk.active = "activated"
	w.typed("1", "1")
	wantCode(t, w.run(), term.ExitOK)
	w.wantErr("connected to uk1", "  1) Switch to another server\n", "  2) Disconnect\n", "  q) Cancel\n",
		"Choose [1, 2, q]: ", "nord › Switch", "  2) uk1 (connected)\n", "Connected to de2.\n")
	de := w.profileAt(1)
	if de.active != "activated" || uk.active != "" {
		t.Errorf("profiles %+v %+v", uk, de)
	}
}

func TestChooserDisconnect(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	uk := w.imported("nordvpn-uk1")
	uk.active = "activated"
	w.typed("2")
	wantCode(t, w.run(), term.ExitOK)
	wantChanges(t, w, "nmcli connection down uuid "+uk.uuid)
	w.wantErr("Disconnected uk1.\n")
}

// E22: no terminal is an error before anything runs.
func TestChooserNeedsATerminal(t *testing.T) {
	for _, set := range []func(*world){
		func(w *world) { w.stdinTTY = false },
		func(w *world) { w.noTTY = true },
	} {
		w := newWorld(t)
		w.config("uk1.ovpn")
		set(w)
		wantCode(t, w.run(), term.ExitFailure)
		w.wantErr("nord: the chooser needs an interactive terminal; see 'nord --help' for the commands.\n")
		if len(w.fake.Calls()) != 0 {
			t.Errorf("calls %d", len(w.fake.Calls()))
		}
		if _, err := os.Stat(filepath.Join(w.home, ".local/share/nordvpn/nord.log")); !os.IsNotExist(err) {
			t.Errorf("the log was opened before the terminal was checked: %v", err)
		}
	}
}

// E22: no configs.
func TestChooserNoConfigs(t *testing.T) {
	w := newWorld(t)
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("nord: no VPN configs in ~/.config/nordvpn/configs/.\n", "Download .ovpn files from NordVPN")
}

// E22, E8: the chooser checks what connect needs.
func TestChooserChecks(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	delete(w.installed, "openvpn")
	wantCode(t, w.run(), term.ExitFailure)
	w.wantErr("nord: openvpn is not installed")
}
