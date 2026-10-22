package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const msgStoreRefused exits.Template = "the store of team {} does not decode; run status and tell a human"

// errNoStore is a write before the team's first Load made its store.
var errNoStore = errors.New("engine: the team has no store yet")

// clone is a deep copy of s, so a caller's change never reaches the engine's memory.
func clone(s state.Store) (state.Store, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return state.Store{}, fmt.Errorf("engine: copy the store: %w", err)
	}
	var out state.Store
	if err := json.Unmarshal(data, &out); err != nil {
		return state.Store{}, fmt.Errorf("engine: copy the store: %w", err)
	}
	return out, nil
}

// heldFor is t's held team for a team goroutine's call: ErrRemoved once removed (or never
// held), the store's refusal while refused.
func (e *Engine) heldFor(t paths.TeamID) (*held, error) {
	h := e.teams[t]
	switch {
	case h == nil || h.removed:
		return nil, team.ErrRemoved
	case h.refused:
		return nil, exits.New(exits.CodeStore, &t, msgStoreRefused, exits.Team(t))
	}
	return h, nil
}

// change is one store write of t's store: f changes a copy, and the copy is written and kept
// only when f says it changed and the write succeeded, so memory never runs ahead of the disk.
func (e *Engine) change(ctx context.Context, t paths.TeamID, f func(h *held, s *state.Store) (bool, error)) error {
	var out error
	if err := e.do(ctx, func() { out = e.changeLocked(t, f) }); err != nil {
		return err
	}
	return out
}

func (e *Engine) changeLocked(t paths.TeamID, f func(h *held, s *state.Store) (bool, error)) error {
	h, err := e.heldFor(t)
	if err != nil {
		return err
	}
	if h.store == nil {
		return errNoStore
	}
	s, err := clone(*h.store)
	if err != nil {
		return err
	}
	changed, err := f(h, &s)
	if err != nil || !changed {
		return err
	}
	if err := h.writer.Write(s); err != nil {
		return fmt.Errorf("engine: team %s: %w", t, err)
	}
	h.store = &s
	e.publish()
	return nil
}

// Load is t's store, created for userID with this run when there is none yet (team.Engine).
func (e *Engine) Load(t paths.TeamID, userID string) (state.Store, error) {
	var (
		out     state.Store
		loadErr error
	)
	if err := e.do(context.Background(), func() { out, loadErr = e.loadLocked(t, userID) }); err != nil {
		return state.Store{}, err
	}
	return out, loadErr
}

func (e *Engine) loadLocked(t paths.TeamID, userID string) (state.Store, error) {
	h, err := e.heldFor(t)
	if err != nil {
		return state.Store{}, err
	}
	if h.store == nil {
		now := e.cfg.Clock.WallMS()
		bucket := proto.FullBucket(proto.DefaultLimits(), now)
		s := state.NewStore(t, userID, state.StoreRun{ID: e.cfg.Run, StartedMS: now},
			state.Gate{Credit: bucket.Credit, AtMS: bucket.AtMs, Recent: []state.GateKey{}})
		if err := h.writer.Write(s); err != nil {
			return state.Store{}, fmt.Errorf("engine: team %s: create the store: %w", t, err)
		}
		h.store = &s
		if h.userID == "" {
			h.userID = userID
		}
		e.publish()
	}
	return clone(*h.store)
}

// View is what Judge reads of the store (gap 14, SY4a): which ids are pending or delivered,
// what the ledger and the outbox know of the related events, and this member's own pings.
func (e *Engine) View(t paths.TeamID, ids, relations []string, r proto.Roster) (team.View, error) {
	var (
		out     team.View
		viewErr error
	)
	if err := e.do(context.Background(), func() {
		h, err := e.heldFor(t)
		if err != nil {
			viewErr = err
			return
		}
		out = view(h, ids, relations, r)
	}); err != nil {
		return team.View{}, err
	}
	return out, viewErr
}

func view(h *held, ids, relations []string, r proto.Roster) team.View {
	v := team.View{SeenIDs: map[string]bool{}, Events: map[string]proto.Related{}, OwnPings: map[string]proto.OwnPing{}}
	if h.store == nil {
		return v
	}
	s := h.store
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	related := map[string]bool{}
	for _, id := range relations {
		related[id] = true
	}
	for _, p := range s.Pending {
		if want[p.ID] {
			v.SeenIDs[p.ID] = true
		}
	}
	for _, l := range s.Ledger {
		if want[l.ID] {
			v.SeenIDs[l.ID] = true
		}
		if related[l.ID] {
			v.Events[l.ID] = ledgerRelated(l, h.anchors.MainRoom, r)
		}
	}
	for _, p := range s.Outbox.Pings {
		v.OwnPings[p.ID] = proto.OwnPing{Verb: p.Verb, To: slices.Clone(p.To), OriginTs: p.SentMS}
		if related[p.ID] {
			v.Events[p.ID] = pingRelated(p, s.UserID, h.anchors.MainRoom, r)
		}
	}
	return v
}

