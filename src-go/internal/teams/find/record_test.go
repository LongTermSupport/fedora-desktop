package find

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// recorded is the rig with a scratch session registry. No tmux call is staged: record-seats
// is told the session by the launcher, so any tmux call fails the test.
type recorded struct {
	rig

	store *registry.Store
	dir   string
}

func recordRig(t *testing.T) recorded {
	t.Helper()
	h := newRig(t, "")
	dir := t.TempDir()
	store := registry.New(dir)
	h.Host.Registry = func() (*registry.Store, error) { return store, nil }
	// The TMUX of an OUTER session's pane, as a ccy run from a shell inside it inherits: it
	// must play no part in which record is written.
	h.Env["TMUX"] = "/tmp/tmux-1000/ccy,4242,0"
	return recorded{rig: h, store: store, dir: dir}
}

func (h recorded) write(t *testing.T, name string) {
	t.Helper()
	rec := registry.Record{Name: name, Dir: "/x", Launcher: "/x/ccy", Prefix: "ccy", Restore: true, Args: []string{"--no-ssh"}}
	if err := h.store.Write(rec); err != nil {
		t.Fatal(err)
	}
}

func TestRecordSeatsPutsTheTeamsInTheNamedSessionsOwnRecord(t *testing.T) {
	h := recordRig(t)
	h.write(t, "ccy-proj")
	h.write(t, "cc-outer")
	if code := h.f.RecordSeats(t.Context(), []string{"ccy-proj", "dev1@team-a"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	got, err := h.store.Get("ccy-proj")
	if err != nil || !slices.Equal(got.Args, []string{"--no-ssh", "--teams", "dev1@team-a"}) {
		t.Fatalf("record %+v, err %v", got, err)
	}
	outer, err := h.store.Get("cc-outer")
	if err != nil || !slices.Equal(outer.Args, []string{"--no-ssh"}) {
		t.Fatalf("the outer session's record changed: %+v, err %v", outer, err)
	}
}

func TestRecordSeatsForASessionWithNoRecordIsNothingToDo(t *testing.T) {
	h := recordRig(t)
	if code := h.f.RecordSeats(t.Context(), []string{"ccy-proj", "dev1@team-a"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
}

func TestRecordSeatsNeedsExactlyTheSessionAndTheSeats(t *testing.T) {
	h := recordRig(t)
	for _, args := range [][]string{nil, {"a@b"}, {"", "a@b"}, {"ccy-proj", "a@b", "c@d"}} {
		if code := h.f.RecordSeats(t.Context(), args); code != term.ExitUsage {
			t.Fatalf("%q: exit %d", args, code)
		}
	}
}

func TestRecordSeatsOnAnUnreadableRecordFails(t *testing.T) {
	h := recordRig(t)
	teamtest.MustWrite(t, filepath.Join(h.dir, "ccy-proj"), "junk\n")
	if code := h.f.RecordSeats(t.Context(), []string{"ccy-proj", "a@b"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}
