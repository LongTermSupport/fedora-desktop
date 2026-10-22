// Package teamsync is one team goroutine of the link (Plan 00209 SPEC-n3 SY1-SY14, SY4a, UH1,
// LN6, LN9): it owns every homeserver request of its team, verifies the main room, syncs it,
// judges each event and hands the engine one batch per answer to persist.
package teamsync

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"slices"
	"sync"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// memorySize bounds every memory of a team goroutine: the related-event cache, the own sends,
// the unknown set and UH1's counted ids (n3-D20, n3c-D10, n3c-D11).
const memorySize = 1024

// poisonTries is how many transient failures in a row a related event may cost before it is
// taken as unknown (n3c-D9).
const poisonTries = 5

// newLogger is the team's link.log writer: one line per call, prefixed with the team.
func newLogger(w io.Writer, t paths.TeamID) *log.Logger {
	if w == nil {
		w = io.Discard
	}
	return log.New(w, "teamsync "+t.String()+": ", 0)
}

// bounded is a map of at most capacity entries; a put or a get makes its entry the newest, and
// a put past the capacity evicts the oldest.
type bounded[V any] struct {
	capacity int
	items    map[string]*node[V]
	// newest and oldest are the ends of the recency list.
	newest, oldest *node[V]
}

type node[V any] struct {
	key          string
	value        V
	newer, older *node[V]
}

func newBounded[V any](capacity int) *bounded[V] {
	return &bounded[V]{capacity: capacity, items: map[string]*node[V]{}}
}

func (b *bounded[V]) put(key string, v V) {
	if n, ok := b.items[key]; ok {
		n.value = v
		b.touch(n)
		return
	}
	n := &node[V]{key: key, value: v}
	b.items[key] = n
	b.front(n)
	if len(b.items) > b.capacity {
		oldest := b.oldest
		b.unlink(oldest)
		delete(b.items, oldest.key)
	}
}

func (b *bounded[V]) get(key string) (V, bool) {
	n, ok := b.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	b.touch(n)
	return n.value, true
}

func (b *bounded[V]) has(key string) bool {
	_, ok := b.items[key]
	return ok
}

func (b *bounded[V]) len() int { return len(b.items) }

func (b *bounded[V]) touch(n *node[V]) {
	b.unlink(n)
	b.front(n)
}

func (b *bounded[V]) front(n *node[V]) {
	n.older, n.newer = b.newest, nil
	if b.newest != nil {
		b.newest.newer = n
	}
	b.newest = n
	if b.oldest == nil {
		b.oldest = n
	}
}

func (b *bounded[V]) unlink(n *node[V]) {
	if n.newer != nil {
		n.newer.older = n.older
	} else {
		b.newest = n.older
	}
	if n.older != nil {
		n.older.newer = n.newer
	} else {
		b.oldest = n.newer
	}
	n.newer, n.older = nil, nil
}

// relation is one related id an event reads for its addressing.
type relation struct{ event, id string }

// resolveInput is what one resolve reads: the member, main, the roster, the engine's view of the
// answer, and the fetch of one event of main.
type resolveInput struct {
	self, room string
	roster     proto.Roster
	view       team.View
	get        func(ctx context.Context, id string) (json.RawMessage, error)
}

// resolver is SY4a's memory of related events beyond the store: the own sends, the cache of
// fetched events and the ids the server will not give. It lives as long as the team goroutine;
// a link restart empties it and the fetch backs it.
type resolver struct {
	log *log.Logger

	mu      sync.Mutex
	own     *bounded[proto.Related]
	cache   *bounded[proto.Related]
	unknown *bounded[struct{}]
	fails   map[string]int
}

func newResolver(l *log.Logger) *resolver {
	return &resolver{
		log: l, own: newBounded[proto.Related](memorySize), cache: newBounded[proto.Related](memorySize),
		unknown: newBounded[struct{}](memorySize), fails: map[string]int{},
	}
}

func (r *resolver) recordOwnSend(id string, rel proto.Related) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.own.put(id, rel)
}

// resolve answers each relation the view does not hold, in SY4a's order: the own sends, the
// cache, the unknown set, then one fetch. A 404 or 403 is unknown for the link's life; a
// transient failure is the error, so the answer is held, until poisonTries in a row make the
// id unknown.
func (r *resolver) resolve(ctx context.Context, in resolveInput, rels []relation) (map[string]proto.Related, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	got := map[string]proto.Related{}
	for _, rel := range rels {
		if _, ok := in.view.Event(rel.id); ok {
			continue
		}
		if _, done := got[rel.id]; done {
			continue
		}
		if v, ok := r.own.get(rel.id); ok {
			got[rel.id] = v
			continue
		}
		if v, ok := r.cache.get(rel.id); ok {
			got[rel.id] = v
			continue
		}
		if r.unknown.has(rel.id) {
			continue
		}
		if err := r.fetch(ctx, in, rel, got); err != nil {
			return nil, err
		}
	}
	return got, nil
}

