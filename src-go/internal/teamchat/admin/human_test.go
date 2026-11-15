package admin

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

var humanPassword = regexp.MustCompile(`^[A-Za-z0-9]{32}$`)

// humanDoc is a human verb's JSON answer (HU2, HU4, HU5), no unknown key allowed.
type humanDoc struct {
	V        int              `json:"v"`
	Team     string           `json:"team"`
	Name     string           `json:"name"`
	Changed  []string         `json:"changed"`
	Password *string          `json:"password"`
	Devices  []map[string]any `json:"devices"`
}

func decodeHumanDoc(t *testing.T, stdout string) humanDoc {
	t.Helper()
	if strings.Count(stdout, "\n") != 1 || !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("stdout is not one line: %q", stdout)
	}
	dec := json.NewDecoder(strings.NewReader(stdout))
	dec.DisallowUnknownFields()
	var d humanDoc
	if err := dec.Decode(&d); err != nil || d.V != 1 || d.Team != "alpha" || d.Name != "alice" {
		t.Fatalf("answer %q: %v", stdout, err)
	}
	return d
}

func humanArgs(verb string) []string {
	return []string{"human", verb, "--team", "alpha", "--name", "alice"}
}

// noFileHolds fails the test when any file under the world's root holds secret (HU2, HV8).
func (w *world) noFileHolds(t *testing.T, secret string) {
	t.Helper()
	tree := os.DirFS(w.root)
	err := fs.WalkDir(tree, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		raw, err := fs.ReadFile(tree, path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), secret) {
			t.Errorf("%s holds the password", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (w *world) signsInAsAlice(t *testing.T, password, device string) bool {
	t.Helper()
	_, err := w.admin.LoginDevice(t.Context(), "alice", password, device)
	return err == nil
}

// HU2: `human password` prints a new 32-character password once, which signs alice in, and
// keeps it nowhere: no file, no stderr line.
func TestHumanPasswordThroughTheVerb(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	out := w.ok(t, "", humanArgs("password")...)
	d := decodeHumanDoc(t, out.stdout)
	if d.Password == nil || !humanPassword.MatchString(*d.Password) || d.Devices != nil {
		t.Fatalf("answer %q", out.stdout)
	}
	if !strings.Contains(strings.Join(d.Changed, "\n"), "human alice password set") {
		t.Errorf("changed = %v", d.Changed)
	}
	if !w.signsInAsAlice(t, *d.Password, "PHONE") {
		t.Error("the printed password does not sign alice in")
	}
	if strings.Contains(out.stderr, *d.Password) {
		t.Error("stderr holds the password")
	}
	w.noFileHolds(t, *d.Password)
	w.lockIsFree(t)
}

// HU2 on a terminal: the password is shown on the alternate screen and cleared on Enter.
func TestAHumanPasswordOnATerminalLeavesNoCopy(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	out := w.verbOn(strings.NewReader("\n"), onTerminal, humanArgs("password")...)
	body, shown := strings.CutPrefix(out.stdout, term.AltScreenOn)
	body, left := strings.CutSuffix(body, humanClearPrompt+term.AltScreenOff)
	if out.code != term.ExitOK || !shown || !left {
		t.Fatalf("not on the alternate screen: %+v", out)
	}
	if d := decodeHumanDoc(t, body); d.Password == nil || !w.signsInAsAlice(t, *d.Password, "PHONE") {
		t.Fatalf("password on a terminal %+v", out)
	}
	logout := w.verbOn(strings.NewReader(""), onTerminal, humanArgs("logout")...)
	if logout.code != term.ExitOK || strings.Contains(logout.stdout, "\x1b") {
		t.Fatalf("a document with no password is not plain: %+v", logout)
	}
}

// HU4: `human logout` signs every device out and prints no password.
func TestHumanLogoutThroughTheVerb(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	pw := *decodeHumanDoc(t, w.ok(t, "", humanArgs("password")...).stdout).Password
	if !w.signsInAsAlice(t, pw, "PHONE") {
		t.Fatal("no sign-in")
	}
	d := decodeHumanDoc(t, w.ok(t, "", humanArgs("logout")...).stdout)
	if d.Password != nil || d.Devices != nil || !strings.Contains(strings.Join(d.Changed, "\n"), "human alice signed out") {
		t.Fatalf("logout answered %+v", d)
	}
	if w.signsInAsAlice(t, pw, "AGAIN") {
		t.Error("the old password still signs in after logout")
	}
}

// HU5: `human devices` lists the account's devices by ID.
func TestHumanDevicesThroughTheVerb(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	pw := *decodeHumanDoc(t, w.ok(t, "", humanArgs("password")...).stdout).Password
	for _, device := range []string{"TABLET", "PHONE"} {
		if !w.signsInAsAlice(t, pw, device) {
			t.Fatal("no sign-in")
		}
	}
	out := w.ok(t, "", humanArgs("devices")...)
	d := decodeHumanDoc(t, out.stdout)
	if len(d.Devices) != 2 || d.Devices[0]["id"] != "PHONE" || d.Devices[1]["id"] != "TABLET" || d.Password != nil {
		t.Fatalf("devices %q", out.stdout)
	}
	for _, dev := range d.Devices {
		if _, seen := dev["last_seen_ms"]; !seen || len(dev) != 2 {
			t.Errorf("a device is not an id and a last_seen_ms: %v", dev)
		}
	}
	if strings.Contains(out.stdout, `"changed"`) {
		t.Error("devices answered changed lines")
	}
}

// HU1, HV4: a bad or missing flag is 64 before any seam; an undeclared human is 70.
func TestHumanVerbRefusals(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	cases := map[string]struct {
		args []string
		code term.ExitCode
	}{
		"no name":    {[]string{"human", "password", "--team", "alpha"}, term.ExitUsage},
		"bad name":   {[]string{"human", "password", "--team", "alpha", "--name", "Al!ce"}, term.ExitUsage},
		"no team":    {[]string{"human", "password", "--name", "alice"}, term.ExitUsage},
		"undeclared": {[]string{"human", "devices", "--team", "alpha", "--name", "carol"}, ExitRefused},
		"stray":      {append(humanArgs("logout"), "extra"), term.ExitUsage},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			out := w.verb("", c.args...)
			if out.code != c.code || out.stdout != "" {
				t.Fatalf("%+v, want %d", out, c.code)
			}
		})
	}
}

// HU1: a team that is not applied is refused 70.
func TestHumanVerbOfATeamNotApplied(t *testing.T) {
	w := newWorld(t)
	if out := w.verb("", humanArgs("devices")...); out.code != ExitRefused || !strings.Contains(out.stderr, "not applied") {
		t.Fatalf("%+v", out)
	}
}

// HV7, HU13: a child's answer that is not what it was asked is refused, and no part of a lying
// password reaches stdout or stderr.
func TestAHumanAnswerIsCheckedAgainstTheRequest(t *testing.T) {
	const short = "tooshort"
	cases := map[string]struct {
		verb string
		lie  func(*Answer)
	}{
		"a handle":               {"password", func(a *Answer) { a.Handle = coord }},
		"a short password":       {"password", func(a *Answer) { a.Human.Password = short }},
		"no password":            {"password", func(a *Answer) { a.Human = nil }},
		"a password for logout":  {"logout", func(a *Answer) { a.Human = &HumanAnswer{Password: strings.Repeat("a", 32)} }},
		"devices for a password": {"password", func(a *Answer) { a.Human = &HumanAnswer{Devices: []matrix.Device{{ID: "X"}}} }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.applied(t)
			w.lie = c.lie
			out := w.verb("", humanArgs(c.verb)...)
			if out.code != term.ExitFailure || out.stdout != "" || !strings.Contains(out.stderr, "not what it was asked") {
				t.Fatalf("%+v", out)
			}
			if strings.Contains(out.stderr, short) {
				t.Error("stderr quotes the lying password")
			}
		})
	}
}

// HV8: the password exists once the reset is done, so a failed status refresh still prints it.
func TestAFailedStatusRefreshStillPrintsThePassword(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	w.statErr = errors.New("disk full")
	out := w.verb("", humanArgs("password")...)
	if out.code != term.ExitFailure || decodeHumanDoc(t, out.stdout).Password == nil || !strings.Contains(out.stderr, "disk full") {
		t.Fatalf("%+v", out)
	}
}
