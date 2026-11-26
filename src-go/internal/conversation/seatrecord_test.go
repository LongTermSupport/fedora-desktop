package conversation

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

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

// A launch writes its seats' records inside the checkout only (R36 batch review, finding 4): a
// container can replace the seat-records directory, or a team's directory in it, with a link,
// and a launch that followed it would write the host's records outside the checkout.
func TestRecordSeatsNeverWritesThroughALinkOutOfTheCheckout(t *testing.T) {
	for _, linked := range []string{seatRecordDir, filepath.Join(seatRecordDir, "alpha")} {
		t.Run(linked, func(t *testing.T) {
			w := newWorld(t)
			outside := t.TempDir()
			at := filepath.Join(w.checkout, linked)
			if err := os.MkdirAll(filepath.Dir(at), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, at); err != nil {
				t.Fatal(err)
			}
			if err := RecordSeats(w.checkout, "1@alpha", id, SourceLaunch, recordMS); err == nil {
				t.Error("recorded through a link out of the checkout, want a refusal")
			}
			entries, err := os.ReadDir(outside)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("wrote %d entries outside the checkout, want none", len(entries))
			}
		})
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

// heldElsewhere is whether another open file description cannot take path's flock(2) now.
func heldElsewhere(t *testing.T, path string) bool {
	t.Helper()
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("the seat-record lock %s: %v", path, err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
			t.Fatal(err)
		}
		return false
	}
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatalf("flock %s: %v", path, err)
	}
	return true
}

// lockedDuring runs write and reports, for every time a seat-record writer took the checkout's
// lock, whether that lock was the checkout's and was held against everyone else.
func lockedDuring(t *testing.T, checkout string, write func() error) []string {
	t.Helper()
	var seen []string
	seatRecordsLocked = func(lockPath string) {
		if lockPath != filepath.Join(checkout, seatRecordDir, ".lock") {
			seen = append(seen, "the lock is "+lockPath)
		}
		if !heldElsewhere(t, lockPath) {
			seen = append(seen, "not held")
		}
		seen = append(seen, "locked")
	}
	t.Cleanup(func() { seatRecordsLocked = nil })
	if err := write(); err != nil {
		t.Fatal(err)
	}
	return seen
}

// The old #417 branch's lock (4001e4789), kept: a launch writes every seat of its list under
// flock(2) on the checkout's seat-record lock, so two launches of the same seats cannot leave
// one seat on each conversation.
func TestRecordSeatsWritesUnderTheCheckoutsLock(t *testing.T) {
	w := newWorld(t)
	seen := lockedDuring(t, w.checkout, func() error {
		return RecordSeats(w.checkout, "1@alpha,2@beta", id, SourceLaunch, recordMS)
	})
	if !slices.Equal(seen, []string{"locked"}) {
		t.Fatalf("RecordSeats under the lock: %v, want the lock taken once and held", seen)
	}
}

// fakeLockClock is a clock whose Sleep only moves its time on, so a wait on the lock is measured
// without real time passing.
type fakeLockClock struct {
	now   time.Time
	slept time.Duration
}

func (c *fakeLockClock) Now() time.Time { return c.now }

func (c *fakeLockClock) Sleep(d time.Duration) {
	c.slept += d
	c.now = c.now.Add(d)
}

// holdSeatRecordLock takes the checkout's seat-record lock through another open file description
// for the rest of the test.
func holdSeatRecordLock(t *testing.T, checkout string) string {
	t.Helper()
	path := filepath.Join(checkout, seatRecordDir, seatRecordLock)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	return path
}

