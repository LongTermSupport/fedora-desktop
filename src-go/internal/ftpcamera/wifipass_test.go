package ftpcamera

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E67: the shipped word list is short common lowercase words, each once, enough of them, and any
// three of them joined make a WPA2 passphrase (8 to 63 characters).
func TestHotspotWordList(t *testing.T) {
	words, err := hotspotWords()
	if err != nil {
		t.Fatal(err)
	}
	if len(words) < minHotspotWords {
		t.Fatalf("%d words, want at least %d", len(words), minHotspotWords)
	}
	shape := regexp.MustCompile(`^[a-z]{3,9}$`)
	seen := map[string]bool{}
	shortest, longest := 99, 0
	for _, w := range words {
		if !shape.MatchString(w) {
			t.Errorf("word %q is not 3 to 9 lowercase letters", w)
		}
		if seen[w] {
			t.Errorf("word %q is listed twice", w)
		}
		seen[w] = true
		shortest, longest = min(shortest, len(w)), max(longest, len(w))
	}
	sep := len(hotspotSeparator) * (hotspotPasswordWords - 1)
	if n := shortest*hotspotPasswordWords + sep; n < 8 {
		t.Errorf("the shortest password is %d characters, WPA2 needs 8", n)
	}
	if n := longest*hotspotPasswordWords + sep; n > 63 {
		t.Errorf("the longest password is %d characters, WPA2 allows 63", n)
	}
}

// E67: a list that breaks the rule is refused rather than used.
func TestParseHotspotWordsRefusals(t *testing.T) {
	for _, text := range []string{"apple apple", "Apple", "ab", "abcdefghij", "café"} {
		if _, err := parseHotspotWords(text, 1); err == nil {
			t.Errorf("%q accepted", text)
		}
	}
	if _, err := parseHotspotWords("apple pear", 3); err == nil {
		t.Error("two words accepted where three are needed")
	}
	got, err := parseHotspotWords("apple\npear  plum\n", 3)
	if err != nil || strings.Join(got, ",") != "apple,pear,plum" {
		t.Fatalf("%q %v", got, err)
	}
}

// E67: each word is drawn uniformly: two bytes, big-endian, a draw at or above the largest
// multiple of the list's length is drawn again, then the remainder is the index.
func TestNewHotspotPasswordDraws(t *testing.T) {
	words := []string{"amber", "birch", "cedar", "delta", "ember"}
	// 65536 % 5 == 1, so 65535 is redrawn.
	random := bytes.NewReader([]byte{0x00, 0x02, 0xFF, 0xFF, 0x00, 0x06, 0x00, 0x04})
	got, err := newHotspotPassword(random, words)
	if err != nil {
		t.Fatal(err)
	}
	if got != "cedar-birch-ember" {
		t.Fatalf("got %q", got)
	}
}

// E67: a random source that fails is an error, never a weaker password.
func TestNewHotspotPasswordRandomFails(t *testing.T) {
	if _, err := newHotspotPassword(bytes.NewReader([]byte{0x00}), []string{"amber", "birch"}); err == nil {
		t.Fatal("a short read gave a password")
	}
	if _, err := newHotspotPassword(nil, []string{"amber"}); err == nil {
		t.Fatal("no random source gave a password")
	}
}

// E67: --new-hotspot-password prints one password and nothing else, needs no camera account,
// runs no program and takes no suspend lock.
func TestNewHotspotPasswordMode(t *testing.T) {
	k := newKit(t)
	k.path("systemd-inhibit")
	k.fake.On([]string{"getent"}, execx.Result{ExitCode: 2})
	k.random = bytes.NewReader([]byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x02})
	k.wantCode(k.run("--new-hotspot-password"), term.ExitOK)
	words, err := hotspotWords()
	if err != nil {
		t.Fatal(err)
	}
	if want := words[0] + "-" + words[1] + "-" + words[2] + "\n"; k.out.String() != want {
		t.Fatalf("stdout %q, want %q", k.out.String(), want)
	}
	if k.err.Len() != 0 {
		t.Errorf("stderr %q", k.err.String())
	}
	if calls := k.fake.Calls(); len(calls) != 0 {
		t.Errorf("ran %v", calls)
	}
	if execs := k.fake.Execs(); len(execs) != 0 {
		t.Errorf("exec'd %v", execs)
	}
}

// E67: no random bytes: exit 1, nothing on stdout.
func TestNewHotspotPasswordModeRandomFails(t *testing.T) {
	k := newKit(t)
	k.random = errReader{}
	k.wantCode(k.run("--new-hotspot-password"), term.ExitFailure)
	k.wantErr("ERROR: could not make a hotspot password: read random bytes: no entropy\n")
	if k.out.Len() != 0 {
		t.Errorf("stdout %q", k.out.String())
	}
}

