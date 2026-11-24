package conversation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// indexPath is a seat's pointer file, spelt here independently of the code.
func (w *world) indexPath(seat string) string {
	return filepath.Join(w.checkout, ".claude/ccy/seat-conversations", seat+".json")
}

// pointer plants a seat's index entry.
func (w *world) pointer(seat, sessionID, session, source, ms string) {
	w.t.Helper()
	write(w.t, w.indexPath(seat), `{"v":1,"seat":"`+seat+`","session_id":"`+sessionID+`","session":"`+session+
		`","source":"`+source+`","written_ms":`+ms+`}`)
}

// transcriptFor writes the checkout's transcript of sessionID.
func (w *world) transcriptFor(sessionID string) {
	w.t.Helper()
	write(w.t, filepath.Join(w.checkout, ".claude/ccy/projects/-workspace", sessionID+".jsonl"), "{}\n")
}

// E2: a launch's lookup (SaveSeats) writes each held seat's pointer, whole, so the seat keeps its
// conversation when its session's folder is later reused by a launch of other seats.
func TestSeatKeepsItsConversationWhenItsSessionFolderIsReused(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	if _, err := SaveSeats(w.checkout, "qa@beta", w.where()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(w.indexPath("dev1@alpha"))
	if err != nil {
		t.Fatalf("the lookup did not write dev1@alpha's pointer: %v", err)
	}
	if !strings.Contains(string(data), id) || !strings.Contains(string(data), `"source":"session"`) {
		t.Errorf("pointer %s", data)
	}
	info, err := os.Stat(w.indexPath("dev1@alpha"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("pointer mode %v, %v", info, err)
	}
	// The folder is reused for another seat: its records no longer name dev1.
	w.v1Session("ccy-proj", otherID, "1792000000000", "qa@beta")
	got, err := w.forSeats("dev1@alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id {
		t.Errorf("dev1@alpha = %+v, want %s", got, id)
	}
}

// E2: an older session record never moves a newer pointer back.
func TestAnOlderSessionRecordDoesNotMoveThePointer(t *testing.T) {
	w := newWorld(t)
	w.transcriptFor(otherID)
	w.pointer("dev1@alpha", otherID, "", "set", "1795000000000")
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	got, err := w.forSeats("dev1@alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != otherID {
		t.Errorf("dev1@alpha = %+v, want the newer pointer %s", got, otherID)
	}
}

// E5: --set repairs the pointer; a session that runs the seat afterwards moves it on.
func TestSetSeatsPointsTheSeatsAtTheConversation(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	w.v1Session("ccy-proj-2", otherID, "1792000000000", "qa@beta")
	if err := SetSeats(w.checkout, "dev1@alpha,qa@beta", id, w.where(), 1793000000000); err != nil {
		t.Fatal(err)
	}
	got, err := w.forSeats("dev1@alpha,qa@beta")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id || len(got.Unrecorded) != 0 {
		t.Errorf("after --set: %+v, want %s for both", got, id)
	}
	data, err := os.ReadFile(w.indexPath("qa@beta"))
	if err != nil || !strings.Contains(string(data), `"source":"set"`) || !strings.Contains(string(data), `"written_ms":1793000000000`) {
		t.Errorf("qa@beta's pointer %s, %v", data, err)
	}
	w.v1Session("ccy-proj-2", thirdID, "1794000000000", "qa@beta")
	w.transcriptFor(thirdID)
	got, err = w.forSeats("qa@beta")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != thirdID {
		t.Errorf("a later session record did not move the pointer: %+v", got)
	}
}

// E5: --set takes only a conversation of this checkout.
func TestSetSeatsRefusesAnIDWithNoTranscript(t *testing.T) {
	w := newWorld(t)
	err := SetSeats(w.checkout, "dev1@alpha", thirdID, w.where(), 1793000000000)
	if err == nil || !strings.Contains(err.Error(), filepath.Join(w.checkout, ".claude/ccy/projects/-workspace", thirdID+".jsonl")) {
		t.Fatalf("SetSeats = %v, want the refusal naming the transcript path", err)
	}
	if _, statErr := os.Stat(w.indexPath("dev1@alpha")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("a refused --set wrote the pointer: %v", statErr)
	}
	for _, bad := range []string{"not-an-id", ""} {
		if err := SetSeats(w.checkout, "dev1@alpha", bad, w.where(), 1); err == nil {
			t.Errorf("SetSeats(%q) accepted", bad)
		}
	}
	if err := SetSeats(w.checkout, "a@b@c", id, w.where(), 1); err == nil {
		t.Error("SetSeats accepted a malformed list")
	}
}

// E1: a pointer that is not the index's own record is refused, naming the file.
func TestAMalformedPointerIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"unknown field": `{"v":1,"seat":"dev1@alpha","session_id":"` + id + `","session":"","source":"set","written_ms":1,"x":1}`,
		"another seat":  `{"v":1,"seat":"qa@beta","session_id":"` + id + `","session":"","source":"set","written_ms":1}`,
		"bad source":    `{"v":1,"seat":"dev1@alpha","session_id":"` + id + `","session":"","source":"guess","written_ms":1}`,
		"bad id":        `{"v":1,"seat":"dev1@alpha","session_id":"nope","session":"","source":"set","written_ms":1}`,
		"version":       `{"v":2,"seat":"dev1@alpha","session_id":"` + id + `","session":"","source":"set","written_ms":1}`,
		"not json":      `{`,
		"too large":     strings.Repeat(" ", 5000),
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			write(t, w.indexPath("dev1@alpha"), body)
			got, err := w.forSeats("dev1@alpha")
			mustRefuse(t, got, err, w.indexPath("dev1@alpha"))
		})
	}
	w := newWorld(t)
	if err := os.MkdirAll(w.indexPath("dev1@alpha"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := w.forSeats("dev1@alpha")
	mustRefuse(t, got, err, "not a regular file")
}

// E4 (d): the agreed conversation's transcript is gone: the path it was expected at is named.
func TestTheAgreedConversationWithNoTranscriptIsRefused(t *testing.T) {
	w := newWorld(t)
	w.pointer("dev1@alpha", thirdID, "ccy-proj", "session", "1791000000000")
	got, err := w.forSeats("dev1@alpha")
	mustRefuse(t, got, err, thirdID, "no transcript on this host",
		filepath.Join(w.checkout, ".claude/ccy/projects/-workspace", thirdID+".jsonl"))
	if !errors.Is(err, ErrNoSeatConversation) {
		t.Errorf("refusal %v is not ErrNoSeatConversation", err)
	}
}

// A plain lookup (ccy-teams seats conversation without --set) only reads: it writes no pointer.
func TestALookupWritesNoPointer(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	got, err := w.forSeats("dev1@alpha")
	if err != nil || got.ID != id {
		t.Fatalf("ForSeats = %+v, %v; want %s", got, err, id)
	}
	if _, err := os.Lstat(w.indexPath("dev1@alpha")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a lookup wrote a pointer: %v", err)
	}
}

// 4a: SaveSeats reads, compares and writes each pointer under the index's lock, so a --set
// written before the lock is taken is read and kept, never overwritten by an older record.
func TestSaveSeatsComparesUnderTheIndexLock(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	w.transcriptFor(otherID)
	locked := false
	indexLocked = func(lockPath string) {
		f, err := os.OpenFile(lockPath, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := f.Close(); err != nil {
				t.Error(err)
			}
		}()
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
			t.Errorf("the index lock is not held while the pointers are compared: %v", err)
		}
		locked = true
		w.pointer("dev1@alpha", otherID, "", "set", "1795000000000")
	}
	t.Cleanup(func() { indexLocked = nil })
	got, err := SaveSeats(w.checkout, "dev1@alpha", w.where())
	if err != nil {
		t.Fatal(err)
	}
	if !locked || len(got.Pointers) != 1 || got.Pointers[0].ID != otherID || !got.Pointers[0].SetByHand() {
		t.Errorf("SaveSeats = %+v (locked %v), want the --set pointer %s kept", got, locked, otherID)
	}
	data, err := os.ReadFile(w.indexPath("dev1@alpha"))
	if err != nil || !strings.Contains(string(data), otherID) {
		t.Errorf("the --set pointer was overwritten: %s, %v", data, err)
	}
}