func (r *resolver) fetch(ctx context.Context, in resolveInput, rel relation, got map[string]proto.Related) error {
	raw, err := in.get(ctx, rel.id)
	if err != nil {
		e, isMatrix := errors.AsType[*matrix.Error](err)
		switch {
		case errors.Is(err, matrix.ErrNotFound) || errors.Is(err, matrix.ErrForbidden):
			delete(r.fails, rel.id)
			r.unknown.put(rel.id, struct{}{})
			return nil
		case !isMatrix || !e.Transient():
			return err
		}
		r.fails[rel.id]++
		if r.fails[rel.id] < poisonTries {
			return err
		}
		delete(r.fails, rel.id)
		r.unknown.put(rel.id, struct{}{})
		r.log.Printf("event %s: its related event %s unresolved after %d tries; taken as unknown", rel.event, rel.id, poisonTries)
		return nil
	}
	delete(r.fails, rel.id)
	v := relatedOf(raw, in.room, in.roster, in.self, func(id string) (proto.Related, bool) {
		if rv, ok := in.view.Event(id); ok {
			return rv, true
		}
		if rv, ok := got[id]; ok {
			return rv, true
		}
		if rv, ok := r.own.get(id); ok {
			return rv, true
		}
		return r.cache.get(id)
	})
	r.cache.put(rel.id, v)
	got[rel.id] = v
	return nil
}

// relatedOf is SY4a's rule for a fetched event: its sender and room, and its addressing from its
// own mentions, `@room` and the relations known answers, never a further fetch. An agent's event
// is addressed by its `to`, and a ping carries its verb. An event that does not read is zero.
func relatedOf(raw json.RawMessage, room string, ro proto.Roster, self string, known func(string) (proto.Related, bool)) proto.Related {
	v, err := proto.Parse(raw)
	if err != nil {
		return proto.Related{}
	}
	sender, typ, content := str(v, "sender"), str(v, "type"), member(v, "content")
	if sender == "" {
		return proto.Related{}
	}
	rel := proto.Related{Sender: sender, RoomID: room}
	if typ != proto.MessageType {
		return rel
	}
	switch _, isMember := ro.Members[sender]; {
	case ro.IsHuman(sender):
		h, reason := proto.DecodeHuman(content)
		if reason != "" {
			return rel
		}
		rel.AddressedToSelf = slices.Contains(h.Mentions, self) || h.MentionRoom ||
			sentBy(known, h.ReplyTo, func(s string) bool { return s == self }) ||
			sentBy(known, h.ThreadRoot, func(s string) bool { return s == self })
		rel.AddressedToRosterAgent = slices.ContainsFunc(h.Mentions, ro.HumanReader) || h.MentionRoom ||
			sentBy(known, h.ReplyTo, ro.HumanReader) || sentBy(known, h.ThreadRoot, ro.HumanReader)
	case isMember:
		c, reason := proto.DecodeContent(content, proto.MainKeys)
		if reason != "" {
			return rel
		}
		to := addressees(c)
		rel.AddressedToSelf = slices.Contains(to, self)
		rel.AddressedToRosterAgent = slices.ContainsFunc(to, ro.AddressableAgent)
		if c.Key == proto.KeyPing {
			rel.Verb = c.Ping.Verb
		}
	}
	return rel
}

func sentBy(known func(string) (proto.Related, bool), id string, by func(string) bool) bool {
	if id == "" || known == nil {
		return false
	}
	r, ok := known(id)
	return ok && by(r.Sender)
}

func addressees(c proto.Content) []string {
	switch c.Key {
	case proto.KeyPing:
		return c.Ping.To
	case proto.KeyText:
		return c.Text.To
	case proto.KeyNotice:
		return c.Notice.To
	}
	return nil
}

func str(v proto.Value, key string) string {
	if m, ok := v.Get(key); ok && m.Kind() == proto.KindString {
		return m.Str()
	}
	return ""
}

func member(v proto.Value, key string) proto.Value {
	m, ok := v.Get(key)
	if !ok {
		return proto.Value{}
	}
	return m
}

// memLookup is the Lookup Judge reads (gap 14): the engine's view, then what the resolver
// answered; an id already judged in this answer is seen, so an event the answer holds twice is
// judged once. skip is an id that is never seen (Refetch's own).
type memLookup struct {
	view     team.View
	resolved map[string]proto.Related
	judged   map[string]bool
	skip     string
}

func (l *memLookup) Seen(id string) bool {
	return id != l.skip && (l.view.Seen(id) || l.judged[id])
}

func (l *memLookup) Event(id string) (proto.Related, bool) {
	if r, ok := l.view.Event(id); ok {
		return r, true
	}
	r, ok := l.resolved[id]
	return r, ok
}

func (l *memLookup) OwnPing(id string) (proto.OwnPing, bool) { return l.view.OwnPing(id) }
