package conversation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// E2: a lookup writes each held seat's pointer, whole, so the seat keeps its conversation when
// its session's folder is later reused by a launch of other seats.
func TestSeatKeepsItsConversationWhenItsSessionFolderIsReused(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	if got, err := w.forSeats("qa@beta"); err == nil {
		t.Fatalf("qa@beta resolved to %+v with no record", got)
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
