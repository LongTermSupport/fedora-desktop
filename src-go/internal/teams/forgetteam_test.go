package teams

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatcheckouts"
)

// The checkouts a team taken down forgets its seats in are those that hold seats (the list of
// seat checkouts, which outlives a session) and those a running session names, each once.
func TestTheCheckoutsHoldingSeatsAreTheListAndTheRunningSessions(t *testing.T) {
	store := registry.New(filepath.Join(t.TempDir(), "ccy", "sessions"))
	listed, running := t.TempDir(), t.TempDir()
	if err := seatcheckouts.Of(store).Add(listed); err != nil {
		t.Fatal(err)
	}
	for _, rec := range []registry.Record{
		{Name: "ccy-a", Dir: running, Launcher: "/x/ccy", Prefix: "ccy", Args: []string{"--teams", "1@alpha"}},
		{Name: "ccy-b", Dir: listed, Launcher: "/x/ccy", Prefix: "ccy", Args: []string{"--teams", "2@alpha"}},
	} {
		if err := store.Write(rec); err != nil {
			t.Fatal(err)
		}
	}
	got, err := checkoutsHoldingSeats(store)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{listed, running}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestForgetTeamRemovesTheRecordsInACheckoutWhoseSessionHasEnded(t *testing.T) {
	store := registry.New(filepath.Join(t.TempDir(), "ccy", "sessions"))
	ended := t.TempDir()
	if err := seatcheckouts.Of(store).Add(ended); err != nil {
		t.Fatal(err)
	}
	const id = "0f8b7c1e-3d2a-4b5c-9e6f-1a2b3c4d5e6f"
	if err := conversation.RecordSeats(ended, "1@alpha", id, conversation.SourceExit, 1); err != nil {
		t.Fatal(err)
	}
	record := conversation.SeatRecordPath(ended, membership.Seat{Seat: "1", Team: "alpha"})
	if err := forgetTeam(func() (*registry.Store, error) { return store, nil }, "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(record); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s: %v", record, err)
	}
}

func TestForgetTeamWithNoRegistryIsAnError(t *testing.T) {
	err := forgetTeam(func() (*registry.Store, error) { return nil, errors.New("no registry") }, "alpha")
	if err == nil || err.Error() != "no registry" {
		t.Errorf("err %v", err)
	}
}

func TestAnUnreadableListOfSeatCheckoutsIsAnError(t *testing.T) {
	store := registry.New(filepath.Join(t.TempDir(), "ccy", "sessions"))
	path := seatcheckouts.Of(store).Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("relative/path\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := checkoutsHoldingSeats(store); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("err %v", err)
	}
}

func TestAnUnreadableSessionRecordIsAnError(t *testing.T) {
	store := registry.New(filepath.Join(t.TempDir(), "ccy", "sessions"))
	if err := os.MkdirAll(store.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(store.Dir(), "ccy-bad")
	if err := os.WriteFile(bad, []byte("not a record\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := checkoutsHoldingSeats(store); err == nil || !strings.Contains(err.Error(), bad) {
		t.Errorf("err %v", err)
	}
}
