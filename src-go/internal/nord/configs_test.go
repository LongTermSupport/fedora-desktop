package nord

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E5, E7: list names each config less .ovpn and the NordVPN suffix, in byte order.
func TestListNamesTheConfigs(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.nordvpn.com.udp.ovpn", "de7.nordvpn.com.tcp.ovpn", "home.ovpn", "notes.txt")
	w.dir(".config/nordvpn/configs/dir.ovpn")
	if err := os.Symlink(filepath.Join(w.home, ".config/nordvpn/configs/home.ovpn"),
		filepath.Join(w.home, ".config/nordvpn/configs/link.ovpn")); err != nil {
		t.Fatal(err)
	}
	wantCode(t, w.run("list"), term.ExitOK)
	want := "Available VPN configs:\n\n  de7\n  home\n  link\n  uk1\n\nTotal: 4 configs\n\n" +
		"To connect: nord connect <name>\nOr run 'nord' for the interactive chooser.\n"
	if got := w.stdout.String(); got != want {
		t.Errorf("stdout\n%s\nwant\n%s", got, want)
	}
	if len(w.fake.Calls()) != 0 {
		t.Errorf("list ran %d commands; it needs no NetworkManager (B6)", len(w.fake.Calls()))
	}
}

// E5: two configs that strip to one name each keep their long name.
func TestSharedShortNameKeepsTheLongNames(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.nordvpn.com.udp.ovpn", "uk1.nordvpn.com.tcp.ovpn", "fr2.nordvpn.com.udp.ovpn")
	wantCode(t, w.run("list"), term.ExitOK)
	w.wantOut("  fr2\n  uk1.nordvpn.com.tcp\n  uk1.nordvpn.com.udp\n\nTotal: 3 configs\n")
}

// E7: no configs prints the hint.
func TestListWithNoConfigs(t *testing.T) {
	w := newWorld(t)
	wantCode(t, w.run("list"), term.ExitOK)
	w.wantOut("Total: 0 configs\n", "Download .ovpn files from NordVPN and place them in:\n",
		"  ~/.config/nordvpn/configs/\n", "Visit: nordvpn.com → Dashboard → Downloads → OpenVPN configs\n")
}

// E7, E9: a missing configs directory names the play.
func TestListWithoutTheConfigsDirectory(t *testing.T) {
	w := newWorld(t)
	if err := os.RemoveAll(filepath.Join(w.home, ".config/nordvpn/configs")); err != nil {
		t.Fatal(err)
	}
	wantCode(t, w.run("list"), term.ExitFailure)
	w.wantErr("nord: the configs directory ~/.config/nordvpn/configs/ is missing",
		"./playbooks/imports/optional/common/play-nordvpn-openvpn.yml")
	if w.stdout.Len() != 0 {
		t.Errorf("stdout %q", w.stdout.String())
	}
}

// E6: the long form names the same config, and the profile takes the short name.
func TestLongNameIsTheSameConfig(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.nordvpn.com.udp.ovpn")
	wantCode(t, w.run("connect", "uk1.nordvpn.com.udp"), term.ExitOK)
	if len(w.profiles) != 1 || w.profiles[0].name != "nordvpn-uk1" {
		t.Errorf("profiles %+v", w.profiles)
	}
}

// E6: a shared short name is refused, naming both long names; nothing runs that changes anything.
func TestAmbiguousNameIsRefused(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.nordvpn.com.udp.ovpn", "uk1.nordvpn.com.tcp.ovpn")
	wantCode(t, w.run("connect", "uk1"), term.ExitFailure)
	w.wantErr("nord: 'uk1' names more than one config: uk1.nordvpn.com.tcp, uk1.nordvpn.com.udp; use one of those names.\n")
	wantChanges(t, w)
}

// E6, B2: an unknown name, a path included, names the configs and reads no file outside them.
func TestUnknownNameListsTheConfigs(t *testing.T) {
	for _, name := range []string{"nosuch", "../secret", "uk1.ovpn"} {
		w := newWorld(t)
		w.config("uk1.nordvpn.com.udp.ovpn")
		w.file(".config/nordvpn/secret.ovpn", "client\n")
		wantCode(t, w.run("connect", name), term.ExitFailure)
		w.wantErr("nord: no config named '"+name+"' in ~/.config/nordvpn/configs/.\n", "Available configs:\n  uk1\n")
		wantChanges(t, w)
	}
}
