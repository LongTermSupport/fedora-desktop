package proto

import (
	"bytes"
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
		if got := SyncFilter(r, anchors(self), true); !got.Equal(want) {
			t.Errorf("%s: filter is\n%s", file, got.Compact())
		}
	}
}

// G5 (QS3): the coordinator's form needs the questions room ready, not only the roster naming
// this member. A coordinator whose room is not ready syncs main alone, byte for byte as a
// worker does; a worker's filter does not depend on the argument.
func TestACoordinatorWhoseQuestionsRoomIsNotReadyHasTheWorkersFilter(t *testing.T) {
	r := testRoster(t)
	forms := map[string]func(Roster, Anchors, bool) Value{"sync": SyncFilter, "re-add": SyncFilterReAdd}
	for name, form := range forms {
		t.Run(name, func(t *testing.T) {
			worker := form(r, anchors(workerID), true).Compact()
			if got := form(r, anchors(workerID), false).Compact(); !bytes.Equal(got, worker) {
				t.Errorf("a worker's filter changed with the argument:\n%s\nwant\n%s", got, worker)
			}
			if got := form(r, anchors(coordID), false).Compact(); !bytes.Equal(got, worker) {
				t.Errorf("a coordinator, room not ready:\n%s\nwant the worker's\n%s", got, worker)
			}
			ready := form(r, anchors(coordID), true).Compact()
			if bytes.Equal(ready, worker) || !bytes.Contains(ready, []byte(r.Rooms.Questions)) {
				t.Errorf("a coordinator, room ready, lacks the questions room:\n%s", ready)
			}
			if bytes.Contains(worker, []byte(r.Rooms.Questions)) {
				t.Errorf("the worker's filter names the questions room:\n%s", worker)
			}
		})
	}
}

func TestFilterKeepsEveryTypeTheRulesRead(t *testing.T) {
	read := []string{MessageType, RedactionType, CreateType, PowerLevelsType, JoinRulesType, RosterType, LinkType, MemberType, topicType, ColourType}
	if !slices.Equal(TimelineTypes, read) {
		t.Fatalf("timeline types %v", TimelineTypes)
	}
	if !slices.Equal(StateTypes, read[2:]) {
		t.Fatalf("state types %v", StateTypes)
	}
}
