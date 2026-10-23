package teamsync

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The bounded memories (the cache, the own sends, the unknown set, UH1's counted ids) hold 1024
// entries and evict the oldest; a read makes an entry the newest.
func TestBoundedEvictsTheOldest(t *testing.T) {
	b := newBounded[int](3)
	b.put("a", 1)
	b.put("b", 2)
	b.put("c", 3)
	if v, ok := b.get("a"); !ok || v != 1 {
		t.Fatalf("get a = %d %v", v, ok)
	}
	b.put("d", 4)
	if b.has("b") {
		t.Error("b, the least recently used, is still held")
	}
	for _, k := range []string{"a", "c", "d"} {
		if !b.has(k) {
			t.Errorf("%s was evicted", k)
		}
	}
	b.put("a", 9)
	if v, ok := b.get("a"); !ok || v != 9 || b.len() != 3 {
		t.Errorf("a = %d %v, len %d", v, ok, b.len())
	}
	if newBounded[struct{}](memorySize).capacity != 1024 {
		t.Error("the memories do not hold 1024")
	}
}

// fetcher is GetEvent scripted per id.
type fetcher struct {
	answers map[string][]fetched
	calls   map[string]int
}

type fetched struct {
	raw string
	err error
}

func (f *fetcher) get(_ context.Context, id string) (json.RawMessage, error) {
	f.calls[id]++
	a := f.answers[id]
	if len(a) == 0 {
		return nil, errors.New("no answer scripted")
	}
	next := a[0]
	if len(a) > 1 {
		f.answers[id] = a[1:]
	}
	return json.RawMessage(next.raw), next.err
}

func newFetcher() *fetcher { return &fetcher{answers: map[string][]fetched{}, calls: map[string]int{}} }

const (
	evRel1 = "$" + "relatedeventnumberone_aaaaaaaaaaaaaaaaaaaaa"
	evRel2 = "$" + "relatedeventnumbertwo_aaaaaaaaaaaaaaaaaaaaa"
	evRel3 = "$" + "relatedeventnumberthree_aaaaaaaaaaaaaaaaaaa"
	evMsg  = "$" + "theeventthatrefersaway_aaaaaaaaaaaaaaaaaaaa"
)

func rawEvent(t *testing.T, sender string, content map[string]any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"type": proto.MessageType, "sender": sender, "room_id": "!main", "event_id": evRel1,
		"origin_server_ts": 1, "content": content,
	})
	check(t, err)
	return string(data)
}