// ledgerRelated is a delivered item as a later event's relation reads it.
func ledgerRelated(l state.Ledger, main string, r proto.Roster) proto.Related {
	rel := proto.Related{Sender: l.From, RoomID: roomID(main, r, l.Room), AddressedToSelf: true, AddressedToRosterAgent: true}
	if l.Verb != nil {
		rel.Verb = *l.Verb
	}
	return rel
}

// pingRelated is one of this member's outbox pings as a later event's relation reads it.
func pingRelated(p state.Ping, self, main string, r proto.Roster) proto.Related {
	return proto.Related{
		Sender: self, RoomID: main, Verb: p.Verb,
		AddressedToRosterAgent: slices.ContainsFunc(p.To, r.HumanReader),
	}
}

// roomID is the room id of a store's room purpose: main from the bundle, the side rooms from
// the roster; "" for a room neither names.
func roomID(main string, r proto.Roster, purpose string) string {
	switch purpose {
	case proto.PurposeMain:
		return main
	case proto.PurposeQuestions:
		return r.Rooms.Questions
	case proto.PurposeReports:
		return r.Rooms.Reports
	}
	return ""
}

// Persist is one store write of b (SY5): its position, floors and new items, its redactions
// removing items still pending or handed, and the outbox updates its echoes and answers made,
// expired pings pruned; then a wake of every wait and the TIMEOUT timer re-armed. It returns the
// store's main floor after the write. An item already pending or delivered is not added twice.
// A full pending list refuses the whole batch, loudly (B 2.3.2), so nothing is dropped and the
// batch is fetched again; an answer in a refused batch is not counted until it is (SY7 case 4).
func (e *Engine) Persist(ctx context.Context, b team.Batch) (int64, error) {
	var (
		out   error
		floor int64
	)
	if err := e.do(ctx, func() {
		defer e.arm()
		var (
			added   map[string]proto.Verdict
			notes   map[string][]answerNote
			unknown []string
			pruned  []string
		)
		now := e.cfg.Clock.WallMS()
		out = e.changeLocked(b.Team, func(_ *held, s *state.Store) (bool, error) {
			var err error
			if added, err = apply(s, b); err != nil {
				return false, err
			}
			floor = s.Sync.FloorMS[proto.PurposeMain]
			notes, unknown = applyOutbox(s, b)
			pruned = pruneOutbox(s, now)
			return true, nil
		})
		if out != nil {
			return
		}
		// Memory follows the disk: only a written batch's content, counts and answers are kept.
		h := e.teams[b.Team]
		maps.Copy(h.content, added)
		for _, id := range b.Redacts {
			delete(h.content, id)
		}
		h.counts = addCounts(h.counts, b.Counts)
		h.note(notes, pruned)
		for _, id := range unknown {
			e.logf("team %s: an answer to %s, which is not in the outbox, is ignored", b.Team, id)
		}
		e.wake()
	}); err != nil {
		return 0, err
	}
	return floor, out
}

// apply is b in a copy of its store; it is the items added, with their verdicts.
func apply(s *state.Store, b team.Batch) (map[string]proto.Verdict, error) {
	added := map[string]proto.Verdict{}
	s.Sync.NextBatch, s.Sync.RestoreEpoch, s.Sync.Fresh = b.NextBatch, b.RestoreEpoch, b.Fresh
	if len(b.Floors) > 0 && s.Sync.FloorMS == nil {
		s.Sync.FloorMS = map[string]int64{}
	}
	maps.Copy(s.Sync.FloorMS, b.Floors)
	known := map[string]bool{}
	for _, p := range s.Pending {
		known[p.ID] = true
	}
	for _, l := range s.Ledger {
		known[l.ID] = true
	}
	for _, it := range b.Items {
		if known[it.Pending.ID] {
			continue
		}
		p := it.Pending
		if verb := it.Verdict.Content.Ping.Verb; p.Item == records.KindPing && verb != "" {
			p.Verb = &verb // n3d-D30: a ping's verb from its verdict
		}
		if err := s.AddPending(p); err != nil {
			return nil, fmt.Errorf("engine: team %s: %w", b.Team, err)
		}
		known[it.Pending.ID] = true
		added[it.Pending.ID] = it.Verdict
	}
	s.Pending = slices.DeleteFunc(s.Pending, func(p state.Pending) bool { return slices.Contains(b.Redacts, p.ID) })
	ownedFloors(s)
	return added, nil
}

