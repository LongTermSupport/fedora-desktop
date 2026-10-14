package find

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// recorded is the rig with a scratch session registry.
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
	return recorded{rig: h, store: store, dir: dir}
}

func TestRecordSeatsPutsTheTeamsInTheSessionsOwnRecord(t *testing.T) {
	h := recordRig(t)
	rec := registry.Record{Name: "ccy-proj", Dir: "/x", Launcher: "/x/ccy", Prefix: "ccy", Restore: true, Args: []string{"--no-ssh"}}
	if err := h.store.Write(rec); err != nil {
		t.Fatal(err)
	}
	if code := h.f.RecordSeats([]string{"ccy-proj", "dev1@team-a"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
	got, err := h.store.Get("ccy-proj")
	if err != nil || !slices.Equal(got.Args, []string{"--no-ssh", "--teams", "dev1@team-a"}) {
		t.Fatalf("record %+v, err %v", got, err)
	}
}

func TestRecordSeatsForASessionWithNoRecordIsNothingToDo(t *testing.T) {
	h := recordRig(t)
	if code := h.f.RecordSeats([]string{"ccy-gone", "dev1@team-a"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
}

func TestRecordSeatsForNoSessionNameIsNothingToDo(t *testing.T) {
	h := recordRig(t)
	if code := h.f.RecordSeats([]string{"", "dev1@team-a"}); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, h.ErrOut.String())
	}
}

func TestRecordSeatsNeedsASessionAndSeats(t *testing.T) {
	h := recordRig(t)
	if code := h.f.RecordSeats([]string{"only-one"}); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
}

func TestRecordSeatsOnAnUnreadableRecordFails(t *testing.T) {
	h := recordRig(t)
	teamtest.MustWrite(t, filepath.Join(h.dir, "bad"), "junk\n")
	if code := h.f.RecordSeats([]string{"bad", "a@b"}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}
