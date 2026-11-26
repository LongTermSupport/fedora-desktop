package conversation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
)

// renamed is a ccy session renamed from ccy-proj: its checkout records ccy-proj's conversation,
// with its transcript, and nothing under the new name.
func renamed(t *testing.T) registry.Record {
	t.Helper()
	return record("ccy-renamed", "ccy", ccyCase(t).Dir)
}

func mustSay(t *testing.T, err error, says ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("StartPlan started the session, want a refusal")
	}
	for _, s := range says {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q does not say %q", err, s)
		}
	}
}

// fedora-desktop#431: a renamed session whose conversation is recorded under its old name only
// is never started fresh, marked or not; the refusal names the old record and how to resume it.
func TestStartPlanRefusesARenamedSessionWhoseConversationIsUnderItsOldName(t *testing.T) {
	for _, marked := range []bool{true, false} {
		rec := renamed(t)
		rec.FreshIfNoConversation = marked
		got, err := StartPlan(rec, none)
		if got.Fresh {
			t.Fatalf("marked %v: started fresh: %+v", marked, got)
		}
		mustSay(t, err, "ccy-renamed", "ccy-proj", id, recordPath(rec.Dir), "ccy --resume "+id+" in "+rec.Dir)
	}
}

// The issue's own case: a teamchat (v2) seated session, renamed, whose v2 record and seat record
// are both missing and whose conversation is in the v1 record of its old name. It is refused,
// naming the seat repair with the id; once the seats are pointed at it, it is resumed.
func TestStartPlanRefusesARenamedTeamchatSessionAndResumesItOnceItsSeatsAreSet(t *testing.T) {
	w := newWorld(t)
	w.poolSeat("team-a")
	rec := record("ccy-team-a-proj-1", "ccy", w.checkout, "--teams", "1@team-a")
	rec.FreshIfNoConversation = true
	write(t, w.v1Path("ccy-proj"), recorded("ccy-proj", id))
	write(t, w.transcriptOf(rec, id), "{}\n")
	got, err := StartPlan(rec, w.where())
	if got.Fresh {
		t.Fatalf("started fresh: %+v", got)
	}
	mustSay(t, err, "ccy-proj", id, "ccy-teams seats conversation 1@team-a --set "+id)
	if err := RecordSeats(w.checkout, "1@team-a", id, SourceSet, 1); err != nil {
		t.Fatal(err)
	}
	got, err = StartPlan(rec, w.where())
	if err != nil || got.Fresh || got.ID != id {
		t.Fatalf("after the repair: StartPlan = %+v, %v; want %s resumed", got, err, id)
	}
}

// A conversation another recorded session owns is that session's, so it does not stop a marked
// record that has never run from starting fresh.
func TestStartPlanStartsFreshBesideAConversationAnotherRecordOwns(t *testing.T) {
	rec := renamed(t)
	rec.FreshIfNoConversation = true
	state := t.TempDir()
	owner := record("ccy-proj", "ccy", rec.Dir)
	if err := registry.New(filepath.Join(state, "sessions")).Write(owner); err != nil {
		t.Fatal(err)
	}
	where := Where{ClaudeHome: "/nonexistent", Getenv: func(k string) string {
		switch k {
		case "HOME":
			return "/nonexistent"
		case "CCY_STATE_DIR":
			return state
		}
		return ""
	}}
	got, err := StartPlan(rec, where)
	if err != nil || !got.Fresh {
		t.Fatalf("StartPlan = %+v, %v; want a fresh start", got, err)
	}
}

// A record under another name whose conversation has no transcript has nothing to resume, so a
// marked record still starts fresh beside it.
func TestStartPlanStartsFreshBesideAnOldRecordWithNoTranscript(t *testing.T) {
	rec := renamed(t)
	rec.FreshIfNoConversation = true
	if err := os.Remove(transcriptPath(rec.Dir)); err != nil {
		t.Fatal(err)
	}
	got, err := StartPlan(rec, none)
	if err != nil || !got.Fresh {
		t.Fatalf("StartPlan = %+v, %v; want a fresh start", got, err)
	}
}

// A record under another name that cannot be read might be this session's, so it stops a fresh
// start and is named.
func TestStartPlanRefusesBesideAnOldRecordThatCannotBeRead(t *testing.T) {
	rec := renamed(t)
	rec.FreshIfNoConversation = true
	write(t, recordPath(rec.Dir), "not json")
	got, err := StartPlan(rec, none)
	if got.Fresh {
		t.Fatalf("started fresh: %+v", got)
	}
	mustSay(t, err, "ccy-proj", recordPath(rec.Dir))
}
