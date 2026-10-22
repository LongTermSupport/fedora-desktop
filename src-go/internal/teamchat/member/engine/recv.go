package engine

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// LN4's caps: a recv or wait hands out at most 8 items and 12 KiB of record lines (each line's
// bytes and its newline), then a more record; the first item always goes, whatever its size,
// so an item over the cap is never stuck.
const (
	maxEntries = 8
	maxBytes   = 12 << 10
)

// refetchWait bounds a restored item's re-fetch: a team whose homeserver does not answer in
// time keeps its items pending for the next recv, and every other team's items go now.
const refetchWait = 2 * time.Second

// waitRetry is how soon a wait looks again when every pending item it found could not be
// rendered yet (a restored item whose team did not answer its re-fetch).
const waitRetry = time.Second

// candidate is one pending item a recv may hand out.
type candidate struct {
	team  paths.TeamID
	id    string
	room  string
	sent  int64
	fetch bool
}

// plan is a recv's first, local look: the items not handed, oldest first, and the teams they
// are of.
type plan struct {
	cands []candidate
	teams map[paths.TeamID]team.Team
}

type fetched struct {
	v   proto.Verdict
	err error
}

type key struct {
	team paths.TeamID
	id   string
}

// handout is what a recv's hand-out gave: the records, in order, and what is still pending.
type handout struct {
	recs      []records.Record
	remaining int64
}

// eligible is whether h's items can be handed out: held, not removed, with a store.
func eligible(h *held, only *paths.TeamID) bool {
	return !h.removed && !h.refused && h.store != nil && (only == nil || *only == h.id)
}

func (e *Engine) plan(only *paths.TeamID) plan {
	p := plan{teams: map[paths.TeamID]team.Team{}}
	for _, id := range e.order() {
		h := e.teams[id]
		if !eligible(h, only) {
			continue
		}
		p.teams[id] = h.team
		for _, it := range h.store.Pending {
			if it.Handed != nil {
				continue
			}
			_, known := h.content[it.ID]
			p.cands = append(p.cands, candidate{team: id, id: it.ID, room: it.Room, sent: it.SentMS, fetch: needsContent(it) && !known})
		}
	}
	slices.SortStableFunc(p.cands, func(a, b candidate) int { return cmp.Compare(a.sent, b.sent) })
	return p
}

