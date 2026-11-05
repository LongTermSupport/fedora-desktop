package wiring

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The sends end to end (n3d W, handoff section 11): a coordinator C and a worker K on team
// alpha, each its own `run --plain` with its own state root, over fakehs. One row carries a
// reference, to prove the link's forge wiring with no gh; the forge's answers are proven in
// process (SD5, SD1-2).

const (
	// kShort is K's short name (P69), what C's --to names it by.
	kShort = "myrepo.2"
	// coordSeat is C's handle's <seat>.
	coordSeat = "1"
)

// pair is K (me, the world) and C (coord, its member world) on hs, both seated.
func pair(t *testing.T, hs homeserver) (*world, *world, *session, *session) {
	t.Helper()
	k := newWorld(t, hs)
	c := k.member(coord, coordSeat, "c")
	return k, c, k.seated(), c.seated()
}

// send runs a send verb in the session with text on its stdin, none when text is nil, as its
// CMD would.
func (s *session) send(text *string, args ...string) execx.Result {
	s.w.t.Helper()
	cmd := execx.Cmd{Name: s.w.kit + "/bin/teamchat", Args: args, Env: s.w.env(paths.EnvRuntime + "=" + s.runtime)}
	if text != nil {
		cmd.Stdin = strings.NewReader(*text)
	}
	res, err := execx.OS{}.Run(s.w.t.Context(), cmd)
	if exited, isExit := errors.AsType[*execx.ExitError](err); err != nil && (!isExit || exited.Signal != 0) {
		s.w.t.Fatalf("%v: %v", args, err)
	}
	return res
}

// sent is a send that must answer one sent record, exit 0: its record.
func (s *session) sent(text *string, args ...string) map[string]any {
	s.w.t.Helper()
	res := s.send(text, args...)
	recs := parseRecords(s.w.t, res.Stdout)
	if res.ExitCode != 0 || len(recs) != 1 || recs[0]["kind"] != "sent" {
		s.w.t.Fatalf("%v: exit %d, records %v, stderr %s", args, res.ExitCode, recs, res.Stderr)
	}
	return recs[0]
}

// posted is a send that must be posted at once: its event id.
func (s *session) posted(text *string, args ...string) string {
	s.w.t.Helper()
	r := s.sent(text, args...)
	id, ok := r["id"].(string)
	if !ok || r["queued"] == true {
		s.w.t.Fatalf("%v: not posted: %v", args, r)
	}
	return id
}

// queued is a send that must be held in the outbox: exit 0, id null, queued (n3d-D2).
func (s *session) queued(text *string, args ...string) {
	s.w.t.Helper()
	if r := s.sent(text, args...); r["id"] != nil || r["queued"] != true {
		s.w.t.Fatalf("%v: not queued: %v", args, r)
	}
}

// status is the session's status records of kind for the world's team.
func (s *session) status(kind string) []map[string]any {
	s.w.t.Helper()
	var out []map[string]any
	for _, r := range s.records("status", "--team", s.w.team.name) {
		if r["kind"] == kind {
			out = append(out, r)
		}
	}
	return out
}

// textsFrom are the ids of local's teamchat.text events in main whose text is text.
func (w *world) textsFrom(local, text string) []string {
	w.t.Helper()
	page, err := w.client(alice).Messages(w.t.Context(), w.main, matrix.MessagesRequest{Dir: matrix.Backward, Limit: 100})
	check(w.t, err)
	var ids []string
	for _, raw := range page.Chunk {
		v, err := proto.Parse(raw)
		check(w.t, err)
		content, ok := v.Get("content")
		if !ok || str(v, "sender") != w.user(local) {
			continue
		}
		if body, ok := content.Get(proto.KeyText); ok && str(body, "text") == text {
			ids = append(ids, str(v, "event_id"))
		}
	}
	return ids
}

// postedOnce waits until C's queue is empty and its text is in main, then holds that it is
// there exactly once: its event id. A team whose server is back resumes within the backoff's cap.
func (s *session) postedOnce(text string) string {
	s.w.t.Helper()
	within(s.w.t, backoffCap+settle, func() bool {
		return len(s.status("queued")) == 0 && len(s.w.textsFrom(coord, text)) > 0
	}, s.describe)
	ids := s.w.textsFrom(coord, text)
	if len(ids) != 1 {
		s.w.t.Fatalf("%q is in main %d times: %s", text, len(ids), s.describe())
	}
	return ids[0]
}

// SD2, SD1-2: C's halt reaches K as a PING; K's `reply --verb ack` answers it, and C's status
// shows the outbox ping answered by K with ack (n3d-D17: status is where --plain reads it).
func TestE2EPingAnswered(t *testing.T) { pingAnswered(t, fakeServer(t)) }

