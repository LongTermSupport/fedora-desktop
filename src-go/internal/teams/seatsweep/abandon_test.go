package seatsweep

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	seat1 = "repo.1+site.podman"
	seat2 = "repo.2+site.podman"
)

// noNet is a credential that fails the test on any use: an abandon never contacts a server.
type noNet struct{ t *testing.T }

func (noNet) Kind() seat.Kind { return seat.KindPassword }

func (n noNet) Mint(context.Context, seat.Bundle, seat.Secret) (seat.Secret, error) {
	n.t.Error("the abandon minted a token")
	return seat.Secret{}, errors.New("no network")
}

func (n noNet) Retire(context.Context, seat.Bundle, seat.Secret) error {
	n.t.Error("the abandon logged a token out")
	return errors.New("no network")
}

// abandonWorld is the sweep's world with a working directory (the checkout the verb acts on),
// no status records, the caller "tester", and a credential that must not be used.
func abandonWorld(t *testing.T) (*world, paths.CheckoutKey) {
	t.Helper()
	w := newWorld(t)
	wd := filepath.Join(w.home, "checkout")
	if err := os.MkdirAll(wd, 0o700); err != nil {
		t.Fatal(err)
	}
	w.cmd.Getwd = func() (string, error) { return wd, nil }
	w.cmd.Status = func(paths.TeamID) (status.Record, bool, error) { return status.Record{}, false, nil }
	w.cmd.Who = func() (string, error) { return "tester", nil }
	w.cmd.Parts.Credential = noNet{t}
	resolved, err := filepath.EvalSymlinks(wd)
	if err != nil {
		t.Fatal(err)
	}
	return w, w.key(resolved)
}

// released is the abandonment a pool seat's released.json records.
func (w *world) released(pool paths.PoolSeat) map[string]any {
	w.t.Helper()
	data, err := os.ReadFile(pool.ReleasedRecord())
	if err != nil {
		w.t.Fatal(err)
	}
	ab, ok := decode(w.t, data)["abandoned"].(map[string]any)
	if !ok {
		w.t.Fatalf("released.json %s has no abandoned", data)
	}
	return ab
}

func decode(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%s: %v", data, err)
	}
	return out
}

// running holds a session's seat lock of team, as its member link does while it runs.
func (w *world) running(session, team string) {
	w.t.Helper()
	path := w.store.SessionSeat(mustSession(w.t, session), mustTeam(w.t, team)).Lock()
	w.write(path, "")
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() {
		if err := f.Close(); err != nil {
			w.t.Error(err)
		}
	})
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		w.t.Fatal(err)
	}
}

// With no member named, every held seat of the team in the working directory's checkout is
// abandoned: one line each on stdout, released.json naming why, who and when. A free seat is
// said on stderr and is no error; another checkout and another team are untouched.
func TestAbandonVerb(t *testing.T) {
	w, key := abandonWorld(t)
	a := w.hold(key, "dead", seat1, "s1")
	b := w.hold(key, "dead", seat2, "s2")
	free := w.store.PoolSeat(key, mustTeam(t, "dead"), mustSeat(t, "repo.3+site.podman"))
	w.write(free.Bundle(), "{}")
	other := w.hold(w.key("/work/other"), "dead", seat1, "s1")
	ops := w.hold(key, "ops", seat1, "s1")
	if code := w.run("abandon", "dead", "--reason", "team purged"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.errOut.String())
	}
	want := "abandoned seat " + seat1 + " of team dead (session s1)\nabandoned seat " + seat2 + " of team dead (session s2)\n"
	if w.out.String() != want {
		t.Errorf("stdout %q, want %q", w.out.String(), want)
	}
	if !strings.Contains(w.errOut.String(), "repo.3+site.podman") || !strings.Contains(w.errOut.String(), "not held") {
		t.Errorf("stderr %q does not say the free seat is not held", w.errOut.String())
	}
	for _, p := range []paths.PoolSeat{a, b} {
		if exists(t, p.Held()) {
			t.Errorf("%s is still held", p.Dir())
		}
		if ab := w.released(p); ab["reason"] != "team purged" || ab["by"] != "tester" || ab["at_ms"] == nil {
			t.Errorf("abandoned %v", ab)
		}
	}
	if !exists(t, other.Held()) || !exists(t, ops.Held()) {
		t.Errorf("another checkout's or another team's seat was abandoned")
	}
}

// A member named by its seat part or its whole name is the only seat abandoned; a name that
// is no pool seat of the team is a usage error, and nothing is abandoned.
func TestAbandonMember(t *testing.T) {
	for _, name := range []string{"1", seat1} {
		w, key := abandonWorld(t)
		a := w.hold(key, "dead", seat1, "s1")
		b := w.hold(key, "dead", seat2, "s1")
		if code := w.run("abandon", "dead", name, "--reason=gone"); code != term.ExitOK {
			t.Fatalf("%s: exit %d: %s", name, code, w.errOut.String())
		}
		if exists(t, a.Held()) || !exists(t, b.Held()) {
			t.Errorf("%s: abandoned the wrong seats", name)
		}
	}
	w, key := abandonWorld(t)
	a := w.hold(key, "dead", seat1, "s1")
	if code := w.run("abandon", "dead", "1", "9", "--reason", "gone"); code != term.ExitUsage || !exists(t, a.Held()) {
		t.Errorf("an unknown member: exit %d, held %v: %s", code, exists(t, a.Held()), w.errOut.String())
	}
}

