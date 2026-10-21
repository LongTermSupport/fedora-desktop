package proto

import (
	"io/fs"
	"os"
	"slices"
	"testing"
)

// TestSyncFiltersArePinned compares each role's filter with its pinned JSON in testdata/ (A 6.10,
// 14), over the test roster: the worker's, the coordinator's, and the probe's, which is the
// worker's.
func TestSyncFiltersArePinned(t *testing.T) {
	r := testRoster(t)
	r.Members[probeID] = RosterMember{Role: RoleProbe}
	for file, self := range map[string]string{"filter-worker.json": workerID, "filter-coordinator.json": coordID, "filter-probe.json": probeID} {
		data, err := fs.ReadFile(os.DirFS("testdata"), file)
		if err != nil {
			t.Fatal(err)
		}
		want := mustParse(t, string(data))
		if got := SyncFilter(r, anchors(self)); !got.Equal(want) {
			t.Errorf("%s: filter is\n%s", file, got.Compact())
		}
	}
}

func TestFilterKeepsEveryTypeTheRulesRead(t *testing.T) {
	read := []string{MessageType, RedactionType, CreateType, PowerLevelsType, JoinRulesType, RosterType, LinkType, MemberType}
	if !slices.Equal(TimelineTypes, read) {
		t.Fatalf("timeline types %v", TimelineTypes)
	}
	if !slices.Equal(StateTypes, read[2:]) {
		t.Fatalf("state types %v", StateTypes)
	}
}
