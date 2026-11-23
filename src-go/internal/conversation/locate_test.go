package conversation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
)

// v2Recorded is a teamchat link's conversation record (state.Conversation's bytes).
func v2Recorded(sessionID string) string {
	return `{"v":1,"run":"0123456789abcdef","session_id":"` + sessionID + `","source":"compact","written_ms":1791000000000}`
}

// world is an owner's home with a seat store, a checkout and Claude's home.
type world struct {
	t                      *testing.T
	home, claude, checkout string
	store                  paths.Store
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, home: t.TempDir(), claude: t.TempDir(), checkout: t.TempDir()}
	if err := os.Mkdir(filepath.Join(w.checkout, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := paths.SeatStore(filepath.Join(w.home, ".local/state"))
	if err != nil {
		t.Fatal(err)
	}
	w.store = store
	return w
}

func (w *world) where() Where {
	return Where{ClaudeHome: w.claude, Getenv: func(k string) string {
		if k == "HOME" {
			return w.home
		}
		return ""
	}}
}

// poolSeat puts a seat of team in the checkout's pool, which makes the team teamchat-live here.
func (w *world) poolSeat(team string) {
	w.t.Helper()
	resolved, err := filepath.EvalSymlinks(w.checkout)
	if err != nil {
		w.t.Fatal(err)
	}
	key, err := paths.CheckoutKeyOf(resolved)
	if err != nil {
		w.t.Fatal(err)
	}
	id, err := paths.ParseTeamID(team)
	if err != nil {
		w.t.Fatal(err)
	}
	if err := os.MkdirAll(w.store.PoolTeam(key, id)+"/repo.1+site.host", 0o700); err != nil {
		w.t.Fatal(err)
	}
}

// v1Path and v2Path are where each system keeps the session's record, spelt here independently
// of Locate.
func (w *world) v1Path(session string) string {
	return filepath.Join(w.checkout, ".claude/ccy/pingbus/sessions", session, "conversation.json")
}

func (w *world) v2Path(session string) string {
	return filepath.Join(w.store.Root(), "sessions", session, "conversation.json")
}

func (w *world) transcriptOf(rec registry.Record, sessionID string) string {
	w.t.Helper()
	dir, err := transcript.Dir(rec.Prefix, rec.Dir, w.claude)
	if err != nil {
		w.t.Fatal(err)
	}
	return filepath.Join(dir, sessionID+".jsonl")
}

// The locator's table (DESIGN-host-cc-v2.md section 6 step 1): {ccy, cc} × {agent-bus,
// teamchat, mixed, no list} × {record present, missing, wrong session id, no transcript}. Both
// systems' records are planted, the other one naming another conversation, so a reader that falls
// back to the other record, or reads the wrong one, gives the wrong id.
func TestLocateReadsTheRecordOfTheSystemItsTeamsAreOn(t *testing.T) {
	lists := map[string]struct {
		args   func(prefix string) []string
		system string
	}{
		"agent-bus": {func(string) []string { return []string{"--teams", "1@alpha"} }, system.AgentBus},
		"teamchat":  {func(string) []string { return []string{"--teams", "1@beta"} }, system.Teamchat},
		"mixed":     {func(string) []string { return []string{"--teams", "1@alpha,1@beta"} }, ""},
		"no list": {func(prefix string) []string {
			if prefix == "cc" {
				return []string{"--bus"}
			}
			return nil
		}, system.AgentBus},
	}
	for _, prefix := range []string{"ccy", "cc"} {
		for listName, list := range lists {
			for _, st := range []string{"present", "missing", "wrong session id", "no transcript"} {
				t.Run(prefix+"/"+listName+"/"+st, func(t *testing.T) {
					locateCase(t, prefix, list.args(prefix), list.system, st)
				})
			}
		}
	}
}

// locateCase is one row: a session of prefix launched with args, whose teams are on sys ("" for
// both), with its own record in state st.
func locateCase(t *testing.T, prefix string, args []string, sys, st string) {
	t.Helper()
	w := newWorld(t)
	w.poolSeat("beta")
	session := prefix + "-proj"
	rec := registry.Record{Name: session, Dir: w.checkout, Prefix: prefix, Restore: true, Args: args}
	mine, other := w.v1Path(session), w.v2Path(session)
	mineBody, otherBody := recorded(session, id), v2Recorded(otherID)
	if sys == system.Teamchat {
		mine, other = other, mine
		mineBody, otherBody = v2Recorded(id), recorded(session, otherID)
	}
	write(t, other, otherBody)
	write(t, w.transcriptOf(rec, otherID), "{}\n")
	if st != "no transcript" {
		write(t, w.transcriptOf(rec, id), "{}\n")
	}
	switch st {
	case "present", "no transcript":
		write(t, mine, mineBody)
	case "wrong session id":
		write(t, mine, strings.ReplaceAll(mineBody, id, "d2dae64d"))
	}
	got, err := For(rec, w.where())
	if sys == "" {
		if !errors.Is(err, system.ErrRefused) || !strings.Contains(err.Error(), "both systems") {
			t.Fatalf("a mixed list gave %q, %v; want the refusal", got, err)
		}
		return
	}
	loc, locErr := Locate(rec, w.where().Getenv)
	if locErr != nil || loc.Path != mine || loc.System != sys {
		t.Fatalf("Locate = %+v, %v; want %s on %s", loc, locErr, mine, sys)
	}
	want := map[string]func() bool{
		"present": func() bool { return err == nil && got == id },
		"missing": func() bool { return errors.Is(err, errNotRecorded) && strings.Contains(err.Error(), mine) },
		"wrong session id": func() bool {
			return err != nil && !errors.Is(err, errNotRecorded) && !errors.Is(err, errNoTranscript)
		},
		"no transcript": func() bool { return errors.Is(err, errNoTranscript) },
	}
	if !want[st]() {
		t.Fatalf("%s: For = %q, %v (record %s)", st, got, err, mine)
	}
}

// A teamchat session's record is not a v1 record: an unknown key, or the v1 shape, is refused.
func TestLocateRefusesWhatIsNotTheLinksRecord(t *testing.T) {
	for name, body := range map[string]string{
		"the v1 shape":   recorded("cc-proj", id),
		"an unknown key": strings.Replace(v2Recorded(id), `"v":1`, `"v":1,"x":1`, 1),
		"not JSON":       "not json",
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.poolSeat("beta")
			rec := registry.Record{Name: "cc-proj", Dir: w.checkout, Prefix: "cc", Args: []string{"--teams", "1@beta"}}
			write(t, w.v2Path("cc-proj"), body)
			write(t, w.transcriptOf(rec, id), "{}\n")
			got, err := For(rec, w.where())
			if !errors.Is(err, errNotARecord) {
				t.Fatalf("For = %q, %v; want the record refused", got, err)
			}
		})
	}
}

// A teamchat list needs the owner's home to find the seat store; none is a refusal, not v1.
func TestLocateNeedsTheOwnersHomeForATeamchatList(t *testing.T) {
	rec := registry.Record{Name: "cc-proj", Dir: t.TempDir(), Prefix: "ccy", Args: []string{"--teams", "1@beta"}}
	_, err := Locate(rec, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "state home") {
		t.Fatalf("Locate with no HOME: %v", err)
	}
}

// Here is Claude's home and the environment it was given.
func TestHereIsClaudesHomeAndTheEnvironment(t *testing.T) {
	env := map[string]string{"HOME": "/home/u"}
	w := Here(func(k string) string { return env[k] })
	if w.ClaudeHome != "/home/u/.claude" || w.Getenv("HOME") != "/home/u" {
		t.Fatalf("Here = %+v", w)
	}
}