func newTestResolver(log *syncBuffer) *resolver {
	return newResolver(newLogger(log, alphaID))
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

var (
	errTransient = matrix.StatusError(http.MethodGet, "/event", http.StatusBadGateway, "")
	errMissing   = matrix.StatusError(http.MethodGet, "/event", http.StatusNotFound, "M_NOT_FOUND")
	errForbid    = matrix.StatusError(http.MethodGet, "/event", http.StatusForbidden, "M_FORBIDDEN")
)

// SY4a: the order is the engine's view, the own sends, the cache, the unknown set, and only then
// one fetch; a fetched event is cached, so a second event with the same relation costs nothing.
func TestResolveOrder(t *testing.T) {
	r := newTestResolver(&syncBuffer{})
	f := newFetcher()
	room := "!main"
	f.answers[evRel3] = []fetched{{raw: rawEvent(t, user(alice), human("hi", user(peer)))}}
	r.recordOwnSend(evRel2, proto.Related{Sender: user(me), Verb: "review"})
	view := team.View{Events: map[string]proto.Related{evRel1: {Sender: user(coord)}}}
	in := resolveInput{self: user(me), room: room, roster: baseRoster(), view: view, get: f.get}
	rels := []relation{{event: evMsg, id: evRel1}, {event: evMsg, id: evRel2}, {event: evMsg, id: evRel3}}
	got, err := r.resolve(t.Context(), in, rels)
	check(t, err)
	if _, ok := got[evRel1]; ok {
		t.Error("a related event the view holds is resolved again")
	}
	if got[evRel2].Verb != "review" || got[evRel2].Sender != user(me) {
		t.Errorf("own send = %+v", got[evRel2])
	}
	if rel := got[evRel3]; rel.Sender != user(alice) || !rel.AddressedToRosterAgent || rel.AddressedToSelf || rel.RoomID != room {
		t.Errorf("fetched = %+v", rel)
	}
	if f.calls[evRel1]+f.calls[evRel2] != 0 || f.calls[evRel3] != 1 {
		t.Errorf("fetches = %v", f.calls)
	}
	if _, err := r.resolve(t.Context(), in, rels[2:]); err != nil || f.calls[evRel3] != 1 {
		t.Errorf("a cached event was fetched again: %v %v", err, f.calls)
	}
}

// A5: the resolver holds no lock across a fetch, so an own send recorded meanwhile (a say that
// has sent, the ends' back) does not wait on a slow homeserver.
func TestAFetchHoldsNoLock(t *testing.T) {
	r := newTestResolver(&syncBuffer{})
	fetching, answer := make(chan struct{}), make(chan struct{})
	get := func(context.Context, string) (json.RawMessage, error) {
		close(fetching)
		<-answer
		return json.RawMessage(rawEvent(t, user(alice), human("hi"))), nil
	}
	in := resolveInput{self: user(me), room: "!main", roster: baseRoster(), get: get}
	done := make(chan error, 1)
	go func() {
		_, err := r.resolve(context.Background(), in, []relation{{event: evMsg, id: evRel1}})
		done <- err
	}()
	<-fetching
	r.recordOwnSend(evRel2, proto.Related{Sender: user(me)})
	close(answer)
	check(t, <-done)
	got, err := r.resolve(t.Context(), in, []relation{{event: evMsg, id: evRel1}, {event: evMsg, id: evRel2}})
	check(t, err)
	if got[evRel1].Sender != user(alice) || got[evRel2].Sender != user(me) {
		t.Fatalf("resolved %+v", got)
	}
}

// SY4a: a 404 or 403 is an unknown sender for the link's life, fetched once; a transient failure
// holds the answer, and after 5 in a row the relation is unknown with one log line (n3c-D9).
func TestResolveFailures(t *testing.T) {
	log := &syncBuffer{}
	r := newTestResolver(log)
	f := newFetcher()
	f.answers[evRel1] = []fetched{{err: errMissing}}
	f.answers[evRel2] = []fetched{{err: errForbid}}
	f.answers[evRel3] = []fetched{{err: errTransient}}
	in := resolveInput{self: user(me), room: "!main", roster: baseRoster(), get: f.get}
	gone := []relation{{event: evMsg, id: evRel1}, {event: evMsg, id: evRel2}}
	for range 2 {
		got, err := r.resolve(t.Context(), in, gone)
		if err != nil || len(got) != 0 {
			t.Fatalf("resolve = %v, %v", got, err)
		}
	}
	if f.calls[evRel1] != 1 || f.calls[evRel2] != 1 {
		t.Errorf("an unknown event was fetched again: %v", f.calls)
	}
	poison := []relation{{event: evMsg, id: evRel3}}
	for i := 1; i < poisonTries; i++ {
		if _, err := r.resolve(t.Context(), in, poison); !errors.Is(err, errTransient) {
			t.Fatalf("try %d: %v, want the transient failure", i, err)
		}
	}
	got, err := r.resolve(t.Context(), in, poison)
	if err != nil || len(got) != 0 {
		t.Fatalf("the fifth failure: %v %v", got, err)
	}
	if n := log.count("unresolved after 5 tries"); n != 1 || !containsAll(log.String(), evMsg, evRel3) {
		t.Errorf("log = %q", log.String())
	}
	if _, err := r.resolve(t.Context(), in, poison); err != nil || f.calls[evRel3] != poisonTries {
		t.Errorf("a poisoned relation was fetched again: %v %v", err, f.calls)
	}
}

// n3c-D9: a success in between starts the count again.
func TestResolvePoisonCountsInARow(t *testing.T) {
	r := newTestResolver(&syncBuffer{})
	f := newFetcher()
	ok := rawEvent(t, user(alice), human("x"))
	f.answers[evRel1] = []fetched{{err: errTransient}, {err: errTransient}, {err: errTransient}, {err: errTransient}, {raw: ok}}
	in := resolveInput{self: user(me), room: "!main", roster: baseRoster(), get: f.get}
	for range poisonTries - 1 {
		if _, err := r.resolve(t.Context(), in, []relation{{event: evMsg, id: evRel1}}); err == nil {
			t.Fatal("a transient failure resolved")
		}
	}
	got, err := r.resolve(t.Context(), in, []relation{{event: evMsg, id: evRel1}})
	if err != nil || got[evRel1].Sender != user(alice) {
		t.Fatalf("resolve = %v %v", got, err)
	}
	if len(r.fails) != 0 {
		t.Errorf("fails = %v after a success", r.fails)
	}
}

// SY4a's rule for a fetched event: its own mentions, `@room` and relations answered from memory.
func TestRelatedOf(t *testing.T) {
	ro := baseRoster()
	known := map[string]proto.Related{evRel1: {Sender: user(me)}, evRel2: {Sender: user(peer)}}
	cases := []struct {
		name          string
		content       map[string]any
		self, toAgent bool
	}{
		{"a human mentioning me", human("x", user(me)), true, true},
		{"a human mentioning a deaf worker", human("x", user(deaf)), false, false},
		{"a human's @room", map[string]any{"msgtype": "m.text", "body": "x", "m.mentions": map[string]any{"room": true}}, true, true},
		{"a human replying to my event", reply("x", evRel1), true, true},
		{"a human in peer's thread", inThread("x", evRel2), false, true},
		{"a human's emote", map[string]any{"msgtype": "m.emote", "body": "x"}, false, false},
		{"not a message", nil, false, false},
	}
	for _, c := range cases {
		typ := proto.MessageType
		if c.content == nil {
			typ = proto.MemberType
		}
		data, err := json.Marshal(map[string]any{"type": typ, "sender": user(alice), "event_id": evRel3, "origin_server_ts": 1, "content": c.content})
		check(t, err)
		rel := relatedOf(data, "!main", ro, user(me), func(id string) (proto.Related, bool) {
			r, ok := known[id]
			return r, ok
		})
		if rel.Sender != user(alice) || rel.RoomID != "!main" || rel.AddressedToSelf != c.self || rel.AddressedToRosterAgent != c.toAgent || rel.Verb != "" {
			t.Errorf("%s: %+v", c.name, rel)
		}
	}
	if rel := relatedOf(json.RawMessage(`[]`), "!main", ro, user(me), nil); rel != (proto.Related{}) {
		t.Errorf("an unreadable event = %+v", rel)
	}
}

// An agent's event is addressed by its `to`, and a ping carries its verb.
func TestRelatedOfAnAgentsEvent(t *testing.T) {
	ro := baseRoster()
	out, err := proto.Composer{Server: server, Self: user(coord), Roster: ro}.Ping(proto.Ping{Verb: "halt", To: []string{user(me)}}, "", "halt")
	check(t, err)
	data, err := json.Marshal(map[string]any{"type": proto.MessageType, "sender": user(coord), "event_id": evRel3, "origin_server_ts": 1, "content": json.RawMessage(out.Content.Compact())})
	check(t, err)
	rel := relatedOf(data, "!main", ro, user(me), nil)
	if rel.Verb != "halt" || !rel.AddressedToSelf || !rel.AddressedToRosterAgent || rel.Sender != user(coord) {
		t.Errorf("ping = %+v", rel)
	}
}

// The memory-only Lookup: the view, then what was resolved; an id of the answer itself is seen
// once judged.
func TestMemLookup(t *testing.T) {
	l := &memLookup{
		view: team.View{
			SeenIDs:  map[string]bool{evRel1: true},
			Events:   map[string]proto.Related{evRel2: {Sender: "x"}},
			OwnPings: map[string]proto.OwnPing{evRel3: {Verb: "review"}},
		},
		resolved: map[string]proto.Related{evRel3: {Sender: "y"}},
		judged:   map[string]bool{},
	}
	if !l.Seen(evRel1) || l.Seen(evMsg) {
		t.Error("Seen is wrong")
	}
	l.judged[evMsg] = true
	if !l.Seen(evMsg) {
		t.Error("a judged id is not seen")
	}
	if r, ok := l.Event(evRel2); !ok || r.Sender != "x" {
		t.Error("the view's event is not found")
	}
	if r, ok := l.Event(evRel3); !ok || r.Sender != "y" {
		t.Error("a resolved event is not found")
	}
	if _, ok := l.Event(evMsg); ok {
		t.Error("an unknown event is found")
	}
	if p, ok := l.OwnPing(evRel3); !ok || p.Verb != "review" {
		t.Error("the own ping is not found")
	}
}

// A refusal that is neither transient nor a 404 or 403 (a 401) is the error at once, and counts
// toward no poison bound; nothing is logged without a log.
func TestResolveRefusalIsTheError(t *testing.T) {
	r := newResolver(newLogger(nil, alphaID))
	f := newFetcher()
	refused := matrix.StatusError(http.MethodGet, "/event", http.StatusUnauthorized, "M_UNKNOWN_TOKEN")
	f.answers[evRel1] = []fetched{{err: refused}}
	in := resolveInput{self: user(me), room: "!main", roster: baseRoster(), get: f.get}
	rels := []relation{{event: evMsg, id: evRel1}, {event: evMsg, id: evRel1}}
	if _, err := r.resolve(t.Context(), in, rels); !errors.Is(err, matrix.ErrAuthRefused) {
		t.Fatalf("resolve = %v", err)
	}
	if len(r.fails) != 0 || r.unknown.has(evRel1) {
		t.Errorf("a refusal was counted: %v", r.fails)
	}
}

// A fetched event's relations are answered from the view and from what this resolve already
// answered, and a relation asked twice is resolved once.
func TestResolveFetchedRelationsFromMemory(t *testing.T) {
	r := newTestResolver(&syncBuffer{})
	f := newFetcher()
	f.answers[evRel2] = []fetched{{raw: rawEvent(t, user(alice), reply("x", evRel1))}}
	f.answers[evRel3] = []fetched{{raw: rawEvent(t, user(alice), reply("y", evMsg))}}
	r.recordOwnSend(evMsg, proto.Related{Sender: user(me)})
	view := team.View{Events: map[string]proto.Related{evRel1: {Sender: user(me)}}}
	in := resolveInput{self: user(me), room: "!main", roster: baseRoster(), view: view, get: f.get}
	got, err := r.resolve(t.Context(), in, []relation{{event: evMsg, id: evRel2}, {event: evMsg, id: evRel3}, {event: evMsg, id: evRel2}})
	check(t, err)
	if !got[evRel2].AddressedToSelf || !got[evRel3].AddressedToSelf || f.calls[evRel2] != 1 {
		t.Errorf("got %+v, calls %v", got, f.calls)
	}
}

// An agent's text and notice are addressed by their `to`; an agent event that does not decode,
// and one from no member, carry no addressing.
func TestRelatedOfAgentTextAndNotice(t *testing.T) {
	ro := baseRoster()
	out, err := proto.Composer{Server: server, Self: user(coord), Roster: ro}.Text(proto.Text{To: []string{user(peer)}, Text: "hi"}, "", "hi")
	check(t, err)
	notice, err := proto.Composer{Server: server, Self: user(peer), Roster: ro}.Notice(proto.Notice{Event: proto.EventLeft, Subject: user(peer), Reason: "exit", To: []string{user(alice), user(coord)}}, "left")
	check(t, err)
	cases := []struct {
		name          string
		sender        string
		content       json.RawMessage
		self, toAgent bool
	}{
		{"text to peer", user(coord), out.Content.Compact(), false, true},
		{"a left notice to the coordinator", user(peer), notice.Content.Compact(), false, true},
		{"an agent's undecodable event", user(peer), json.RawMessage(`{"msgtype":"m.text","body":"x"}`), false, false},
		{"no member", "@stranger:" + server, out.Content.Compact(), false, false},
	}
	for _, c := range cases {
		data, err := json.Marshal(map[string]any{"type": proto.MessageType, "sender": c.sender, "event_id": evRel3, "origin_server_ts": 1, "content": c.content})
		check(t, err)
		rel := relatedOf(data, "!main", ro, user(me), nil)
		if rel.AddressedToSelf != c.self || rel.AddressedToRosterAgent != c.toAgent || rel.Sender != c.sender {
			t.Errorf("%s: %+v", c.name, rel)
		}
	}
	if rel := relatedOf(json.RawMessage(`{"type":"m.room.message"}`), "!main", ro, user(me), nil); rel != (proto.Related{}) {
		t.Errorf("an event with no sender = %+v", rel)
	}
}
