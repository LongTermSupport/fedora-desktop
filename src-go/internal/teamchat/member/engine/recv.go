package engine

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
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
// "Content"), each team on its own goroutine, its items in order until one fails transiently
// (an item that is gone is dropped by rejudgeAll and holds none after it); bounded by
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
				if err != nil && !errors.Is(err, team.ErrItemGone) {
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
	c := copies{e: e, stores: map[paths.TeamID]*state.Store{}, raised: map[paths.TeamID]int64{}}
	e.rejudgeAll(got, &c)
	chosen, recs := e.choose(p, names, ack, &c)
	failed := e.commit(c.stores)
	for id, floor := range c.raised {
		if !failed[id] {
			e.floorRaised(id, floor)
		}
	}
	out := handout{}
	for i, k := range chosen {
		if !failed[k.team] {
			out.recs = append(out.recs, recs[i])
		}
	}
	for id := range p.teams {
		if h := e.teams[id]; h != nil && eligible(h, nil) {
			out.remaining += unhanded(h.store)
		}
	}
	e.publish()
	return out
}

// copies are a hand-out's store copies, one per team touched, written together at its end;
// raised is each copy's main floor where a drop raised it.
type copies struct {
	e      *Engine
	stores map[paths.TeamID]*state.Store
	raised map[paths.TeamID]int64
}

func (c *copies) of(h *held) *state.Store {
	if s, ok := c.stores[h.id]; ok {
		return s
	}
	s, err := clone(*h.store)
	if err != nil {
		c.e.logf("team %s: %v", h.id, err)
		return nil
	}
	c.stores[h.id] = &s
	return &s
}

// rejudgeAll keeps each re-fetched item's verdict in memory, or drops the item from its copy:
// one that no longer passes, and one the server will never serve again (team.ErrItemGone).
func (e *Engine) rejudgeAll(got map[key]fetched, c *copies) {
	for k, f := range got {
		h := e.teams[k.team]
		gone := errors.Is(f.err, team.ErrItemGone)
		if (f.err != nil && !gone) || h == nil || !eligible(h, nil) {
			continue
		}
		var why string
		if gone {
			why = "it can no longer be fetched (" + f.err.Error() + ")"
		} else {
			why = e.rejudge(h, k.id, f.v)
		}
		if why == "" {
			h.content[k.id] = f.v
			continue
		}
		if s := c.of(h); s != nil {
			i := slices.IndexFunc(s.Pending, func(it state.Pending) bool { return it.ID == k.id })
			if i < 0 {
				continue
			}
			left := s.Pending[i]
			s.Pending = slices.Delete(s.Pending, i, i+1)
			if floor, moved := raiseFloors(s, []state.Pending{left}); moved {
				c.raised[h.id] = floor
			}
			e.logf("team %s: restored item %s dropped: %s", h.id, k.id, why)
		}
	}
}

// choose marks the candidates within the caps handed by ack, in their copies, and is their
// keys and records in order.
func (e *Engine) choose(p plan, names map[paths.TeamID]namer, ack string, c *copies) ([]key, []records.Record) {
	var (
		chosen []key
		recs   []records.Record
		total  int
	)
	for _, cand := range p.cands {
		if len(chosen) == maxEntries {
			break
		}
		s, i, r, ok := e.render(cand, names, c)
		if !ok {
			continue
		}
		line, err := records.Encode(r)
		if err != nil {
			e.logf("team %s: item %s has no record: %v", cand.team, cand.id, err)
			continue
		}
		if len(chosen) > 0 && total+len(line)+1 > maxBytes {
			break
		}
		total += len(line) + 1
		handed := ack
		s.Pending[i].Handed = &handed
		chosen = append(chosen, key{cand.team, cand.id})
		recs = append(recs, r)
	}
	return chosen, recs
}