// E4, E5: it is a primary mode of its own, and takes no server modifier.
func TestNewHotspotPasswordModeRefusals(t *testing.T) {
	k := newKit(t)
	k.wantCode(k.run("--new-hotspot-password", "--sort"), exitUsage)
	k.wantErr("ERROR: only one primary mode at a time\n")
	k.wantCode(k.run("--new-hotspot-password", "--hotspot"), exitUsage)
	k.wantErr("ERROR: --hotspot requires an FTP-server mode\n")
}

// testWifiPass is the hotspot password the fake NetworkManager profile holds.
const testWifiPass = "maple-river-candle"

// pskArgv is the read of the hotspot kit's profile's Wi-Fi password, after the given leading
// words.
func pskArgv(lead ...string) []string {
	return append(lead, "-s", "-g", "802-11-wireless-security.psk", "connection", "show", "Camera AP")
}

// E68: the password is read as the user first; NetworkManager hands it over, and root is not
// asked.
func TestHotspotPasswordReadAsUser(t *testing.T) {
	k := hotspotKit(t, true)
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx, "--hotspot"), term.ExitOK)
	k.wantOut("    Wi-Fi:       ftp-camera   (join this network on the camera first)\n",
		"    Wi-Fi pass:  "+testWifiPass+"\n")
	k.wantNotRan("sudo -n nmcli -s")
	if strings.Contains(k.err.String(), testWifiPass) {
		t.Error("the Wi-Fi password went to stderr")
	}
}

// E68: NetworkManager keeps the secret from the user (an empty answer): it is read as root.
func TestHotspotPasswordReadAsRoot(t *testing.T) {
	k := hotspotKit(t, true)
	k.fake.On(pskArgv("nmcli"), execx.Result{})
	k.fake.On(pskArgv("sudo", "-n", "nmcli"), execx.Result{Stdout: []byte(testWifiPass + "\n")})
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx, "--hotspot"), term.ExitOK)
	k.wantRan(strings.Join(pskArgv("sudo", "-n", "nmcli"), " "))
	k.wantOut("    Wi-Fi pass:  " + testWifiPass + "\n")
}

// E68: the user's read failing is not an answer either; root's failing stops the session
// before vsftpd starts, and it tears down.
func TestHotspotPasswordUnreadable(t *testing.T) {
	k := hotspotKit(t, false)
	k.fake.On(pskArgv("nmcli"), execx.Result{ExitCode: 1, Stderr: []byte("Error: not authorized\n")})
	k.fake.On(pskArgv("sudo", "-n", "nmcli"), execx.Result{ExitCode: 4, Stderr: []byte("Error: no secrets\n")})
	k.wantCode(k.run("--hotspot"), term.ExitFailure)
	k.wantErr("ERROR: could not read the Wi-Fi password of hotspot 'Camera AP': ")
	k.wantNotRan("sudo -n systemctl start")
	k.wantRan("sudo -n nmcli connection down Camera AP")
}

// E68: a profile with no password (an open network) says so rather than printing nothing.
func TestHotspotPasswordNoneSet(t *testing.T) {
	k := hotspotKit(t, true)
	k.fake.On(pskArgv("nmcli"), execx.Result{})
	k.fake.On(pskArgv("sudo", "-n", "nmcli"), execx.Result{})
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx, "--hotspot"), term.ExitOK)
	k.wantOut("    Wi-Fi pass:  (none: profile 'Camera AP' has no Wi-Fi password)\n")
}

// E68: a shared connection with no SSID (wired) has no Wi-Fi to join: no rows, no read.
func TestHotspotPasswordWiredShared(t *testing.T) {
	k := hotspotKit(t, true)
	k.fake.On([]string{"nmcli", "-g", "802-11-wireless.ssid", "connection", "show", "Camera AP"}, execx.Result{})
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx, "--hotspot"), term.ExitOK)
	k.wantNotRan("nmcli -s")
	k.wantNotRan("sudo -n nmcli -s")
	if strings.Contains(k.out.String(), "Wi-Fi pass:") {
		t.Error("a wired shared connection printed a Wi-Fi password row")
	}
}

// E68: no hotspot, no Wi-Fi rows.
func TestNoHotspotNoWifiRows(t *testing.T) {
	k := serverKit(t)
	k.feedOn(true, tailArgv)
	k.feedOn(false, []string{"inotifywait"})
	ctx := interruptAt(k)
	k.wantCode(k.runCtx(ctx), term.ExitOK)
	if strings.Contains(k.out.String(), "Wi-Fi") {
		t.Error("printed Wi-Fi rows with no hotspot")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }
