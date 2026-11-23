// Package convtest plants a teamchat (v2) session's conversation for the tests of the host's
// readers of it (sessclose, sessions' restore, teams' restart and wind-down): the pool seat that
// makes each of the record's teams teamchat-live in its checkout, and the link's record where
// conversation.Locate says it is. Test support only.
package convtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
)

// TeamchatSession plants rec's teams in its checkout's pool and its conversation sessionID as a
// teamchat link writes it, and returns the record's path. rec's Dir is its checkout (a cc
// record's Dir is its git checkout's top); each --teams item is seat@team.
func TeamchatSession(tb testing.TB, rec registry.Record, getenv func(string) string, sessionID string) string {
	tb.Helper()
	stateHome, err := paths.StateHome(getenv("HOME"), getenv("XDG_STATE_HOME"))
	must(tb, err)
	store, err := paths.SeatStore(stateHome)
	must(tb, err)
	resolved, err := filepath.EvalSymlinks(rec.Dir)
	must(tb, err)
	key, err := paths.CheckoutKeyOf(resolved)
	must(tb, err)
	for item := range strings.SplitSeq(discover.RecordTeams(rec), ",") {
		at := strings.LastIndex(item, "@")
		team, teamErr := paths.ParseTeamID(item[at+1:])
		must(tb, teamErr)
		must(tb, os.MkdirAll(store.PoolTeam(key, team)+"/repo.1+site.host", 0o700))
	}
	loc, err := conversation.Locate(rec, getenv)
	must(tb, err)
	run, err := paths.ParseRunID("0123456789abcdef")
	must(tb, err)
	data, err := state.Conversation{Run: run, SessionID: sessionID, Source: state.SourceStartup, WrittenMS: 1}.Encode()
	must(tb, err)
	must(tb, os.MkdirAll(filepath.Dir(loc.Path), 0o700))
	must(tb, os.WriteFile(loc.Path, data, 0o600))
	return loc.Path
}

func must(tb testing.TB, err error) {
	tb.Helper()
	if err != nil {
		tb.Fatal(err)
	}
}
