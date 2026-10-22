package teamsync

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The world's accounts: me is the member under test, a worker; coord the coordinator; peer
// another worker (agent X of the tests); deaf a worker that reads no human text.
const (
	teamName = "alpha"
	me       = "myrepo.2+desk.podman"
	coord    = "myrepo.1+desk.podman"
	peer     = "myrepo.3+desk.podman"
	deaf     = "myrepo.4+desk.podman"
	alice    = "alice"
	startMS  = int64(1_760_000_000_000)
	linkRun  = "00000000000000aa"
	oldRun   = "00000000000000bb"
)

var (
	alphaID = func() paths.TeamID {
		t, err := paths.ParseTeamID(teamName)
		if err != nil {
			panic(err)
		}
		return t
	}()
	server = paths.ServerName(alphaID)
)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func user(local string) string { return proto.UserID(local, server) }

func runID(t *testing.T, s string) paths.RunID {
	t.Helper()
	r, err := paths.ParseRunID(s)
	check(t, err)
	return r
}

// syncBuffer is link.log, written by the team goroutine and read by the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) count(s string) int { return strings.Count(b.String(), s) }

// human is a plain human message mentioning ids.
func human(body string, mentions ...string) map[string]any {
	c := map[string]any{"msgtype": "m.text", "body": body}
	if mentions != nil {
		c["m.mentions"] = map[string]any{"user_ids": mentions}
	}
	return c
}

// reply is a human message replying to id.
func reply(body, id string, mentions ...string) map[string]any {
	c := human(body, mentions...)
	c["m.relates_to"] = map[string]any{"m.in_reply_to": map[string]any{"event_id": id}}
	return c
}

// inThread is a human message in root's thread.
func inThread(body, root string) map[string]any {
	c := human(body)
	c["m.relates_to"] = map[string]any{"rel_type": "m.thread", "event_id": root, "is_falling_back": true, "m.in_reply_to": map[string]any{"event_id": root}}
	return c
}

func baseRoster() proto.Roster {
	return proto.Roster{
		Team: teamName, Name: "Alpha", Humans: []string{user(alice)},
		Members: map[string]proto.RosterMember{
			user(coord): {Role: proto.RoleCoordinator, HumanText: true},
			user(me):    {Role: proto.RoleWorker, HumanText: true},
			user(peer):  {Role: proto.RoleWorker, HumanText: true},
			user(deaf):  {Role: proto.RoleWorker, HumanText: false},
		},
		Forge: proto.Forge{Kind: proto.ForgeGitHub, Org: "example-org"}, Limits: proto.DefaultLimits(),
	}
}