// render is a candidate's record, its copy and its index there; false when it can no longer
// go (its team gone, the item settled or dropped, or its content not in memory).
func (e *Engine) render(cand candidate, names map[paths.TeamID]namer, c *copies) (*state.Store, int, records.Record, bool) {
	h := e.teams[cand.team]
	if h == nil || !eligible(h, nil) {
		return nil, 0, nil, false
	}
	s := c.of(h)
	if s == nil {
		return nil, 0, nil, false
	}
	i := slices.IndexFunc(s.Pending, func(it state.Pending) bool { return it.ID == cand.id && it.Handed == nil })
	if i < 0 {
		return nil, 0, nil, false
	}
	v, known := h.content[cand.id]
	if needsContent(s.Pending[i]) && !known {
		return nil, 0, nil, false
	}
	return s, i, itemRecord(cand.team, s.Pending[i], v, names[cand.team], s.Outbox.Pings), true
}

// commit writes each copy, keeping it in memory only once written; it is the teams whose write
// failed, whose items stay pending.
func (e *Engine) commit(stores map[paths.TeamID]*state.Store) map[paths.TeamID]bool {
	failed := map[paths.TeamID]bool{}
	for _, id := range slices.SortedFunc(maps.Keys(stores), compareTeams) {
		s, h := stores[id], e.teams[id]
		if err := h.writer.Write(*s); err != nil {
			e.logf("team %s: the hand-out was not persisted, its items stay pending: %v", id, err)
			failed[id] = true
			continue
		}
		h.store = s
	}
	return failed
}

func unhanded(s *state.Store) int64 {
	n := int64(0)
	for _, it := range s.Pending {
		if it.Handed == nil {
			n++
		}
	}
	return n
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
// deadline with nothing handed it ends with no records, exit 4 timed-out (n3c-D7).
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
			return waitTimedOut()
		}
		release()
	}
}

// done is a wait's answer to err: timed-out at its own deadline, else internal.
func done(ctx context.Context, err error) wire.Done {
	if ctx.Err() != nil {
		return waitTimedOut()
	}
	return internal(err)
}

const msgWaitTimedOut exits.Template = "nothing arrived before wait's deadline"

// waitTimedOut is wait's end at its deadline with nothing handed: exit 4, no records.
func waitTimedOut() wire.Done {
	return doneOf(exits.New(exits.CodeTimedOut, nil, msgWaitTimedOut))
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
		s.AddLedger(state.Ledger{ID: it.ID, Room: it.Room, Item: it.Item, From: it.From, AtMS: now, Verb: it.Verb})
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
		returned := false
		for _, id := range e.order() {
			if !eligible(e.teams[id], nil) {
				continue
			}
			var (
				floor int64
				moved bool
				back  bool
			)
			err := e.changeLocked(id, func(h *held, s *state.Store) (bool, error) {
				var left []state.Pending
				left, back = settleStore(h, s, ack, now, deliver)
				floor, moved = raiseFloors(s, left)
				return len(left) > 0 || back, nil
			})
			if err == nil && moved {
				e.floorRaised(id, floor)
			}
			returned = returned || (err == nil && back)
			errs = append(errs, err)
		}
		// A wait that found every item handed sleeps until woken (N1).
		if returned {
			e.wake()
		}
	}); err != nil {
		return err
	}
	return errors.Join(errs...)
}

// settleStore is settle in one store's copy; it is the items that left pending, and whether
// any went back to pending.
func settleStore(h *held, s *state.Store, ack string, now int64, deliver func(*state.Store, state.Pending, int64) bool) ([]state.Pending, bool) {
	var left []state.Pending
	back := false
	kept := s.Pending[:0]
	for _, it := range s.Pending {
		if it.Handed == nil || *it.Handed != ack {
			kept = append(kept, it)
			continue
		}
		if deliver(s, it, now) {
			delete(h.content, it.ID)
			left = append(left, it)
			continue
		}
		back = true
		it.Handed, it.Repeat = nil, true
		kept = append(kept, it)
	}
	s.Pending = kept
	return left, back
}

// floorRaised tells t's team goroutine its store's main floor rose outside a sync answer, once
// the store holds it, so the marker follows (SY10); on the state goroutine.
func (e *Engine) floorRaised(t paths.TeamID, floor int64) {
	if h := e.teams[t]; h != nil && h.team != nil {
		h.team.FloorRaised(floor)
	}
}