func pingAnswered(t *testing.T, hs homeserver) {
	t.Helper()
	_, _, ks, cs := pair(t, hs)
	ping := cs.posted(nil, "ping", "halt", "--to", kShort, "--team", teamName)
	if recs := ks.recvUntil(ping); !hasKind(recs, ping, "ping") {
		t.Fatalf("K's recv: %v", recs)
	}
	ks.posted(nil, "reply", ping, "--verb", "ack", "--team", teamName)
	eventually(t, func() bool {
		for _, r := range cs.status("outbox") {
			answers, ok := r["answers"].([]any)
			if r["id"] != ping || !ok || len(answers) != 1 {
				continue
			}
			a, ok := answers[0].(map[string]any)
			from, isText := a["from"].(string)
			return ok && isText && a["verb"] == "ack" && strings.Contains(from, kShort)
		}
		return false
	}, cs.describe)
}

// hasKind is whether recs hold id as a record of kind.
func hasKind(recs []map[string]any, id, kind string) bool {
	for _, r := range recs {
		if r["id"] == id && r["kind"] == kind {
			return true
		}
	}
	return false
}

// SD2, SD1-1: C's say reaches K as a PEER; K's plain reply, its text on stdin, reaches C as a
// PEER.
func TestE2ESayAndReply(t *testing.T) { sayAndReply(t, fakeServer(t)) }

func sayAndReply(t *testing.T, hs homeserver) {
	t.Helper()
	_, _, ks, cs := pair(t, hs)
	said := cs.posted(new("over to you\n"), "say", "--to", kShort, "--team", teamName)
	if recs := ks.recvUntil(said); !hasKind(recs, said, "peer") {
		t.Fatalf("K's recv: %v", recs)
	}
	reply := ks.posted(new("done it"), "reply", said, "--team", teamName)
	if recs := cs.recvUntil(reply); !hasKind(recs, reply, "peer") {
		t.Fatalf("C's recv: %v", recs)
	}
}

// n3d-D11, n3d-D10: C's referenced ping runs the link's own forge checker, as link/main.go
// wires it (link, engine, forge.Checker, Login, execx): with no gh on PATH (the world's
// environment names no PATH) it is 78 forge-login, and nothing is queued. A checker left
// unwired, or a nil Login, fails this row.
func TestE2EAReferencedPingWithNoGhIsForgeLogin(t *testing.T) {
	c := newWorld(t, fakeServer(t)).member(coord, coordSeat, "c")
	cs := c.seated()
	reference := "commit:example-org/myrepo@" + strings.Repeat("ab", 20)
	res := cs.send(nil, "ping", "review", reference, "--to", kShort, "--team", teamName)
	if res.ExitCode != 78 || !strings.Contains(string(res.Stderr), `"code":"forge-login"`) {
		t.Fatalf("the referenced ping: exit %d, stdout %s, stderr %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	if q := cs.status("queued"); len(q) != 0 {
		t.Fatalf("a refused ping was queued: %v", q)
	}
}

// The owner's offline rule (n3d-D2): with the homeserver down, C's say answers queued, exit 0,
// and status lists it; once the server is back it is posted, and K receives it once.
func TestE2EQueuedWhileDown(t *testing.T) {
	k, _, ks, cs := pair(t, fakeServer(t))
	check(t, k.hs.fake.Stop())
	eventually(t, func() bool { return cs.down(teamName) }, cs.describe)
	const text = "said while the server was down"
	cs.queued(new(text), "say", "--to", kShort, "--team", teamName)
	if q := cs.status("queued"); len(q) != 1 || q[0]["send"] != "text" {
		t.Fatalf("status: %v", q)
	}
	check(t, k.hs.fake.Start())
	ks.recvUntil(cs.postedOnce(text))
}

// n3d-D3, n3d-D1: a link started with its homeserver down, after an earlier run kept the
// verified roster in roster.json, still queues C's say; it is posted once the server is back.
func TestE2EQueuedAtStart(t *testing.T) {
	k, c, ks, cs := pair(t, fakeServer(t))
	cache := c.state + "/" + teamName + "/roster.json"
	eventually(t, func() bool {
		_, err := os.Stat(cache)
		return err == nil
	}, cs.describe)
	kill(t, cs.keeper.Pid(), syscall.SIGTERM)
	cs.wait()
	check(t, k.hs.fake.Stop())
	again := c.seated()
	eventually(t, func() bool { return again.down(teamName) }, again.describe)
	const text = "said by a link that never reached its server"
	again.queued(new(text), "say", "--to", kShort, "--team", teamName)
	check(t, k.hs.fake.Start())
	ks.recvUntil(again.postedOnce(text))
}
