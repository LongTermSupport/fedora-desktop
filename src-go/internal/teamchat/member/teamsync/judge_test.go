package teamsync

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// held runs tm with its first long-poll held until the test lets it go, so what the test posts
// in between comes as one answer. The first answer (SY1) is taken.
func (w *world) held(tm *Team) (*running, func()) {
	w.t.Helper()
	release := make(chan struct{})
	calls := 0
	tm.onPoll = func(string) {
		if calls++; calls == 1 {
			select {
			case <-release:
			case <-w.t.Context().Done():
			}
		}
	}
	r := w.startTeam(tm)
	w.batch()
	return r, func() {
		close(release)
		r.poll(w.t)
	}
}

// post sends local's composed event to main.
func (w *world) post(local string, out proto.Outgoing, err error) string {
	w.t.Helper()
	check(w.t, err)
	id, err := w.client(local).Send(w.t.Context(), w.main, proto.MessageType, "", out.Content.Compact())
	check(w.t, err)
	return id
}

func (w *world) composer(local string) proto.Composer {
	return proto.Composer{Server: server, Self: user(local), Token: w.tokens[local], Roster: w.roster}
}

// UH1: a human's message with no text, one addressed to no one and one to an agent that reads
// no human text are unheard, each once with its id and reason and never its body; one to an
// agent, a reply to an agent's post and an agent's post to another are not.
func TestUnheardHumanMessages(t *testing.T) {
	w := newWorld(t)
	r, release := w.held(w.newTeam())
	toCoord := w.agentText(peer, "for the coordinator", user(coord))
	w.say(alice, map[string]any{"msgtype": "m.emote", "body": "secret-emote"})
	w.say(alice, human("secret-nobody"))
	w.say(alice, human("secret-deaf", user(deaf)))
	w.say(alice, human("to peer", user(peer)))
	w.say(alice, reply("answering peer", toCoord))
	release()
	b := w.batch()
	if b.Counts.Unheard != 3 || b.Counts.Ignored != 6 || len(b.Items) != 0 {
		t.Errorf("counts %+v, items %d", b.Counts, len(b.Items))
	}
	r.poll(t)
	l := w.log.String()
	if n := strings.Count(l, "unheard "); n != 3 || strings.Contains(l, "secret") {
		t.Errorf("log %q", l)
	}
}

// UH1 (n3c-D10): an unheard id is counted once, though the answer that held it was judged twice
// because its first store write failed.
func TestAnUnheardIDIsCountedOnce(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	r, release := w.held(tm)
	failed := false
	w.eng.setPersistErr(func(team.Batch) error {
		if !failed {
			failed = true
			return errors.New("disk full")
		}
		return nil
	})
	w.say(alice, human("to nobody"))
	release()
	w.clk.Advance(r.wait(t))
	if b := w.batch(); b.Counts.Unheard != 1 {
		t.Errorf("counts %+v", b.Counts)
	}
	// The resumed session's long-poll, then the one after the commit.
	r.poll(t)
	r.poll(t)
	if n := w.log.count("unheard "); n != 1 {
		t.Errorf("said %d times", n)
	}
}

// SY4: an agent's event that does not read as teamchat is dropped, with one line; an admin's
// redaction of a held item is a redact.
func TestDropsAndRedacts(t *testing.T) {
	w := newWorld(t)
	r := w.start()
	w.batch()
	r.poll(t)
	w.say(peer, human("plain text from an agent", user(me)))
	if b := w.batch(); b.Counts.Dropped != 1 || len(b.Items) != 0 {
		t.Errorf("batch %+v", b)
	}
	r.poll(t)
	if n := w.log.count("dropped "); n != 1 {
		t.Errorf("log %q", w.log.String())
	}
	id := w.say(alice, human("to be redacted", user(me)))
	w.batch()
	if _, err := w.admin().Redact(t.Context(), w.main, id, "", ""); err != nil {
		t.Fatal(err)
	}
	if b := w.batch(); len(b.Redacts) != 1 || b.Redacts[0] != id || b.Counts.Redacted != 1 {
		t.Errorf("batch %+v", b)
	}
}

