package conversation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const thirdID = "11111111-2222-4333-8444-555555555555"

// seatTeam splits a test's seat@team.
func seatTeam(tb testing.TB, s string) (string, string) {
	tb.Helper()
	seat, team, found := strings.Cut(s, "@")
	if !found {
		tb.Fatalf("%q is not seat@team", s)
	}
	return seat, team
}

// applied is a seat keeper's report holding each seat@team.
func applied(tb testing.TB, seats ...string) string {
	tb.Helper()
	items := make([]string, 0, len(seats))
	for _, s := range seats {
		seat, team := seatTeam(tb, s)
		items = append(items, `{"team":"`+team+`","seat":"`+seat+`","handle":"proj.`+seat+`+local.podman","state":"held"}`)
	}
	return `{"v":1,"generation":1,"seats":[` + strings.Join(items, ",") + `],"refused":[]}`
}

// recordedAt is the hook's record of sessionID for session, written at ms.
func recordedAt(session, sessionID, ms string) string {
	return `{"v":1,"session":"` + session + `","session_id":"` + sessionID + `","source":"startup","written_ms":` + ms + `}`
}

// v1Session plants a v1 session of the world's checkout: its keeper's report holding seats (none:
// no report), its conversation record (sessionID "": none) and that conversation's transcript.
func (w *world) v1Session(session, sessionID, ms string, seats ...string) {
	w.t.Helper()
	dir := filepath.Join(w.checkout, ".claude/ccy/pingbus/sessions", session)
	if len(seats) > 0 {
		write(w.t, filepath.Join(dir, "applied.json"), applied(w.t, seats...))
	}
	if sessionID == "" {
		return
	}
	write(w.t, w.v1Path(session), recordedAt(session, sessionID, ms))
	write(w.t, filepath.Join(w.checkout, ".claude/ccy/projects/-workspace", sessionID+".jsonl"), "{}\n")
}

// v2Session plants a teamchat session: its desired.json naming seats in the seat store's control
// folder, and its link's conversation record with the transcript in the world's checkout.
func (w *world) v2Session(session, sessionID string, ms int64, seats ...string) {
	w.t.Helper()
	name, err := paths.ParseSessionName(session)
	if err != nil {
		w.t.Fatal(err)
	}
	d := state.Desired{Session: name, Generation: 1, WrittenMS: 1, Seats: []state.DesiredSeat{}, Farewell: []state.Farewell{}, Inbound: state.InboundAccept}
	for _, s := range seats {
		seat, team := seatTeam(w.t, s)
		teamID, idErr := paths.ParseTeamID(team)
		if idErr != nil {
			w.t.Fatal(idErr)
		}
		d.Seats = append(d.Seats, state.DesiredSeat{Team: teamID, Seat: seat})
		w.poolSeat(team)
	}
	data, err := d.Encode()
	if err != nil {
		w.t.Fatal(err)
	}
	write(w.t, w.store.DesiredFile(name), string(data))
	run, err := paths.ParseRunID("0123456789abcdef")
	if err != nil {
		w.t.Fatal(err)
	}
	conv, err := state.Conversation{Run: run, SessionID: sessionID, Source: state.SourceStartup, WrittenMS: ms}.Encode()
	if err != nil {
		w.t.Fatal(err)
	}
	write(w.t, w.v2Path(session), string(conv))
	write(w.t, filepath.Join(w.checkout, ".claude/ccy/projects/-workspace", sessionID+".jsonl"), "{}\n")
}

func (w *world) forSeats(list string) (Seated, error) {
	return ForSeats(w.checkout, list, w.where())
}

func mustRefuse(t *testing.T, got Seated, err error, says ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("ForSeats gave %+v, want a refusal", got)
	}
	for _, s := range says {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("refusal %q does not say %q", err, s)
		}
	}
}

// The id is the one recorded by the session whose keeper last held the seat, never the newest
// transcript: another session's newer conversation is there too.
func TestForSeatsIsTheRecordOfTheSessionThatHeldTheSeat(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	w.v1Session("ccy-proj-2", otherID, "1792000000000", "dev2@alpha")
	got, err := w.forSeats("dev1@alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id || got.Session != "ccy-proj" {
		t.Errorf("ForSeats = %+v, want %s of ccy-proj", got, id)
	}
}

// E3: a subset or a superset of a session's seats resolves when every named seat with a pointer
// agrees; a named seat with none is allowed beside one that has one, and reported.
func TestForSeatsOfASubsetOrSupersetIsTheSeatsAgreedConversation(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha", "qa@beta-team")
	for _, list := range []string{"dev1@alpha, qa@beta-team", "dev1@alpha", "qa@beta-team"} {
		got, err := w.forSeats(list)
		if err != nil {
			t.Fatalf("%s: %v", list, err)
		}
		if got.ID != id || len(got.Unrecorded) != 0 {
			t.Errorf("%s: ForSeats = %+v, want %s", list, got, id)
		}
	}
	got, err := w.forSeats("dev1@alpha,ops@gamma")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id || len(got.Unrecorded) != 1 || got.Unrecorded[0] != "ops@gamma" {
		t.Errorf("superset: ForSeats = %+v, want %s with ops@gamma unrecorded", got, id)
	}
}

