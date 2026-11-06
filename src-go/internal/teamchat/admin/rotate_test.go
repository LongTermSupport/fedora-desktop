package admin

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The tests are Plan 00246 SPEC-cross-network X33 through the verb `team rotate-ca`.

const (
	rotLXCPrefix = "thing+desk.lxc"
	lxcSeat      = "thing.1+desk.lxc"
	placed       = "myrepo.3+desk.podman"
	parked       = "myrepo.4+desk.podman"
)

var rotateArgs = []string{"team", "rotate-ca", "--team", "alpha"}

// rotatable is alpha applied with three agent seats: one placed by the owner (enrolled), one
// parked, and one LXC seat the host's record holds placed.
func rotatable(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.applied(t)
	w.srv.AddUser(placed, "pw", false)
	w.srv.AddUser(parked, "pw", false)
	w.srv.AddUser(lxcSeat, "pw", false)
	w.ok(t, "", addArgs("3", "password")...)
	w.ok(t, "", addArgs("4", "token")...)
	w.ok(t, "", "member", "park", "--team", "alpha", "--handle", parked)
	w.ok(t, "", "member", "add", "--team", "alpha", "--prefix", rotLXCPrefix, "--seat", "1", "--kind", "lxc", "--human-text", "false", "--credential", "token")
	box, err := paths.ParseContainerName("box1")
	if err != nil {
		t.Fatal(err)
	}
	user, err := paths.ParseGuestUser("dev")
	if err != nil {
		t.Fatal(err)
	}
	rec := lxcstate.Record{V: 1, Team: alpha(t), Members: []lxcstate.Entry{
		{Container: box, Checkout: "/srv/thing", Seat: 1, User: user, Handle: lxcSeat, Status: lxcstate.StatusJoined, SinceMS: t0ms},
	}}
	if _, err = lxcstate.WriteRecord(w.root, rec, -1); err != nil {
		t.Fatal(err)
	}
	return w
}

// tlsAndRecord are the team's four TLS files and its LXC record, as bytes by path.
func tlsAndRecord(t *testing.T, w *world) map[string][]byte {
	t.Helper()
	team := alpha(t)
	out := map[string][]byte{}
	for _, p := range []string{paths.CAKeyFile(team), paths.CACertFile(team), paths.LeafKeyFile(team), paths.LeafCertFile(team), paths.LXCRecord(team)} {
		data, err := os.ReadFile(filepath.Clean(w.root + p))
		if err != nil {
			t.Fatal(err)
		}
		out[p] = data
	}
	return out
}

func caFingerprint(t *testing.T, w *world) string {
	t.Helper()
	data, err := os.ReadFile(w.root + paths.CACertFile(alpha(t)))
	if err != nil {
		t.Fatal(err)
	}
	fp, err := pki.Fingerprint(data)
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

// X33: stdout is the handles of the seats the host does not place, one per line and nothing
// else; stderr names both fingerprints, how each listed seat is placed again, the LXC seats the
// next lxc apply reissues, the Element play, and the backup taken before; no key is printed.
func TestX33TheVerbRotatesAndListsOnlyTheSeatsTheHostDoesNotPlace(t *testing.T) {
	w := rotatable(t)
	oldFP := caFingerprint(t, w)
	out := w.ok(t, "", rotateArgs...)
	newFP := caFingerprint(t, w)
	if out.stdout != placed+"\n" {
		t.Fatalf("stdout %q, want only %s", out.stdout, placed)
	}
	for _, want := range []string{
		"old CA sha256:" + oldFP, "new CA sha256:" + newFP,
		"member reissue --team alpha --handle <handle>", "seat place-issued",
		lxcSeat, "lxc apply", "play-agent-bus-element.yml", "rotate-ca again",
	} {
		if !strings.Contains(out.stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, out.stderr)
		}
	}
	if oldFP == newFP || strings.Contains(out.stdout+out.stderr, "PRIVATE KEY") {
		t.Fatal("the CA was kept, or a key was printed")
	}
	rec, found, err := lxcstate.ReadRecord(w.root, alpha(t))
	if err != nil || !found {
		t.Fatal(err)
	}
	if e, ok := rec.ByHandle(lxcSeat); !ok || e.Status != lxcstate.StatusDeferred || e.Why != lxc.WhyCARotated {
		t.Fatalf("the LXC seat's entry %+v", e)
	}
	issued, err := host.CAPEM(w.root, alpha(t))
	if err != nil {
		t.Fatal(err)
	}
	if fp, ferr := pki.Fingerprint([]byte(issued)); ferr != nil || fp != newFP {
		t.Fatalf("the CA a bundle or reach fact is issued with is %s, want %s (%v)", fp, newFP, ferr)
	}
	w.lockIsFree(t)
}