// SY4: an agent's text and ping to me are a peer and a ping item.
func TestDeliversAgentItems(t *testing.T) {
	w := newWorld(t)
	_, release := w.held(w.newTeam())
	text := w.agentText(peer, "to me", user(me))
	out, err := w.composer(coord).Ping(proto.Ping{Verb: proto.VerbHalt, To: []string{user(me)}}, "", "")
	ping := w.post(coord, out, err)
	release()
	b := w.batch()
	if len(b.Items) != 2 {
		t.Fatalf("items %+v", b.Items)
	}
	if p := b.Items[0].Pending; p.ID != text || p.Item != records.KindPeer || p.From != user(peer) {
		t.Errorf("peer %+v", p)
	}
	if p := b.Items[1].Pending; p.ID != ping || p.Item != records.KindPing {
		t.Errorf("ping %+v", p)
	}
}

func TestItemKind(t *testing.T) {
	for k, want := range map[string]string{
		proto.KindHuman: records.KindHuman, proto.KindPeer: records.KindPeer, proto.KindPing: records.KindPing,
		proto.KindTimeout: records.KindTimeout, proto.KindNotice: records.KindNotice,
	} {
		if got := itemKind(k); got != want {
			t.Errorf("itemKind(%s) = %s", k, got)
		}
	}
}

// fetches counts the related-event fetches fakehs received.
func (w *world) fetches() int { return w.requests("/event/") }

// SY4a: a thread reply under my own post is addressed to me: the root is fetched once, and a
// second reply under it is answered from the cache.
func TestAThreadUnderMyPostIsMine(t *testing.T) {
	w := newWorld(t)
	root := w.agentText(me, "my say", user(coord))
	_, release := w.held(w.newTeam())
	first := w.say(alice, inThread("under your say", root))
	second := w.say(alice, inThread("and again", root))
	release()
	b := w.batch()
	if got := itemIDs(b); len(got) != 2 || got[0] != first || got[1] != second {
		t.Errorf("items %v", got)
	}
	if n := w.fetches(); n != 1 {
		t.Errorf("%d fetches", n)
	}
}

// SY4a (n3c-D11): an own send recorded through Notices is known without a fetch.
func TestAnOwnSendNeedsNoFetch(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	root := w.agentText(me, "my say", user(coord))
	tm.Notices().RecordOwnSend(root, proto.Related{Sender: user(me), RoomID: w.main})
	_, release := w.held(tm)
	id := w.say(alice, reply("to your say", root))
	release()
	if got := itemIDs(w.batch()); len(got) != 1 || got[0] != id {
		t.Errorf("items %v", got)
	}
	if n := w.fetches(); n != 0 {
		t.Errorf("%d fetches", n)
	}
}

// SY4a: a transient fault fetching a related event holds the whole answer: nothing is judged
// or persisted until the retry.
func TestATransientFetchHoldsTheAnswer(t *testing.T) {
	w := newWorld(t)
	root := w.agentText(peer, "peer's say", user(coord))
	tm := w.newTeam()
	var judged atomic.Int32
	tm.judge = func(j proto.Judge, v proto.Value) proto.Verdict {
		judged.Add(1)
		return j.Judge(v)
	}
	r, release := w.held(tm)
	before := judged.Load()
	w.srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: "/event/", Status: http.StatusBadGateway, Body: []byte(`{}`)})
	id := w.say(alice, reply("to peer, and me", root, user(me)))
	release()
	d := r.wait(t)
	if judged.Load() != before {
		t.Error("judged before the relation resolved")
	}
	w.noBatch()
	w.clk.Advance(d)
	if got := itemIDs(w.batch()); len(got) != 1 || got[0] != id {
		t.Errorf("items %v", got)
	}
}

// SY4a: a related event the server answers 404 for is unknown for the link's life: one fetch.
func TestAMissingRelationIsFetchedOnce(t *testing.T) {
	w := newWorld(t)
	_, release := w.held(w.newTeam())
	missing := "$" + "nosucheventanywhere_aaaaaaaaaaaaaaaaaaaaaaa"
	one := w.say(alice, reply("one", missing, user(me)))
	two := w.say(alice, reply("two", missing, user(me)))
	release()
	if got := itemIDs(w.batch()); len(got) != 2 || got[0] != one || got[1] != two {
		t.Errorf("items %v", got)
	}
	if n := w.fetches(); n != 1 {
		t.Errorf("%d fetches", n)
	}
}
