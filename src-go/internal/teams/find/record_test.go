package find

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const ccyPane = "/tmp/tmux-1000/ccy,4242,0"

// recorded is the rig with a scratch session registry, run from inside a ccy tmux session.
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
	h.Env["TMUX"] = ccyPane
	h.On("tmux -L ccy display-message -p #S", "ccy-proj\n", 0)
	return recorded{rig: h, store: store, dir: dir}
}

func TestRecordSeatsPutsTheTeamsInTheCurrentSessionsOwnRecord(t *testing.T) {
	h := recordRig(t)
	rec := registry.Record{Name: "ccy-proj", Dir: "/x", Launcher: "/x/ccy", Prefix: "ccy", Restore: true, Args: []string{"--no-ssh"}}
	if err := h.store.Write(rec); err != nil {
		t.Fatal(err)
	}
	if code := h.f.RecordSeats(t.Context(), []string{"dev1@team-a"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	got, err := h.store.Get("ccy-proj")
	if err != nil || !slices.Equal(got.Args, []string{"--no-ssh", "--teams", "dev1@team-a"}) {
		t.Fatalf("record %+v, err %v", got, err)
	}
}

func TestRecordSeatsThatCannotTellTheSessionFailsNamingIt(t *testing.T) {
	h := recordRig(t)
	h.OnErr("tmux -L ccy display-message -p #S", "server exited unexpectedly", 1)
	if code := h.f.RecordSeats(t.Context(), []string{"dev1@team-a"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.ErrOut.String(), "which ccy session") || !strings.Contains(h.ErrOut.String(), "server exited unexpectedly") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestRecordSeatsOutsideACcySessionIsNothingToDo(t *testing.T) {
	h := recordRig(t)
	h.Env["TMUX"] = ""
	if code := h.f.RecordSeats(t.Context(), []string{"dev1@team-a"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
}

func TestRecordSeatsForASessionWithNoRecordIsNothingToDo(t *testing.T) {
	h := recordRig(t)
	if code := h.f.RecordSeats(t.Context(), []string{"dev1@team-a"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
}

func TestRecordSeatsNeedsExactlyTheSeats(t *testing.T) {
	h := recordRig(t)
	for _, args := range [][]string{nil, {"a@b", "c@d"}} {
		if code := h.f.RecordSeats(t.Context(), args); code != term.ExitUsage {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
}

func TestRecordSeatsOnAnUnreadableRecordFails(t *testing.T) {
	h := recordRig(t)
	teamtest.MustWrite(t, filepath.Join(h.dir, "ccy-proj"), "junk\n")
	if code := h.f.RecordSeats(t.Context(), []string{"a@b"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}