// ownedFloors is B 2.3.2's floor rule: a room the store owes items in has its floor just below
// the oldest of them, so each is newer than its floor and survives a restart (B44); a room it
// owes none in keeps the batch's high-water. B 2.3.2 counts `awaiting` too, whose entries carry
// ids only: whatever writes them must give them a sent time, or set the floor before moving
// items there.
func ownedFloors(s *state.Store) {
	for _, p := range s.Pending {
		if s.Sync.FloorMS == nil {
			s.Sync.FloorMS = map[string]int64{}
		}
		if floor, ok := s.Sync.FloorMS[p.Room]; !ok || p.SentMS <= floor {
			s.Sync.FloorMS[p.Room] = max(p.SentMS-1, 0)
		}
	}
}

// raiseFloors is B 2.3.2's floor rule after items left pending outside a sync answer (an ack,
// a drop): each room they were in is raised to just below the oldest item it still owes, or,
// owing none, to the newest that left; a floor is never lowered here. It is main's floor, and
// whether that moved.
func raiseFloors(s *state.Store, left []state.Pending) (int64, bool) {
	newest := map[string]int64{}
	for _, p := range left {
		newest[p.Room] = max(newest[p.Room], p.SentMS)
	}
	before := s.Sync.FloorMS[proto.PurposeMain]
	for room, to := range newest {
		owes, oldest := false, int64(0)
		for _, p := range s.Pending {
			if p.Room == room && (!owes || p.SentMS < oldest) {
				owes, oldest = true, p.SentMS
			}
		}
		if owes {
			to = oldest - 1
		}
		if s.Sync.FloorMS == nil {
			s.Sync.FloorMS = map[string]int64{}
		}
		if floor, ok := s.Sync.FloorMS[room]; !ok || to > floor {
			s.Sync.FloorMS[room] = max(to, 0)
		}
	}
	after := s.Sync.FloorMS[proto.PurposeMain]
	return after, after != before
}

func addCounts(a, b team.Counts) team.Counts {
	return team.Counts{
		Ignored: a.Ignored + b.Ignored, Dropped: a.Dropped + b.Dropped, Unheard: a.Unheard + b.Unheard,
		Gaps: a.Gaps + b.Gaps, Redacted: a.Redacted + b.Redacted,
	}
}

// wake ends every wait's sleep; on the state goroutine.
func (e *Engine) wake() {
	for _, w := range e.waiters {
		close(w)
	}
	e.waiters = nil
}

// ResetPosition is SY6: the position dropped and the new restore epoch stored.
func (e *Engine) ResetPosition(ctx context.Context, t paths.TeamID, epoch int64) error {
	return e.change(ctx, t, func(_ *held, s *state.Store) (bool, error) {
		s.Sync.NextBatch = ""
		s.Sync.RestoreEpoch = epoch
		return true, nil
	})
}

// SetEnded records the store's run's end (EN1, EN3).
func (e *Engine) SetEnded(ctx context.Context, t paths.TeamID, end state.RunEnd) error {
	return e.change(ctx, t, func(_ *held, s *state.Store) (bool, error) {
		s.Run.Ended = &end
		return true, nil
	})
}

// BeginRun makes the store's run this link's, ended null; nothing when it already is.
func (e *Engine) BeginRun(ctx context.Context, t paths.TeamID) error {
	return e.change(ctx, t, func(_ *held, s *state.Store) (bool, error) {
		if s.Run.ID == e.cfg.Run && s.Run.Ended == nil {
			return false, nil
		}
		s.Run = state.StoreRun{ID: e.cfg.Run, StartedMS: e.cfg.Clock.WallMS()}
		return true, nil
	})
}

// SetHealth is a team goroutine's state, for status, status.json and the keeper's report. It
// re-arms the TIMEOUT timer too, so one that slept through a suspend is corrected by the first
// sync after it (n3d-D7), and wakes the team's sender, which posts once the team is up.
func (e *Engine) SetHealth(t paths.TeamID, hl team.Health) {
	if err := e.do(context.Background(), func() {
		if h := e.teams[t]; h != nil && !h.removed {
			h.health = hl
			e.publish()
			h.pokeSender()
		}
		e.arm()
	}); err != nil {
		e.logf("team %s: health not recorded: %v", t, err)
	}
}