// R36 batch review: a writer that cannot take the lock (another launch or remove holds it, or a
// container left it held) waits a bounded time, then fails naming the lock, never hangs.
func TestSeatRecordWritersFailNamingALockHeldTooLong(t *testing.T) {
	for _, c := range []struct {
		name  string
		write func(checkout string) error
	}{
		{"RecordSeats", func(checkout string) error {
			return RecordSeats(checkout, "1@alpha", id, SourceLaunch, recordMS)
		}},
		{"ForgetTeam", func(checkout string) error { return ForgetTeam([]string{checkout}, "alpha") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			w.record("1@beta", id, SourceLaunch)
			if err := os.MkdirAll(filepath.Join(w.checkout, seatRecordDir, "alpha"), 0o700); err != nil {
				t.Fatal(err)
			}
			lock := holdSeatRecordLock(t, w.checkout)
			clock := &fakeLockClock{now: time.Unix(1_700_000_000, 0)}
			lockClock = clock
			t.Cleanup(func() { lockClock = realLockClock{} })
			err := c.write(w.checkout)
			if err == nil || !strings.Contains(err.Error(), lock+" is still held after 30s") {
				t.Fatalf("%s with the lock held: %v", c.name, err)
			}
			if clock.slept < seatRecordLockWait {
				t.Fatalf("waited %s, want %s", clock.slept, seatRecordLockWait)
			}
			if _, err := os.Stat(filepath.Join(w.checkout, seatRecordDir, "alpha")); err != nil {
				t.Fatalf("alpha's records changed while the lock was held: %v", err)
			}
		})
	}
}

// R36 batch review nit: removing a team's records opens the lock as the launch does, never
// through a link planted at its name.
func TestForgetTeamRefusesALinkedLock(t *testing.T) {
	w := newWorld(t)
	w.record("1@alpha", id, SourceLaunch)
	lock := filepath.Join(w.checkout, seatRecordDir, seatRecordLock)
	other := filepath.Join(w.checkout, "elsewhere")
	if err := os.WriteFile(other, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	// Relative, so it stays inside the checkout: an os.Root follows such a link at the last name,
	// O_NOFOLLOW or not.
	if err := os.Symlink("../../../elsewhere", lock); err != nil {
		t.Fatal(err)
	}
	if err := ForgetTeam([]string{w.checkout}, "alpha"); err == nil || !strings.Contains(err.Error(), "could not open the seat-record lock") {
		t.Errorf("ForgetTeam with the lock a link: %v, want the lock refused", err)
	}
	if err := RecordSeats(w.checkout, "2@beta", id, SourceLaunch, recordMS); err == nil || !strings.Contains(err.Error(), "could not open the seat-record lock") {
		t.Errorf("RecordSeats with the lock a link: %v, want the lock refused", err)
	}
}

// A dangling link at the lock is refused by both writers, and the file it names is never made: an
// os.Root follows the link at the last name, so a create there that is not exclusive would make it.
func TestSeatRecordWritersCreateNothingThroughADanglingLock(t *testing.T) {
	w := newWorld(t)
	w.record("1@alpha", id, SourceLaunch)
	lock := filepath.Join(w.checkout, seatRecordDir, seatRecordLock)
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../planted", lock); err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(w.checkout, "planted")
	if err := ForgetTeam([]string{w.checkout}, "alpha"); err == nil || !strings.Contains(err.Error(), "could not open the seat-record lock") {
		t.Errorf("ForgetTeam with the lock a dangling link: %v, want the lock refused", err)
	}
	if err := RecordSeats(w.checkout, "2@beta", id, SourceLaunch, recordMS); err == nil || !strings.Contains(err.Error(), "could not open the seat-record lock") {
		t.Errorf("RecordSeats with the lock a dangling link: %v, want the lock refused", err)
	}
	if _, err := os.Lstat(planted); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the dangling lock's target: %v, want it never created", err)
	}
}

// Removing a team's records takes the same lock, so it never lands between another writer's seats.
func TestForgetTeamRemovesUnderTheCheckoutsLock(t *testing.T) {
	w := newWorld(t)
	w.record("1@alpha", id, SourceLaunch)
	seen := lockedDuring(t, w.checkout, func() error {
		return ForgetTeam([]string{w.checkout}, "alpha")
	})
	if !slices.Equal(seen, []string{"locked"}) {
		t.Fatalf("ForgetTeam under the lock: %v, want the lock taken once and held", seen)
	}
}