// 3: a lookup reads only what concerns the named seats: an unreadable report, desired
// membership, control folder or pointer of anything else is ignored.
func TestAnUnrelatedUnreadableRecordIsIgnored(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	write(t, filepath.Join(w.checkout, ".claude/ccy/pingbus/sessions/ccy-proj-2/applied.json"), "{")
	write(t, w.indexPath("qa@beta"), "{")
	got, err := SaveSeats(w.checkout, "dev1@alpha", w.where())
	if err != nil || len(got.Pointers) != 1 || got.Pointers[0].ID != id {
		t.Fatalf("SaveSeats = %+v, %v; want %s despite unrelated unreadable files", got, err, id)
	}
	if s, err := got.Resolve(); err != nil || s.ID != id {
		t.Errorf("Resolve = %+v, %v", s, err)
	}
}

// 3, teamchat: a control folder that is not a session's and another session's unreadable
// desired membership are ignored.
func TestAnUnrelatedUnreadableTeamchatRecordIsIgnored(t *testing.T) {
	w := newWorld(t)
	w.v2Session("ccy-proj", id, 1791000000000, "1@beta")
	if err := os.MkdirAll(filepath.Join(w.store.Root(), "control", "Not A Session"), 0o700); err != nil {
		t.Fatal(err)
	}
	w.v2Session("ccy-proj-2", otherID, 1792000000000, "2@beta")
	name, err := paths.ParseSessionName("ccy-proj-2")
	if err != nil {
		t.Fatal(err)
	}
	write(t, w.store.DesiredFile(name), "{")
	if got, err := w.forSeats("1@beta"); err != nil || got.ID != id {
		t.Errorf("ForSeats = %+v, %v; want %s despite unrelated unreadable files", got, err, id)
	}
}