// The reason is required and not empty; the override must repeat the team; nothing moves.
func TestAbandonUsage(t *testing.T) {
	for _, args := range [][]string{
		{"abandon"},
		{"abandon", "--reason", "gone"},
		{"abandon", "dead"},
		{"abandon", "dead", "--reason"},
		{"abandon", "dead", "--reason", ""},
		{"abandon", "dead", "--reason=  "},
		{"abandon", "dead", "--reason", "two\nlines"},
		{"abandon", "dead", "--reason", "a", "--reason", "b"},
		{"abandon", "dead", "--reason", "gone", "--allow-known-team", "ops"},
		{"abandon", "dead", "--reason", "gone", "--allow-known-team"},
		{"abandon", "dead", "--reason", "gone", "--force"},
		{"abandon", "NOT A TEAM", "--reason", "gone"},
	} {
		w, key := abandonWorld(t)
		a := w.hold(key, "dead", seat1, "s1")
		if code := w.run(args...); code != term.ExitUsage || !exists(t, a.Held()) || w.out.Len() != 0 {
			t.Errorf("%q: exit %d, held %v, stdout %q", args, code, exists(t, a.Held()), w.out.String())
		}
	}
}

// A team a local source still knows is refused (78) unless --allow-known-team names it: a
// session's desired.json listing it, or its status record on this machine.
func TestAbandonKnown(t *testing.T) {
	sources := map[string]func(*world){
		"desired": func(w *world) { w.desire("s2", `{"team":"dead","seat":"9"}`) },
		"status": func(w *world) {
			w.cmd.Status = func(paths.TeamID) (status.Record, bool, error) { return status.Record{}, true, nil }
		},
	}
	for name, known := range sources {
		w, key := abandonWorld(t)
		a := w.hold(key, "dead", seat1, "s1")
		known(w)
		if code := w.run("abandon", "dead", "--reason", "gone"); code != term.ExitConfig || !exists(t, a.Held()) {
			t.Errorf("%s: exit %d, held %v", name, code, exists(t, a.Held()))
		}
		if !strings.Contains(w.errOut.String(), "--allow-known-team dead") {
			t.Errorf("%s: stderr %q does not name the override", name, w.errOut.String())
		}
		if code := w.run("abandon", "dead", "--reason", "gone", "--allow-known-team", "dead"); code != term.ExitOK || exists(t, a.Held()) {
			t.Errorf("%s with the override: exit %d: %s", name, code, w.errOut.String())
		}
	}
}

// A source that cannot be read is a refusal (78), never a pass.
func TestAbandonUnreadable(t *testing.T) {
	sources := map[string]func(*world){
		"status": func(w *world) {
			w.cmd.Status = func(paths.TeamID) (status.Record, bool, error) {
				return status.Record{}, false, errors.New("unreadable")
			}
		},
		"desired": func(w *world) { w.write(w.store.DesiredFile(mustSession(w.t, "s2")), "not json") },
		"missing": func(w *world) {
			if err := os.MkdirAll(w.store.Control(mustSession(w.t, "s3")), 0o700); err != nil {
				w.t.Fatal(err)
			}
		},
		"stray": func(w *world) { w.write(w.store.Root()+"/control/NOT A SESSION", "") },
	}
	for name, broken := range sources {
		w, key := abandonWorld(t)
		a := w.hold(key, "dead", seat1, "s1")
		broken(w)
		if code := w.run("abandon", "dead", "--reason", "gone"); code != term.ExitConfig || !exists(t, a.Held()) {
			t.Errorf("%s: exit %d, held %v: %s", name, code, exists(t, a.Held()), w.errOut.String())
		}
	}
}

// The running check is per team: session s1's link of team ops runs, and its dead seat of team
// dead is abandoned while ops's seat stays held. A dead seat whose own link runs is refused (75).
func TestAbandonBusy(t *testing.T) {
	w, key := abandonWorld(t)
	dead := w.hold(key, "dead", seat1, "s1")
	ops := w.hold(key, "ops", seat1, "s1")
	w.desire("s1", `{"team":"ops","seat":"1"}`)
	w.running("s1", "ops")
	if code := w.run("abandon", "dead", "--reason", "gone"); code != term.ExitOK || exists(t, dead.Held()) || !exists(t, ops.Held()) {
		t.Fatalf("exit %d, dead held %v, ops held %v: %s", code, exists(t, dead.Held()), exists(t, ops.Held()), w.errOut.String())
	}
	w, key = abandonWorld(t)
	dead = w.hold(key, "dead", seat1, "s1")
	w.running("s1", "dead")
	if code := w.run("abandon", "dead", "--reason", "gone"); code != term.ExitBusy || !exists(t, dead.Held()) {
		t.Fatalf("a running link: exit %d, held %v", code, exists(t, dead.Held()))
	}
	if !strings.Contains(w.errOut.String(), "running") {
		t.Errorf("stderr %q", w.errOut.String())
	}
}