// X33: a failure at each write, landed or not, leaves the CA, the leaf and the LXC record as
// they were, prints no handle, and says nothing was replaced.
func TestX33TheVerbFailingAtEachWriteChangesNothing(t *testing.T) {
	const writes = 5
	for _, landed := range []bool{false, true} {
		for n := 1; n <= writes; n++ {
			w := rotatable(t)
			was := tlsAndRecord(t, w)
			w.rotateWrite = faultAt(n, landed)
			out := w.verb("", rotateArgs...)
			if out.code == term.ExitOK || out.stdout != "" || !strings.Contains(out.stderr, "nothing is replaced") {
				t.Fatalf("write %d (landed %v): %+v", n, landed, out)
			}
			unchanged(t, was, tlsAndRecord(t, w))
			w.lockIsFree(t)
		}
	}
}

// X33, HB3: while a restore is recorded the verb refuses as every lock-taking verb does, and
// changes nothing.
func TestX33TheVerbIsRefusedWhileARestoreIsRecorded(t *testing.T) {
	w := rotatable(t)
	was := tlsAndRecord(t, w)
	if err := os.WriteFile(w.root+paths.RestoreRecord(alpha(t)), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := w.verb("", rotateArgs...)
	if out.code != ExitRefused || out.stdout != "" || !strings.Contains(out.stderr, "--resume") {
		t.Fatalf("%+v", out)
	}
	unchanged(t, was, tlsAndRecord(t, w))
}

// faultAt is a rotation writer that fails its n-th write, its bytes landed first when landed.
func faultAt(n int, landed bool) host.FileWriter {
	calls := 0
	return func(path string, data []byte, mode fs.FileMode, gid int) error {
		calls++
		if calls != n {
			return state.WriteFile(path, data, mode, gid)
		}
		if landed {
			if err := state.WriteFile(path, data, mode, gid); err != nil {
				return err
			}
		}
		return errors.New("the disk refused this write")
	}
}

func unchanged(t *testing.T, was, now map[string][]byte) {
	t.Helper()
	for p, data := range was {
		if !bytes.Equal(data, now[p]) {
			t.Fatalf("%s changed", p)
		}
	}
}

// X33: --help is the verb's text on stdout, which says that restoring a backup taken before a
// rotation brings the old CA back and the rotation is to be made again.
func TestX33TheHelpSaysToRotateAgainAfterRestoringAnOlderBackup(t *testing.T) {
	w := bareWorld(t)
	out := w.verb("", "team", "rotate-ca", "--help")
	if out.code != term.ExitOK || out.stderr != "" || !strings.Contains(out.stdout, "backup taken before") || !strings.Contains(out.stdout, "rotate-ca again") {
		t.Fatalf("%+v", out)
	}
}

// X33: the flags are strict, and a team that is not applied has no CA to rotate.
func TestX33TheVerbsRefusals(t *testing.T) {
	w := newWorld(t)
	for _, c := range []struct {
		args []string
		code term.ExitCode
	}{
		{[]string{"team", "rotate-ca"}, term.ExitUsage},
		{[]string{"team", "rotate-ca", "--team", "alpha", "--purge"}, term.ExitUsage},
		{rotateArgs, ExitRefused},
	} {
		if out := w.verb("", c.args...); out.code != c.code || out.stdout != "" {
			t.Errorf("%v: %+v, want %d", c.args, out, c.code)
		}
	}
}