// 3: the conversation record of a session holding a named seat that cannot be read is refused,
// naming the file.
func TestARelevantUnreadableRecordIsRefusedNamingIt(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	write(t, w.v1Path("ccy-proj"), "{")
	got, err := w.forSeats("dev1@alpha")
	mustRefuse(t, got, err, w.v1Path("ccy-proj"), "ccy-proj")
}

// 5: when the newest session to hold a seat gave no usable record, the older pointer is not
// resumed in its place: the launch is refused naming that session.
func TestTheNewestRecordUnusableIsRefused(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	if _, err := SaveSeats(w.checkout, "dev1@alpha", w.where()); err != nil {
		t.Fatal(err)
	}
	write(t, w.v1Path("ccy-proj-2"), recordedAt("ccy-proj-2", thirdID, "1792000000000"))
	write(t, filepath.Join(w.checkout, ".claude/ccy/pingbus/sessions/ccy-proj-2/applied.json"), applied(t, "dev1@alpha"))
	got, err := w.forSeats("dev1@alpha")
	mustRefuse(t, got, err, "ccy-proj-2", thirdID, "no older one is resumed")
	if !errors.Is(err, ErrNoSeatConversation) {
		t.Errorf("refusal %v is not ErrNoSeatConversation", err)
	}
	// A repair newer than the unusable record is resumed.
	w.transcriptFor(otherID)
	if err := SetSeats(w.checkout, "dev1@alpha", otherID, w.where(), 1793000000000); err != nil {
		t.Fatal(err)
	}
	if got, err := w.forSeats("dev1@alpha"); err != nil || got.ID != otherID {
		t.Errorf("after --set: %+v, %v; want %s", got, err, otherID)
	}
}

// 6: a team taken down loses its seats' pointers in every checkout the session registry names,
// so a team recreated under the name resumes nothing of the old one; other teams' stay.
func TestForgetTeamRemovesItsPointersInEveryCheckout(t *testing.T) {
	w := newWorld(t)
	other := t.TempDir()
	state := t.TempDir()
	reg := registry.New(filepath.Join(state, "sessions"))
	if err := os.MkdirAll(reg.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, dir := range map[string]string{"ccy-p": w.checkout, "ccy-q": other} {
		if err := reg.Write(registry.Record{
			Name: name, Dir: dir, Launcher: "/usr/local/bin/ccy", Prefix: "ccy", Restore: true,
			Args: []string{"--teams", "dev1@alpha"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	ow := &world{t: t, checkout: other}
	for _, x := range []*world{w, ow} {
		x.pointer("dev1@alpha", id, "", "set", "1")
		x.pointer("qa@alpha", id, "", "set", "1")
		x.pointer("dev1@beta", id, "", "set", "1")
	}
	getenv := func(k string) string {
		if k == "CCY_STATE_DIR" {
			return state
		}
		return ""
	}
	if err := ForgetTeam(getenv, "alpha"); err != nil {
		t.Fatal(err)
	}
	for _, x := range []*world{w, ow} {
		for _, gone := range []string{"dev1@alpha", "qa@alpha"} {
			if _, err := os.Lstat(x.indexPath(gone)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%s in %s survived: %v", gone, x.checkout, err)
			}
		}
		if _, err := os.Lstat(x.indexPath("dev1@beta")); err != nil {
			t.Errorf("another team's pointer was removed: %v", err)
		}
	}
	if err := ForgetTeam(getenv, "../x"); err == nil {
		t.Error("ForgetTeam accepted a team name that is not one")
	}
}

// Recorded tells a first launch (nothing recorded, nothing unusable) from one with a record.
func TestRecordedTellsAFirstLaunch(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	w.v1Session("ccy-proj-2", "", "", "qa@alpha")
	for list, want := range map[string]bool{"dev1@alpha": true, "qa@alpha": true, "ops@alpha": false} {
		got, err := LookupSeats(w.checkout, list, w.where())
		if err != nil || got.Recorded() != want {
			t.Errorf("%s: Recorded %v, %v; want %v", list, got.Recorded(), err, want)
		}
	}
}

// Lookup gives each named seat's pointer, also when they disagree (the --resume check).
func TestLookupGivesEachSeatsPointer(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	w.v1Session("ccy-proj-2", otherID, "1792000000000", "qa@beta")
	got, err := LookupSeats(w.checkout, "dev1@alpha,qa@beta,ops@gamma", w.where())
	if err != nil {
		t.Fatal(err)
	}
	if got.Canonical != "dev1@alpha,qa@beta,ops@gamma" || len(got.Pointers) != 2 ||
		got.Pointers[0].Seat != "dev1@alpha" || got.Pointers[0].ID != id || got.Pointers[0].Session != "ccy-proj" ||
		got.Pointers[1].Seat != "qa@beta" || got.Pointers[1].ID != otherID {
		t.Errorf("LookupSeats = %+v", got)
	}
}
