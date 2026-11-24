package conversation

import (
	"errors"
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

// A list of several seats is the one session that held them all.
func TestForSeatsOfAListIsTheSessionHoldingEveryOne(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha", "qa@beta-team")
	w.v1Session("ccy-proj-2", otherID, "1792000000000", "dev1@other")
	got, err := w.forSeats("dev1@alpha, qa@beta-team")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id {
		t.Errorf("ForSeats = %+v, want %s", got, id)
	}
	got, err = w.forSeats("dev1@alpha,dev1@other")
	mustRefuse(t, got, err, "no session record", "dev1@alpha,dev1@other")
}

func TestForSeatsRefusesASeatNoSessionHeld(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	got, err := w.forSeats("dev9@alpha")
	mustRefuse(t, got, err, "no session record in "+w.checkout+" holds dev9@alpha")
	if !errors.Is(err, ErrNoSeatConversation) {
		t.Errorf("refusal %v is not ErrNoSeatConversation", err)
	}
}

// A session that held the seat but recorded nothing is said so, with its reason.
func TestForSeatsRefusesASessionWithNoRecord(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", "", "", "dev1@alpha")
	got, err := w.forSeats("dev1@alpha")
	mustRefuse(t, got, err, "ccy-proj", "dev1@alpha", "no conversation is recorded")
	if !errors.Is(err, ErrNoSeatConversation) {
		t.Errorf("refusal %v is not ErrNoSeatConversation", err)
	}
}

// Two sessions that each held the seat with their own conversation: which is the seat's is not
// chosen; both are named, the newest record marked.
func TestForSeatsRefusesTwoSessionsThatHeldTheSeat(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1791000000000", "dev1@alpha")
	w.v1Session("ccy-proj-2", otherID, "1792000000000", "dev1@alpha")
	got, err := w.forSeats("dev1@alpha")
	mustRefuse(t, got, err, "more than one session", "ccy-proj (seats", "ccy-proj-2 (seats", id, otherID,
		otherID+", recorded 2026-10-14T17:46:40Z (the newest)")
	if strings.Contains(err.Error(), id+", recorded 2026-10-03T04:00:00Z (the newest)") {
		t.Errorf("the older record is marked newest: %v", err)
	}
	if errors.Is(err, ErrNoSeatConversation) {
		t.Errorf("two candidates are not no conversation: %v", err)
	}
}

// The live failure: two sessions recorded the same conversation (one record stale). The id is
// flagged, never printed as the answer, and every session recording it is named with its seats.
func TestForSeatsRefusesAConversationRecordedByTwoSessions(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1792000000000", "1@alpha")
	w.v1Session("ccy-proj-3", id, "1791000000000", "3@alpha")
	w.v1Session("ccy-proj-4", thirdID, "1793000000000")
	got, err := w.forSeats("1@alpha")
	mustRefuse(t, got, err, "recorded by more than one session", id,
		"ccy-proj (seats 1@alpha", "ccy-proj-3 (seats 3@alpha", "(the newest)", "not chosen for you")
	if strings.Contains(err.Error(), "ccy-proj-4") {
		t.Errorf("a session recording another conversation is named: %v", err)
	}
	if errors.Is(err, ErrNoSeatConversation) {
		t.Errorf("a duplicate is not no conversation: %v", err)
	}
}

// A session with no keeper's report still counts for the duplicate check: its record names the id.
func TestForSeatsDuplicateIsFoundInASessionWithNoReport(t *testing.T) {
	w := newWorld(t)
	w.v1Session("ccy-proj", id, "1792000000000", "1@alpha")
	w.v1Session("ccy-proj-9", id, "1791000000000")
	got, err := w.forSeats("1@alpha")
	mustRefuse(t, got, err, "recorded by more than one session", "ccy-proj-9 (no seats)")
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

func TestForSeatsRefusesATeamchatConversationRecordedTwice(t *testing.T) {
	w := newWorld(t)
	w.v2Session("ccy-proj", id, 1791000000000, "1@beta")
	w.v2Session("ccy-proj-2", id, 1792000000000, "2@beta")
	got, err := w.forSeats("1@beta")
	mustRefuse(t, got, err, "recorded by more than one session", "ccy-proj (seats 1@beta", "ccy-proj-2 (seats 2@beta")
}

func TestForSeatsRefusesAMalformedList(t *testing.T) {
	w := newWorld(t)
	for _, list := range []string{"", "alpha", "a@b@c"} {
		got, err := w.forSeats(list)
		mustRefuse(t, got, err)
	}
}