// refetch re-fetches the restored items among the first maxEntries candidates (B 2.7.3
// "Content"), each team on its own goroutine, its items in order until one fails; bounded by
// refetchWait, so one team that does not answer holds no other team's items.
func (e *Engine) refetch(ctx context.Context, p plan) map[key]fetched {
	byTeam := map[paths.TeamID][]candidate{}
	for _, c := range p.cands[:min(len(p.cands), maxEntries)] {
		if c.fetch && p.teams[c.team] != nil {
			byTeam[c.team] = append(byTeam[c.team], c)
		}
	}
	out := map[key]fetched{}
	if len(byTeam) == 0 {
		return out
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := e.cfg.Clock.AfterFunc(refetchWait, cancel)
	defer stop()
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for id, cands := range byTeam {
		tm := p.teams[id]
		wg.Go(func() {
			for _, c := range cands {
				v, err := tm.Refetch(ctx, c.room, c.id)
				mu.Lock()
				out[key{id, c.id}] = fetched{v: v, err: err}
				mu.Unlock()
				if err != nil {
					e.logf("team %s: restored item %s not re-fetched: %v", id, c.id, err)
					return
				}
			}
		})
	}
	wg.Wait()
	return out
}

// rosters are the teams' rosters, read off the state goroutine.
func rosters(teams map[paths.TeamID]team.Team) map[paths.TeamID]namer {
	out := map[paths.TeamID]namer{}
	for id, tm := range teams {
		n := namer{server: paths.ServerName(id)}
		if tm != nil {
			n.roster, n.verified = tm.Notices().Roster()
		}
		out[id] = n
	}
	return out
}

// handOut is the recv's hand-out, on the state goroutine: re-fetched items judged again (one
// that no longer passes, or is not newer than its room's floor, is dropped, B44), then the
// items chosen within the caps marked handed by ack, one write per store, before any record is
// sent.
func (e *Engine) handOut(p plan, got map[key]fetched, names map[paths.TeamID]namer, ack string) handout {
	stores := map[paths.TeamID]*state.Store{}
	storeOf := func(h *held) *state.Store {
		if s, ok := stores[h.id]; ok {
			return s
		}
		s, err := clone(*h.store)
		if err != nil {
			e.logf("team %s: %v", h.id, err)
			return nil
		}
		stores[h.id] = &s
		return &s
	}
	for k, f := range got {
		h := e.teams[k.team]
		if f.err != nil || h == nil || !eligible(h, nil) {
			continue
		}
		if why := e.rejudge(h, k.id, f.v); why != "" {
			if s := storeOf(h); s != nil {
				s.Pending = slices.DeleteFunc(s.Pending, func(it state.Pending) bool { return it.ID == k.id })
				e.logf("team %s: restored item %s dropped: %s", h.id, k.id, why)
			}
			continue
		}
		h.content[k.id] = f.v
	}
	var (
		chosen []key
		recs   []records.Record
		total  int
	)
	for _, c := range p.cands {
		h := e.teams[c.team]
		if h == nil || !eligible(h, nil) || len(chosen) == maxEntries {
			continue
		}
		s := storeOf(h)
		if s == nil {
			continue
		}
		i := slices.IndexFunc(s.Pending, func(it state.Pending) bool { return it.ID == c.id && it.Handed == nil })
		if i < 0 {
			continue
		}
		v, known := h.content[c.id]
		if needsContent(s.Pending[i]) && !known {
			continue
		}
		r := itemRecord(c.team, s.Pending[i], v, names[c.team], s.Outbox.Pings)
		line, err := records.Encode(r)
		if err != nil {
			e.logf("team %s: item %s has no record: %v", c.team, c.id, err)
			continue
		}
		if len(chosen) > 0 && total+len(line)+1 > maxBytes {
			break
		}
		total += len(line) + 1
		handed := ack
		s.Pending[i].Handed = &handed
		chosen = append(chosen, key{c.team, c.id})
		recs = append(recs, r)
	}
	failed := map[paths.TeamID]bool{}
	for id, s := range stores {
		h := e.teams[id]
		if err := h.writer.Write(*s); err != nil {
			e.logf("team %s: the hand-out was not persisted, its items stay pending: %v", id, err)
			failed[id] = true
			continue
		}
		h.store = s
	}
	out := handout{}
	for i, k := range chosen {
		if !failed[k.team] {
			out.recs = append(out.recs, recs[i])
		}
	}
	for id := range p.teams {
		if h := e.teams[id]; h != nil && eligible(h, nil) {
			for _, it := range h.store.Pending {
				if it.Handed == nil {
					out.remaining++
				}
			}
		}
	}
	e.publish()
	return out
}

// rejudge is why a re-fetched item no longer passes: its verdict, or its room's floor; "" when
// it passes.
func (e *Engine) rejudge(h *held, id string, v proto.Verdict) string {
	if v.Outcome != proto.Deliver {
		return "it no longer passes: " + v.Reason
	}
	i := slices.IndexFunc(h.store.Pending, func(it state.Pending) bool { return it.ID == id })
	if i < 0 {
		return ""
	}
	if floor, ok := h.store.Sync.FloorMS[h.store.Pending[i].Room]; ok && v.Event.OriginTs <= floor {
		return "it is not newer than its room's floor"
	}
	return ""
}

// recv is LN4's recv: items handed and persisted, then their records, then more when items are
// still pending; the answer's ack is what Ack delivers them by.
func (e *Engine) recv(ctx context.Context, only *paths.TeamID, out RecordWriter) wire.Done {
	ack, err := wire.NewAck()
	if err != nil {
		return internal(err)
	}
	var p plan
	if err := e.do(ctx, func() { p = e.plan(only) }); err != nil {
		return internal(err)
	}
	if len(p.cands) == 0 {
		return wire.Done{}
	}
	names := rosters(p.teams)
	got := e.refetch(ctx, p)
	var h handout
	if err := e.do(ctx, func() { h = e.handOut(p, got, names, ack.Ack) }); err != nil {
		return internal(err)
	}
	if len(h.recs) == 0 {
		return wire.Done{}
	}
	failpoint.Hit(failpoint.LinkAfterHandOut)
	if err := e.write(out, h); err != nil {
		return internal(errors.Join(err, e.Return(context.WithoutCancel(ctx), ack.Ack)))
	}
	return wire.Done{Ack: ack.Ack}
}

func (e *Engine) write(out RecordWriter, h handout) error {
	if err := writeAll(out, h.recs); err != nil {
		return err
	}
	if h.remaining > 0 {
		if err := out.Record(records.More{Pending: h.remaining}); err != nil {
			return err
		}
	}
	return out.Flush()
}

// wait is LN4's wait: it sleeps until something is pending, then acts as recv; at its
// deadline with nothing handed it ends with no records (n3c-D7).
func (e *Engine) wait(ctx context.Context, only *paths.TeamID, out RecordWriter) wire.Done {
	for {
		var (
			woken   chan struct{}
			pending bool
		)
		if err := e.do(ctx, func() {
			pending = e.hasPending(only)
			woken = make(chan struct{})
			e.waiters = append(e.waiters, woken)
		}); err != nil {
			return done(ctx, err)
		}
		retry, release := (<-chan time.Time)(nil), func() bool { return false }
		if pending {
			if d := e.recv(ctx, only, out); d.Ack != "" || d.Exit != 0 {
				return d
			}
			retry, release = e.cfg.Clock.Hold(waitRetry)
		}
		select {
		case <-woken:
		case <-retry:
		case <-ctx.Done():
			release()
			return wire.Done{}
		}
		release()
	}
}

// done is a wait's answer to err: none at its own deadline, else internal.
func done(ctx context.Context, err error) wire.Done {
	if ctx.Err() != nil {
		return wire.Done{}
	}
	return internal(err)
}

func (e *Engine) hasPending(only *paths.TeamID) bool {
	for _, h := range e.teams {
		if !eligible(h, only) {
			continue
		}
		if slices.ContainsFunc(h.store.Pending, func(it state.Pending) bool { return it.Handed == nil }) {
			return true
		}
	}
	return false
}

// inbox lists every item owed, handed ones included, without bodies; it consumes nothing.
func (e *Engine) inbox(ctx context.Context, only *paths.TeamID, out RecordWriter) wire.Done {
	var p plan
	if err := e.do(ctx, func() { p = e.plan(only) }); err != nil {
		return internal(err)
	}
	names := rosters(p.teams)
	var recs []records.Record
	if err := e.do(ctx, func() {
		for _, id := range e.order() {
			h := e.teams[id]
			if _, ok := p.teams[id]; !ok || !eligible(h, nil) {
				continue
			}
			for _, it := range h.store.Pending {
				recs = append(recs, records.Pending{Team: id, ID: it.ID, From: names[id].name(it.From), Item: it.Item, SentMS: it.SentMS})
			}
		}
	}); err != nil {
		return internal(err)
	}
	if err := writeAll(out, recs); err != nil {
		return internal(err)
	}
	if err := out.Flush(); err != nil {
		return internal(err)
	}
	return wire.Done{}
}

// Ack delivers the items a recv handed by ack (B 2.4.4): out of pending and into the ledger,
// one write per store. An ack for a removed team is ignored and recreates nothing (T2).
func (e *Engine) Ack(ctx context.Context, ack string) error {
	return e.settle(ctx, ack, func(s *state.Store, it state.Pending, now int64) bool {
		s.AddLedger(state.Ledger{ID: it.ID, Room: it.Room, Item: it.Item, From: it.From, AtMS: now})
		return true
	})
}

// Return gives the items a recv handed by ack back to pending, marked repeat (no ack in 10 s,
// a closed connection, a failed write), one write per store.
func (e *Engine) Return(ctx context.Context, ack string) error {
	return e.settle(ctx, ack, func(_ *state.Store, _ state.Pending, _ int64) bool { return false })
}

// settle applies an ack's end to every held team's items handed by it: deliver says whether
// an item leaves pending (into the ledger) or stays, marked repeat.
func (e *Engine) settle(ctx context.Context, ack string, deliver func(*state.Store, state.Pending, int64) bool) error {
	var errs []error
	if err := e.do(ctx, func() {
		now := e.cfg.Clock.WallMS()
		for _, id := range e.order() {
			if !eligible(e.teams[id], nil) {
				continue
			}
			errs = append(errs, e.changeLocked(id, func(h *held, s *state.Store) (bool, error) {
				changed := false
				kept := s.Pending[:0]
				for _, it := range s.Pending {
					if it.Handed == nil || *it.Handed != ack {
						kept = append(kept, it)
						continue
					}
					changed = true
					if deliver(s, it, now) {
						delete(h.content, it.ID)
						continue
					}
					it.Handed, it.Repeat = nil, true
					kept = append(kept, it)
				}
				s.Pending = kept
				return changed, nil
			}))
		}
	}); err != nil {
		return err
	}
	return errors.Join(errs...)
}
