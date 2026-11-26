package conversation

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
)

const recordMS = 1793000000000

// transcriptFor plants the ccy transcript of sessionID in the world's checkout.
func (w *world) transcriptFor(sessionID string) {
	w.t.Helper()
	write(w.t, filepath.Join(w.checkout, ".claude/ccy/projects/-workspace", sessionID+".jsonl"), "{}\n")
}

// record writes the seat records of list, failing the test on an error.
func (w *world) record(list, sessionID, source string) {
	w.t.Helper()
	if err := RecordSeats(w.checkout, list, sessionID, source, recordMS); err != nil {
		w.t.Fatal(err)
	}
}

func seatCase(t *testing.T, err error) Case {
	t.Helper()
	var seatErr *SeatError
	if !errors.As(err, &seatErr) {
		t.Fatalf("%v is not a SeatError", err)
	}
	return seatErr.Case
}

// Issue 417: the record is the seat's. Each seat a launch held points to its conversation, so a
// session's end, which removes the session's own records, does not lose it.
func TestASeatRecordIsReadWithoutAnySessionRecord(t *testing.T) {
	w := newWorld(t)
	w.transcriptFor(id)
	w.record("1@alpha", id, SourceLaunch)
	got, err := w.forSeats("1@alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id || len(got.Unrecorded) != 0 {
		t.Fatalf("ForSeats = %+v, want %s with every seat recorded", got, id)
	}
	data, err := os.ReadFile(SeatRecordPath(w.checkout, membership.Seat{Seat: "1", Team: "alpha"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"session_id":"`+id+`"`) || !strings.Contains(string(data), `"source":"launch"`) {
		t.Fatalf("record %s", data)
	}
}

// The owner's ruling on #417: a subset or a superset of the seats a session held resolves when
// every named seat with a record agrees; a seat with none is allowed when another has one, and is
// named so the launch records it.
func TestASubsetOrSupersetResolvesWhenTheRecordedSeatsAgree(t *testing.T) {
	w := newWorld(t)
	w.transcriptFor(id)
	w.record("1@alpha,2@beta", id, SourceLaunch)
	for list, unrecorded := range map[string][]string{
		"1@alpha":          nil,
		"2@beta":           nil,
		"1@alpha,3@gamma":  {"3@gamma"},
		"2@beta , 1@alpha": nil,
	} {
		got, err := w.forSeats(list)
		if err != nil {
			t.Fatalf("%s: %v", list, err)
		}
		if got.ID != id || !slices.Equal(got.Unrecorded, unrecorded) {
			t.Errorf("%s: ForSeats = %+v, want %s with unrecorded %v", list, got, id, unrecorded)
		}
	}
}

// Two named seats that point at different conversations are the ambiguous case: each seat and
// its id are named, none is chosen.
func TestSeatsThatDisagreeAreRefusedNamingEachSeat(t *testing.T) {
	w := newWorld(t)
	w.transcriptFor(id)
	w.transcriptFor(otherID)
	w.record("1@alpha", id, SourceLaunch)
	w.record("2@beta", otherID, SourceSet)
	got, err := w.forSeats("1@alpha,2@beta")
	mustRefuse(t, got, err, "1@alpha", id, "2@beta", otherID, "disagree")
	if c := seatCase(t, err); c != CaseAmbiguous {
		t.Errorf("case %s, want %s", c, CaseAmbiguous)
	}
}

// A record whose conversation has no transcript here names the path Claude Code would read.
func TestARecordWithNoTranscriptNamesTheExpectedPath(t *testing.T) {
	w := newWorld(t)
	w.record("1@alpha", id, SourceLaunch)
	got, err := w.forSeats("1@alpha")
	mustRefuse(t, got, err, filepath.Join(w.checkout, ".claude/ccy/projects/-workspace", id+".jsonl"))
	if c := seatCase(t, err); c != CaseNoTranscript {
		t.Errorf("case %s, want %s", c, CaseNoTranscript)
	}
}

// The seat record is the answer once there is one: the per-session lookup, which would refuse two
// sessions here, is not asked.
func TestTheSeatRecordWinsOverTheSessionRecords(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "1@alpha")
	w.v1Session("ccy-proj-2", otherID, "1792000000000", "1@alpha")
	w.record("1@alpha", otherID, SourceRelaunch)
	got, err := w.forSeats("1@alpha")
	if err != nil || got.ID != otherID {
		t.Fatalf("ForSeats = %+v, %v; want %s", got, err, otherID)
	}
}

// With no seat record the sessions' records still answer (the records ccy wrote before #417), and
// each refusal is one of the four cases.
func TestWithNoSeatRecordTheSessionRecordsAnswerInTheFourCases(t *testing.T) {
	w := newWorld(t)
	_, err := w.forSeats("1@alpha")
	if c := seatCase(t, err); c != CaseNoSession || !errors.Is(err, ErrNoSeatConversation) {
		t.Errorf("no session: case %s, %v", c, err)
	}
	w.v1Session("ccy-proj", "", "", "1@alpha")
	_, err = w.forSeats("1@alpha")
	if c := seatCase(t, err); c != CaseNoConversation || !errors.Is(err, ErrNoSeatConversation) {
		t.Errorf("no conversation: case %s, %v", c, err)
	}
	w.v1Session("ccy-proj-2", id, "1791000000000", "2@alpha")
	w.v1Session("ccy-proj-3", otherID, "1792000000000", "2@alpha")
	_, err = w.forSeats("2@alpha")
	if c := seatCase(t, err); c != CaseAmbiguous {
		t.Errorf("two sessions: case %s, %v", c, err)
	}
	write(t, w.v1Path("ccy-proj-4"), recordedAt("ccy-proj-4", thirdID, "1791000000000"))
	write(t, filepath.Join(w.checkout, ".claude/ccy/pingbus/sessions/ccy-proj-4/applied.json"), applied(t, "4@alpha"))
	_, err = w.forSeats("4@alpha")
	if c := seatCase(t, err); c != CaseNoTranscript {
		t.Errorf("no transcript: case %s, %v", c, err)
	}
}

// NeverUsed is the first launch: no seat record and no session that held the seats, the case a
// plain launch of named seats refuses. A seat with a record, or held by a session (with or without
// a conversation), was used; what cannot be read is an error, never a first launch.
func TestNeverUsedTellsAFirstLaunch(t *testing.T) {
	w := newWorld(t)
	w.transcriptFor(id)
	w.record("1@alpha", id, SourceLaunch)
	w.v1Session("ccy-proj", otherID, "1791000000000", "dev1@alpha")
	w.v1Session("ccy-proj-2", "", "", "qa@alpha")
	for list, want := range map[string]bool{
		"ops@alpha": true, "ops@alpha,9@beta": true,
		"1@alpha": false, "9@beta,1@alpha": false, "dev1@alpha": false, "qa@alpha": false,
	} {
		got, err := NeverUsed(w.checkout, list, w.where())
		if err != nil || got != want {
			t.Errorf("%s: NeverUsed %v, %v; want %v", list, got, err, want)
		}
	}
	write(t, SeatRecordPath(w.checkout, membership.Seat{Seat: "2", Team: "alpha"}), "{")
	for _, list := range []string{"2@alpha", "nonsense"} {
		if got, err := NeverUsed(w.checkout, list, w.where()); err == nil {
			t.Errorf("%s: NeverUsed %v with no error", list, got)
		}
	}
}

// A record that is not one is an error, never read as no record.
func TestACorruptSeatRecordIsRefused(t *testing.T) {
	w := newWorld(t)
	path := SeatRecordPath(w.checkout, membership.Seat{Seat: "1", Team: "alpha"})
	bodies := []string{
		"{", `{"v":2}`,
		`{"v":1,"seat":"2","team":"alpha","session_id":"` + id + `","source":"launch","written_ms":1}`,
		`{"v":1,"seat":"1","team":"alpha","session_id":"abc","source":"launch","written_ms":1}`,
		`{"v":1,"seat":"1","team":"alpha","session_id":"` + id + `","source":"guess","written_ms":1}`,
	}
	for _, body := range bodies {
		write(t, path, body)
		got, err := w.forSeats("1@alpha")
		mustRefuse(t, got, err, path)
	}
}

func TestRecordSeatsRefusesWhatIsNotARecord(t *testing.T) {
	w := newWorld(t)
	for _, c := range []struct{ list, id, source string }{
		{"1@alpha", "abc", SourceLaunch},
		{"alpha", id, SourceLaunch},
		{"", id, SourceLaunch},
		{"1@alpha", id, "guess"},
	} {
		if err := RecordSeats(w.checkout, c.list, c.id, c.source, recordMS); err == nil {
			t.Errorf("%+v recorded", c)
		}
	}
}

// The session's end carries the conversation it ended in into its seats' records: the v1 hook's
// record here, read as a restore reads it.
func TestSyncSeatsRecordsTheConversationTheSessionEndedIn(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "1@alpha")
	w.transcriptFor(otherID)
	w.record("1@alpha", otherID, SourceLaunch)
	got, err := SyncSeats(w.checkout, "ccy-proj", "1@alpha", w.where(), recordMS)
	if err != nil || got != id {
		t.Fatalf("SyncSeats = %q, %v; want %s", got, err, id)
	}
	seated, err := w.forSeats("1@alpha")
	if err != nil || seated.ID != id {
		t.Fatalf("after the sync ForSeats = %+v, %v", seated, err)
	}
}

// A session that recorded nothing leaves the seats' records as they are, and says so.
func TestSyncSeatsWithNothingRecordedChangesNothing(t *testing.T) {
	w := newWorld(t)
	w.transcriptFor(otherID)
	w.record("1@alpha", otherID, SourceLaunch)
	_, err := SyncSeats(w.checkout, "ccy-proj", "1@alpha", w.where(), recordMS)
	if !errors.Is(err, errNothingToSync) {
		t.Fatalf("SyncSeats: %v, want errNothingToSync", err)
	}
	seated, err := w.forSeats("1@alpha")
	if err != nil || seated.ID != otherID {
		t.Fatalf("ForSeats = %+v, %v; the record moved", seated, err)
	}
}

// At a session's end its seats are those its own state names: the teamchat desired membership or
// the v1 keeper's report; the conversation it ended in is carried into each.
func TestSyncSessionFindsTheSeatsTheSessionHeld(t *testing.T) {
	w := newWorld(t)
	w.v2Session("ccy-proj", id, 1791000000000, "1@beta")
	w.v1Session("ccy-old", otherID, "1791000000000", "2@alpha")
	for session, want := range map[string]string{"ccy-proj": "1@beta", "ccy-old": "2@alpha"} {
		got, err := SyncSession(w.checkout, session, w.where(), recordMS)
		if err != nil || got.List != want {
			t.Fatalf("%s: SyncSession = %+v, %v; want %s", session, got, err, want)
		}
	}
	if seated, err := w.forSeats("1@beta"); err != nil || seated.ID != id {
		t.Fatalf("1@beta: %+v, %v", seated, err)
	}
	if seated, err := w.forSeats("2@alpha"); err != nil || seated.ID != otherID {
		t.Fatalf("2@alpha: %+v, %v", seated, err)
	}
	if got, err := SyncSession(w.checkout, "ccy-none", w.where(), recordMS); err != nil || !strings.Contains(got.Nothing, "holds no seat") || got.ID != "" {
		t.Fatalf("a session holding no seat: %+v, %v", got, err)
	}
}

// Nothing to carry is SyncSession's answer, not an error: a caller reads Synced.Nothing and never
// matches an error of this package (rule project_start_decision).
func TestSyncSessionWithNothingRecordedSaysWhy(t *testing.T) {
	w := newWorld(t)
	w.v2Session("ccy-proj", id, 1791000000000, "1@beta")
	w.transcriptFor(otherID)
	w.record("1@beta", otherID, SourceLaunch)
	if err := os.Remove(filepath.Join(w.checkout, ".claude/ccy/projects/-workspace", id+".jsonl")); err != nil {
		t.Fatal(err)
	}
	got, err := SyncSession(w.checkout, "ccy-proj", w.where(), recordMS)
	if err != nil || got.Nothing == "" || got.ID != "" || got.List != "1@beta" {
		t.Fatalf("SyncSession = %+v, %v; want nothing carried, said", got, err)
	}
	if seated, err := w.forSeats("1@beta"); err != nil || seated.ID != otherID {
		t.Fatalf("1@beta: %+v, %v; the record moved", seated, err)
	}
}

// Register R4 (seat 4 against seat 5): a session that resumed into its seat (its link's record says
// source "resume", as a session that existed before it joined a team says on its relaunch) is
// carried into the seat's record exactly as one that started there; the source is never read.
func TestSyncSessionCarriesASessionThatResumedIntoItsSeat(t *testing.T) {
	w := newWorld(t)
	w.v2Session("ccy-joined", id, 1791000000000, "4@beta")
	name, err := paths.ParseSessionName("ccy-joined")
	if err != nil {
		t.Fatal(err)
	}
	run, err := paths.ParseRunID("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	conv, err := state.Conversation{Run: run, SessionID: id, Source: state.SourceResume, WrittenMS: 1791000000001}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	write(t, w.v2Path(name.String()), string(conv))
	got, err := SyncSession(w.checkout, "ccy-joined", w.where(), recordMS)
	if err != nil || got.ID != id || got.List != "4@beta" {
		t.Fatalf("SyncSession = %+v, %v", got, err)
	}
	if err := os.RemoveAll(filepath.Dir(w.store.DesiredFile(name))); err != nil {
		t.Fatal(err)
	}
	if seated, err := w.forSeats("4@beta"); err != nil || seated.ID != id {
		t.Fatalf("after the session's state is gone ForSeats = %+v, %v", seated, err)
	}
}

func TestWaysOnNamesTheRepairAndFresh(t *testing.T) {
	got := WaysOn("1@alpha")
	for _, want := range []string{"ccy-teams seats conversation 1@alpha --set <id>", "ccy --teams 1@alpha --fresh"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q does not say %q", got, want)
		}
	}
}

func TestTranscriptOfAnIDInTheCheckout(t *testing.T) {
	w := newWorld(t)
	if _, err := TranscriptOf(w.checkout, id); err == nil || !strings.Contains(err.Error(), id+".jsonl") {
		t.Fatalf("a missing transcript: %v", err)
	}
	w.transcriptFor(id)
	path, err := TranscriptOf(w.checkout, id)
	if err != nil || path != filepath.Join(w.checkout, ".claude/ccy/projects/-workspace", id+".jsonl") {
		t.Fatalf("TranscriptOf = %q, %v", path, err)
	}
}