// E4 (c): named seats whose pointers disagree are the ambiguous case, each seat named.
func TestForSeatsRefusesSeatsThatDisagree(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	w.v1Session("ccy-proj-2", otherID, "1792000000000", "dev1@other")
	got, err := w.forSeats("dev1@alpha,dev1@other")
	mustRefuse(t, got, err, "disagree", "dev1@alpha: conversation "+id+", recorded by session ccy-proj",
		"dev1@other: conversation "+otherID+", recorded by session ccy-proj-2")
	if errors.Is(err, ErrNoSeatConversation) {
		t.Errorf("a disagreement is not no conversation: %v", err)
	}
	if !errors.Is(err, ErrSeatsDisagree) {
		t.Errorf("refusal %v is not ErrSeatsDisagree", err)
	}
}

// E4 (a): no named seat has a record and no session held any.
func TestForSeatsRefusesASeatNoSessionHeld(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	got, err := w.forSeats("dev9@alpha")
	mustRefuse(t, got, err, "no conversation is recorded for dev9@alpha in "+w.checkout+": no session held")
	if !errors.Is(err, ErrNoSeatConversation) {
		t.Errorf("refusal %v is not ErrNoSeatConversation", err)
	}
}

// E4 (b): a session that held the seat but recorded nothing is said so, with its reason.
func TestForSeatsRefusesASessionWithNoRecord(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", "", "", "dev1@alpha")
	got, err := w.forSeats("dev1@alpha")
	mustRefuse(t, got, err, "ccy-proj", "dev1@alpha", "no conversation is recorded")
	if !errors.Is(err, ErrNoSeatConversation) {
		t.Errorf("refusal %v is not ErrNoSeatConversation", err)
	}
}

// One seat held by two sessions in turn (a seat is held by one session at a time): the seat's
// pointer is the newer record.
func TestForSeatsTakesTheNewerOfTwoSessionsThatHeldTheSeat(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	w.v1Session("ccy-proj-2", otherID, "1792000000000", "dev1@alpha")
	got, err := w.forSeats("dev1@alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != otherID || got.Session != "ccy-proj-2" {
		t.Errorf("ForSeats = %+v, want %s of ccy-proj-2", got, otherID)
	}
}

// The seats of a session that took them together (recorded too) share its conversation; a
// launch of either alone, or both, resumes it.
func TestForSeatsResolvesAConversationTwoSeatsShare(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1792000000000", "1@alpha")
	w.v1Session("ccy-proj-3", id, "1791000000000", "3@other")
	w.v1Session("ccy-proj-4", thirdID, "1793000000000")
	for _, list := range []string{"1@alpha", "3@other", "1@alpha,3@other"} {
		got, err := w.forSeats(list)
		if err != nil {
			t.Fatalf("%s: %v", list, err)
		}
		if got.ID != id {
			t.Errorf("%s: ForSeats = %+v, want %s", list, got, id)
		}
	}
}

func TestForSeatsReadsATeamchatSessionFromTheSeatStore(t *testing.T) {
	w := newWorld(t)
	w.v2Session("ccy-proj", id, 1791000000000, "1@beta")
	w.v2Session("ccy-proj-2", otherID, 1792000000000, "2@beta")
	got, err := w.forSeats("1@beta")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id || got.Session != "ccy-proj" {
		t.Errorf("ForSeats = %+v, want %s of ccy-proj", got, id)
	}
}

// The seat store is the owner's for every checkout: a session whose conversation has no
// transcript in this checkout is another checkout's, and never this seat's pointer.
func TestForSeatsIgnoresATeamchatSessionOfAnotherCheckout(t *testing.T) {
	w := newWorld(t)
	w.v2Session("ccy-proj", id, 1791000000000, "1@beta")
	w.v2Session("ccy-other", otherID, 1792000000000, "1@beta")
	if err := os.Remove(filepath.Join(w.checkout, ".claude/ccy/projects/-workspace", otherID+".jsonl")); err != nil {
		t.Fatal(err)
	}
	got, err := w.forSeats("1@beta")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id {
		t.Errorf("ForSeats = %+v, want %s", got, id)
	}
}

func TestForSeatsRefusesAMalformedList(t *testing.T) {
	w := newWorld(t)
	for _, list := range []string{"", "alpha", "a@b@c"} {
		got, err := w.forSeats(list)
		mustRefuse(t, got, err)
	}
}
