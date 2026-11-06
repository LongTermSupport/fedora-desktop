package teamsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/bundle"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The sync's fixed numbers: the long-poll (SY2), the page bound of a backwards read (SY1 (b),
// SY3) and the floor marker's coalescing (SY10).
const (
	longPoll    = 30 * time.Second
	pageSize    = 100
	maxPages    = 20
	floorPutGap = 10 * time.Second
)

// sess is one session: the pair and its client as read at its start, and the roster verified.
type sess struct {
	t      *Team
	c      *matrix.Client
	pair   bundle.Pair
	a      proto.Anchors
	roster proto.Roster
	// epoch is the restore epoch the session's position belongs to.
	epoch int64
	// floor is main's floor as persisted.
	floor int64
	// dropOwed is QS9's drop of the questions room, owed by the next write.
	dropOwed bool
	// qinvited is an invite to the questions room seen in this answer, QS2's signal to join.
	qinvited bool
}

// session is one pass of the loop: verify, load, start (SY1), the run's ends, SY6's read, then
// the long-poll until an error.
func (t *Team) session(ctx context.Context, wg *sync.WaitGroup) error {
	c, pair, err := t.current()
	if err != nil {
		return err
	}
	s := &sess{t: t, c: c, pair: pair, a: anchorsOf(t.cfg.Team, pair)}
	if verr := s.verify(ctx); verr != nil {
		return verr
	}
	if t.recoveredPair == nil || !reflect.DeepEqual(*t.recoveredPair, pair) {
		if rerr := t.cfg.Engine.RecoverSends(ctx, t.cfg.Team, t.Notices()); rerr != nil {
			return rerr
		}
		t.recoveredPair = &pair
	}
	store, err := t.cfg.Engine.Load(t.cfg.Team, s.a.Self)
	if err != nil {
		return err
	}
	s.epoch, s.floor = s.roster.RestoreEpoch, store.Sync.FloorMS[proto.PurposeMain]
	t.knowFloor(proto.PurposeMain, s.floor, s.epoch)
	t.knowFloor(proto.PurposeQuestions, store.Sync.FloorMS[proto.PurposeQuestions], s.epoch)
	t.freeze.reset()
	if qerr := s.qstart(ctx, store); qerr != nil {
		return qerr
	}
	since, readd, err := s.start(ctx, store)
	if err != nil {
		return err
	}
	if err := s.ends(ctx, store, readd, wg); err != nil {
		return err
	}
	if err := s.epochCheck(ctx); err != nil {
		return err
	}
	defer s.startReceipts(ctx)()
	// RP1: the reports join runs beside the long-poll, never in its way.
	t.kickReports(true)
	return s.poll(ctx, since)
}

// verify is A 6.4 over main's full state; a 403 is tried once more after a join.
func (s *sess) verify(ctx context.Context) error {
	r, st, content, err := mainState(ctx, s.c, s.a)
	if errors.Is(err, matrix.ErrForbidden) {
		if _, jerr := s.c.Join(ctx, s.a.MainRoom); jerr != nil {
			return jerr
		}
		r, st, content, err = mainState(ctx, s.c, s.a)
	}
	if err != nil {
		return err
	}
	s.roster = r
	s.t.setRoster(r, s.a.Self)
	s.t.setFacts(readFacts(st, r, s.a))
	s.t.saveRoster(ctx, r.Rev, content)
	return nil
}

// verifyMain is A 6.4 over main's full state: the roster verified, and its event's content as
// read, for the roster cache.
func verifyMain(ctx context.Context, c *matrix.Client, a proto.Anchors) (proto.Roster, json.RawMessage, error) {
	r, _, content, err := mainState(ctx, c, a)
	return r, content, err
}

// mainState is main's roster as A 6.4 verifies it, the state it was read from, and the roster
// event's content.
func mainState(ctx context.Context, c *matrix.Client, a proto.Anchors) (proto.Roster, []proto.StateEvent, json.RawMessage, error) {
	raw, err := c.RoomState(ctx, a.MainRoom)
	if errors.Is(err, matrix.ErrNotFound) {
		return proto.Roster{}, nil, nil, fmt.Errorf("%w: main's state", errUntrusted)
	}
	if err != nil {
		return proto.Roster{}, nil, nil, err
	}
	st := proto.StateEvents(raw)
	r, err := proto.VerifyMain(st, a)
	if err != nil {
		return proto.Roster{}, nil, nil, err
	}
	return r, st, rosterContent(raw), nil
}

// rosterContent is the roster event's content among main's state events.
func rosterContent(events []json.RawMessage) json.RawMessage {
	for _, raw := range events {
		v, err := proto.Parse(raw)
		if err == nil && str(v, "type") == proto.RosterType && str(v, "state_key") == "" {
			return member(v, "content").Compact()
		}
	}
	return nil
}

// saveRoster gives a verified rev the team has not saved to the engine's roster cache
// (n3d-D3); a failure is said and blocks nothing, and the next verification tries again.
func (t *Team) saveRoster(ctx context.Context, rev int64, content json.RawMessage) {
	t.mu.Lock()
	saved := t.savedRev == rev
	t.mu.Unlock()
	if saved {
		return
	}
	if err := t.cfg.Engine.SaveRoster(ctx, t.cfg.Team, rev, content); err != nil {
		if !errors.Is(err, team.ErrRemoved) {
			t.log.Printf("the roster at rev %d was not cached: %v", rev, err)
		}
		return
	}
	t.mu.Lock()
	t.savedRev = rev
	t.mu.Unlock()
}

// start is SY1: (a) a position of the roster's epoch is kept; (c) a fresh store of a removed
// account takes no history; (b) any other resumes from its marker and release time.
func (s *sess) start(ctx context.Context, store state.Store) (string, bool, error) {
	t := s.t
	next := store.Sync.NextBatch
	if next != "" && store.Sync.RestoreEpoch != s.epoch {
		if err := t.cfg.Engine.ResetPosition(ctx, t.cfg.Team, s.epoch); err != nil {
			return "", false, err
		}
		next = ""
	}
	switch {
	case next != "":
		return next, false, nil
	case store.Sync.Fresh && slices.Contains(t.cfg.RemovedUserIDs, s.a.Self):
		since, err := s.readd(ctx)
		return since, true, err
	}
	since, err := s.resume(ctx)
	return since, false, err
}

// filterOf is the sync filter (QS4, G5): the questions room is asked for only while it is
// joined, verified and its index at the head.
func (s *sess) filterOf() json.RawMessage { return s.filterFor(s.t.qget().ready()) }

func (s *sess) filterFor(ready bool) json.RawMessage {
	return proto.SyncFilter(s.roster, s.a, ready).Compact()
}

// readd is SY1 (c): one sync with timeline limit 0, persisted with no items.
func (s *sess) readd(ctx context.Context) (string, error) {
	f := proto.SyncFilterReAdd(s.roster, s.a, s.t.qget().ready()).Compact()
	s.t.onFilter(f)
	resp, err := s.c.Sync(ctx, matrix.SyncRequest{FilterJSON: f})
	if err != nil {
		return "", err
	}
	s.t.answeredAt()
	if err := s.epochCheck(ctx); err != nil {
		return "", err
	}
	if _, err := s.persist(ctx, team.Batch{NextBatch: resp.NextBatch}); err != nil {
		return "", err
	}
	s.t.recovered()
	return resp.NextBatch, nil
}

// bounds are SY1 (b)'s resume bounds: the marker's floor and the seat's release time.
type bounds struct {
	floor, released       int64
	hasFloor, hasReleased bool
}

// resume is SY1 (b): a sync with no position, main read backwards past both bounds, and the
// events after the earlier of them judged, those at or before the release time or the member's
// own latest goodbye marked restored.
func (s *sess) resume(ctx context.Context) (string, error) {
	var bd bounds
	var err error
	if bd.floor, bd.hasFloor, err = s.marker(ctx, s.a.MainRoom); err != nil {
		return "", err
	}
	if h := s.pair.Bundle.Held; h != nil && h.ReleasedMS != nil {
		bd.released, bd.hasReleased = *h.ReleasedMS, true
	}
	held := s.t.qget().ready()
	f := s.filterFor(held)
	s.t.onFilter(f)
	resp, err := s.c.Sync(ctx, matrix.SyncRequest{FilterJSON: f})
	if err != nil {
		return "", err
	}
	s.t.answeredAt()
	tl := resp.Join[s.a.MainRoom].Timeline
	events, gap := tl.Events, false
	if tl.Limited && tl.PrevBatch != "" {
		older, g, ferr := s.fill(ctx, s.a.MainRoom, tl.PrevBatch, "", s.passed(bd))
		if ferr != nil {
			return "", ferr
		}
		events, gap = append(older, events...), g
	}
	kept, restored := s.cut(events, bd)
	if err := s.handle(ctx, "", answer{resp: resp, events: kept, first: true, gap: gap, restored: restored, qheld: held}); err != nil {
		return "", err
	}
	return resp.NextBatch, nil
}

// marker is room's teamchat.floor marker, decoded strictly; an undecodable one is none, said
// once per room.
func (s *sess) marker(ctx context.Context, room string) (int64, bool, error) {
	raw, found, err := s.c.RoomAccountData(ctx, s.a.Self, room, paths.FloorAccountData)
	if err != nil || !found {
		return 0, false, err
	}
	floor, ok := decodeMarker(raw)
	if !ok && !s.t.markerSaid[room] {
		s.t.markerSaid[room] = true
		s.t.log.Printf("the %s marker in %s does not decode; resuming as with none", paths.FloorAccountData, room)
	}
	return floor, ok, nil
}

func decodeMarker(raw json.RawMessage) (int64, bool) {
	v, err := proto.Parse(raw)
	if err != nil || v.Kind() != proto.KindObject || len(v.Members()) != 3 {
		return 0, false
	}
	ver, floor, epoch := member(v, "v"), member(v, "floor_ms"), member(v, "restore_epoch")
	if ver.Kind() != proto.KindInt || ver.Int() != 1 || floor.Kind() != proto.KindInt || floor.Int() < 0 || epoch.Kind() != proto.KindInt || epoch.Int() < 0 {
		return 0, false
	}
	return floor.Int(), true
}

func markerContent(floor, epoch int64) json.RawMessage {
	return proto.Obj(
		proto.M("v", proto.Int(1)), proto.M("floor_ms", proto.Int(floor)), proto.M("restore_epoch", proto.Int(epoch)),
	).Compact()
}

// passed is SY1 (b)'s stop for a backwards read: past the marker's floor (or, with none, the
// member's own join) and past the release time (none counts as passed).
func (s *sess) passed(bd bounds) func([]json.RawMessage) bool {
	pastFloor, pastReleased := false, !bd.hasReleased
	return func(page []json.RawMessage) bool {
		for _, raw := range page {
			v, err := proto.Parse(raw)
			if err != nil {
				continue
			}
			ts := originTs(v)
			pastFloor = pastFloor || bd.hasFloor && ts <= bd.floor || !bd.hasFloor && s.ownJoin(v)
			pastReleased = pastReleased || ts <= bd.released
		}
		return pastFloor && pastReleased
	}
}

// cut keeps the events after the earlier bound, and is when an event counts restored.
func (s *sess) cut(events []json.RawMessage, bd bounds) ([]json.RawMessage, func(int64) bool) {
	join, goodbye := s.landmarks(events)
	var kept []json.RawMessage
	for i, raw := range events {
		ts := int64(-1)
		if v, err := proto.Parse(raw); err == nil {
			ts = originTs(v)
		}
		afterFloor := bd.hasFloor && ts > bd.floor || !bd.hasFloor && i > join
		if afterFloor || bd.hasReleased && ts > bd.released {
			kept = append(kept, raw)
		}
	}
	return kept, func(ts int64) bool {
		return bd.hasReleased && ts <= bd.released || goodbye >= 0 && ts <= goodbye
	}
}

// landmarks are the index of the member's own latest join among events, and the time of its own
// latest goodbye; -1 for none.
func (s *sess) landmarks(events []json.RawMessage) (int, int64) {
	join, goodbye := -1, int64(-1)
	for i, raw := range events {
		v, err := proto.Parse(raw)
		switch {
		case err != nil:
		case s.ownJoin(v):
			join = i
		case s.ownGoodbye(v):
			goodbye = max(goodbye, originTs(v))
		}
	}
	return join, goodbye
}

func (s *sess) ownJoin(v proto.Value) bool {
	return str(v, "type") == proto.MemberType && str(v, "state_key") == s.a.Self && str(member(v, "content"), "membership") == "join"
}

// ownGoodbye is the member's own `left` or `disconnected` notice.
func (s *sess) ownGoodbye(v proto.Value) bool {
	if str(v, "type") != proto.MessageType || str(v, "sender") != s.a.Self {
		return false
	}
	c, reason := proto.DecodeContent(member(v, "content"), proto.MainKeys)
	return reason == "" && c.Key == proto.KeyNotice && (c.Notice.Event == proto.EventLeft || c.Notice.Event == proto.EventDisconnected)
}

func originTs(v proto.Value) int64 {
	if ts := member(v, "origin_server_ts"); ts.Kind() == proto.KindInt {
		return ts.Int()
	}
	return -1
}

// fill reads a room backwards from a batch token (down to another, when not empty), 100 a page,
// until stop says so or the pages end; past the page bound it is a gap. The events come oldest
// first.
func (s *sess) fill(ctx context.Context, room, from, to string, stop func([]json.RawMessage) bool) ([]json.RawMessage, bool, error) {
	var pages [][]json.RawMessage
	done := false
	for range s.t.pages.max {
		page, err := s.c.Messages(ctx, room, matrix.MessagesRequest{From: from, To: to, Dir: matrix.Backward, Limit: s.t.pages.size})
		if err != nil {
			return nil, false, err
		}
		pages = append(pages, page.Chunk)
		if done = page.End == "" || len(page.Chunk) == 0 || stop(page.Chunk); done {
			break
		}
		from = page.End
	}
	var out []json.RawMessage
	for _, p := range slices.Backward(pages) {
		for _, raw := range slices.Backward(p) {
			out = append(out, raw)
		}
	}
	return out, !done, nil
}

// ends is the run's start: the previous run closed (EN3) and this one begun, before the first
// long-poll; then, once per team and never after a re-add, back (EN4) in its own goroutine.
func (s *sess) ends(ctx context.Context, store state.Store, readd bool, wg *sync.WaitGroup) error {
	t := s.t
	if store.Run.ID != t.cfg.Run {
		if err := t.cfg.Ends.Close(ctx, t.cfg.Team, t.Notices(), store.Run); err != nil {
			return err
		}
		if err := t.cfg.Engine.BeginRun(ctx, t.cfg.Team); err != nil {
			return err
		}
	}
	if t.backStarted || readd {
		return nil
	}
	reloaded, err := t.cfg.Engine.Load(t.cfg.Team, s.a.Self)
	if err != nil {
		return err
	}
	t.backStarted = true
	wg.Go(func() { t.back(ctx, reloaded.Run) })
	return nil
}

func (t *Team) back(ctx context.Context, run state.StoreRun) {
	if !t.cfg.Plain {
		select {
		case <-t.cfg.FirstSessionStart:
		case <-ctx.Done():
			return
		}
	}
	if err := t.cfg.Ends.Back(ctx, t.cfg.Team, t.Notices(), run); err != nil && ctx.Err() == nil {
		t.log.Printf("back not posted: %s", matrix.Class(err))
	}
}

// epochCheck is SY6: the roster's epoch read before an answer is persisted; another epoch
// drops the position and starts again.
func (s *sess) epochCheck(ctx context.Context) error {
	raw, err := s.c.GetState(ctx, s.a.MainRoom, proto.RosterType, "")
	if errors.Is(err, matrix.ErrNotFound) || errors.Is(err, matrix.ErrForbidden) {
		return fmt.Errorf("%w: the roster", errUntrusted)
	}
	if err != nil {
		return err
	}
	v, err := proto.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: the roster does not parse", errUntrusted)
	}
	r, err := proto.DecodeRoster(v, s.a.Server, s.a.Team, s.a.MainRoom)
	if err != nil {
		return err
	}
	if r.RestoreEpoch == s.epoch {
		return nil
	}
	if err := s.t.cfg.Engine.ResetPosition(ctx, s.t.cfg.Team, r.RestoreEpoch); err != nil {
		return err
	}
	s.t.log.Printf("restore epoch %d, not %d: the answer is discarded and the position dropped", r.RestoreEpoch, s.epoch)
	return errRestart
}

// poll is SY2's long-poll, each answer handled before the next.
func (s *sess) poll(ctx context.Context, since string) error {
	t := s.t
	for {
		if pause, frozen := t.freeze.atStart(); frozen {
			return s.frozen(pause)
		}
		t.onPoll(since)
		started := t.cfg.Clock.Boot()
		p, err := s.longPoll(ctx, since)
		if pause, frozen := t.freeze.atAnswer(started); frozen {
			return s.frozen(pause)
		}
		if err != nil {
			return err
		}
		if p.moved {
			continue
		}
		t.answeredAt()
		if err := s.handle(ctx, since, answer{resp: p.resp, qheld: p.held}); err != nil {
			return err
		}
		since = p.resp.NextBatch
	}
}

// frozen is SY12's restart: the connections dropped, one line with the pause.
func (s *sess) frozen(pause time.Duration) error {
	s.c.Close()
	s.t.log.Printf("a freeze or suspend of %s: the connection is dropped and the session starts again", pause.Round(time.Millisecond))
	return errRestart
}

func (t *Team) answeredAt() {
	t.setHealth(func(h *team.Health) { h.LastSyncOKMS = t.cfg.Clock.WallMS() })
}

// answer is one sync answer to handle; events and restored are SY1 (b)'s, which reads main
// before the answer is handled.
type answer struct {
	resp     matrix.SyncResponse
	events   []json.RawMessage
	first    bool
	gap      bool
	restored func(int64) bool
	// qheld is the request's filter holding the questions room (QS4), whose events only then
	// are this answer's to judge.
	qheld bool
}

// handle is one answer's steps in QS1's order: main's invite, main's trust, the questions
// room's role (QS9, QS10), the side rooms' invites, the questions room's membership and trust,
// then the gap, the verdicts, SY6, the persist and the floor marker.
func (s *sess) handle(ctx context.Context, since string, a answer) error {
	t := s.t
	mainInv, sideInv := splitInvites(a.resp.Invite, s.a.MainRoom)
	joined := s.invites(ctx, mainInv)
	main := a.resp.Join[s.a.MainRoom]
	rev := s.roster.Rev
	if s.trustTriggered(a, main, joined) {
		if err := s.verify(ctx); err != nil {
			return err
		}
		if !a.first {
			t.kickReports(false)
		}
	}
	s.faults(a.resp.Join)
	promoted, err := s.qrole()
	if err != nil {
		return err
	}
	s.qinvited = false
	s.invites(ctx, sideInv)
	if err = s.qanswer(ctx, a, promoted || s.roster.Rev != rev); err != nil {
		return err
	}
	events, gap, err := s.eventsOf(ctx, since, a, main)
	if err != nil {
		return err
	}
	j, err := s.judgeAll(ctx, events, a.restored)
	if err != nil {
		return err
	}
	if err = s.qjudge(ctx, &j, since, a); err != nil {
		return err
	}
	if gap {
		j.batch.Gap = true
		j.batch.Counts.Gaps++
	}
	if err := s.epochCheck(ctx); err != nil {
		return err
	}
	j.batch.NextBatch = a.resp.NextBatch
	j.batch.DropQuestions = s.dropOwed
	qkey(&j, a.first)
	floor := s.floor
	if a.first || !empty(j.batch) {
		failpoint.Hit(failpoint.LinkAfterJudgeBeforePersist)
		var err error
		if floor, err = s.persist(ctx, j.batch); err != nil {
			return err
		}
		s.dropOwed = false
	}
	return s.settle(ctx, j, floor)
}

// settle is what a handled answer leaves once persisted: its effects committed, the questions
// fold made live, the markers moved and put, the failure streak ended.
func (s *sess) settle(ctx context.Context, j judged, floor int64) error {
	t := s.t
	s.commit(j, floor)
	t.qcommit(j)
	if err := s.qmarked(j); err != nil {
		return err
	}
	t.qhealth()
	t.putFloorIfDue(ctx, s.c, s.a)
	t.recovered()
	return nil
}

// eventsOf are main's events of an answer, oldest first: SY1 (b)'s as read, else the timeline,
// a limited one filled back to since (SY3).
func (s *sess) eventsOf(ctx context.Context, since string, a answer, main matrix.RoomUpdate) ([]json.RawMessage, bool, error) {
	tl := main.Timeline
	if a.first {
		return a.events, a.gap, nil
	}
	if !tl.Limited || tl.PrevBatch == "" {
		return tl.Events, false, nil
	}
	older, gap, err := s.fill(ctx, s.a.MainRoom, tl.PrevBatch, since, func([]json.RawMessage) bool { return false })
	if err != nil {
		return nil, false, err
	}
	return append(older, tl.Events...), gap, nil
}

// commit is what a persisted answer leaves: the flood window, UH1's counted ids, the log lines
// and the floor f the store keeps, which the marker carries (B 2.3.2).
func (s *sess) commit(j judged, f int64) {
	t := s.t
	t.flood = j.flood
	t.budget = j.budget
	t.budget.Prune(proto.DeliveryWindowMs, j.now)
	for _, id := range j.unheard {
		t.unheard.put(id, struct{}{})
	}
	for _, line := range j.lines {
		t.log.Print(line)
	}
	t.noteForge(j.forge)
	if f > 0 && f != s.floor {
		s.floor = f
		t.floorMoved(proto.PurposeMain, f, s.epoch)
	}
}

func empty(b team.Batch) bool {
	return len(b.Items) == 0 && len(b.Redacts) == 0 && len(b.Floors) == 0 && len(b.Answered) == 0 &&
		len(b.Echoes) == 0 && !b.Gap && b.Counts == team.Counts{} && len(b.HeldBack) == 0 &&
		b.Questions == nil && !b.DropQuestions
}

// persist is SY5's one store write, after which the team's position is the batch's; a failed
// write is SY13's, said once per streak.
func (s *sess) persist(ctx context.Context, b team.Batch) (int64, error) {
	t := s.t
	b.Team, b.RestoreEpoch, b.Fresh = t.cfg.Team, s.epoch, false
	floor, err := t.cfg.Engine.Persist(ctx, b)
	switch {
	case err == nil:
		t.persistSaid = false
	case errors.Is(err, team.ErrRemoved):
	case !t.persistSaid:
		t.persistSaid = true
		t.log.Printf("the store write failed; the team takes no items until it succeeds: %v", err)
	}
	return floor, err
}

// invites is SY11 over the answer's invites, in room order; a failed join or leave is a log
// line and holds nothing. It says whether a room was joined.
func (s *sess) invites(ctx context.Context, invites map[string]matrix.InvitedRoom) bool {
	t := s.t
	joined := false
	for _, room := range slices.Sorted(maps.Keys(invites)) {
		inviter, creator := inviteSenders(invites[room].InviteState, s.a.Self)
		switch proto.Invite(room, inviter, creator, s.a, &s.roster) {
		case proto.InviteAccept:
			if room == s.roster.Rooms.Questions {
				// QS2: an invite is only the signal; the room is joined by the roster's id.
				s.qinvited = true
				continue
			}
			if _, err := s.c.Join(ctx, room); err != nil {
				t.log.Printf("invite to %s not joined: %s", room, matrix.Class(err))
				continue
			}
			joined = true
		case proto.InvitePending:
			if !t.pendingSaid[room] {
				t.pendingSaid[room] = true
				t.log.Printf("invite to %s left pending: the roster does not name it yet", room)
			}
		case proto.InviteReject:
			if err := s.c.Leave(ctx, room); err != nil {
				t.log.Printf("invite to %s not rejected: %s", room, matrix.Class(err))
			}
		}
	}
	return joined
}

// inviteSenders are the inviter of self and the room's creator, from an invite's stripped state.
func inviteSenders(stripped []json.RawMessage, self string) (string, string) {
	var inviter, creator string
	for _, raw := range stripped {
		v, err := proto.Parse(raw)
		if err != nil {
			continue
		}
		switch str(v, "type") {
		case proto.MemberType:
			if str(v, "state_key") == self && str(member(v, "content"), "membership") == "invite" {
				inviter = str(v, "sender")
			}
		case proto.CreateType:
			creator = str(v, "sender")
		}
	}
	return inviter, creator
}

// trustTriggered is SY14: a join, main left, a limited timeline, a state section (not on SY1
// (b)'s first answer, which always has one) or a trust-bearing type in the timeline.
func (s *sess) trustTriggered(a answer, main matrix.RoomUpdate, joined bool) bool {
	_, left := a.resp.Leave[s.a.MainRoom]
	if joined || left || main.Timeline.Limited || !a.first && len(main.State) > 0 {
		return true
	}
	return slices.ContainsFunc(main.Timeline.Events, func(raw json.RawMessage) bool {
		v, err := proto.Parse(raw)
		return err == nil && slices.Contains(proto.StateTypes, str(v, "type"))
	})
}

// judged is one answer's verdicts, and the effects committed only once it is persisted.
type judged struct {
	batch   team.Batch
	lines   []string
	unheard []string
	flood   proto.Window
	forge   forgeNews
	// budget is the delivery budget's window with this answer's admissions, committed as
	// flood is; now is the judging's wall time.
	budget proto.Window
	now    int64
	// q is the questions room's fold, nil when the answer did not hold the room.
	q *qfold
}

// judgeAll is SY4 with SY4a's resolve first: main's events judged oldest first over a Lookup
// that answers from memory; a transient failure of a related fetch is the error, before any
// verdict.
func (s *sess) judgeAll(ctx context.Context, events []json.RawMessage, restored func(int64) bool) (judged, error) {
	t := s.t
	j := judged{flood: copyWindow(t.flood), budget: copyWindow(t.budget), now: t.cfg.Clock.WallMS()}
	jr, err := s.judgeRoom(ctx, &j, roomJudge{room: s.a.MainRoom, purpose: proto.PurposeMain, events: events, res: t.res, floor: s.floor})
	if err != nil || len(jr.evs) == 0 {
		return j, err
	}
	j.forge = t.forgeCheck(ctx, s.roster.Forge, jr.vs)
	if err := ctx.Err(); err != nil {
		return judged{}, err
	}
	counted := map[string]bool{}
	for i, e := range jr.evs {
		s.take(&j, jr.vs[i], e.id, restored, jr.lk, counted, proto.PurposeMain)
	}
	if jr.floor > s.floor {
		j.batch.Floors = map[string]int64{proto.PurposeMain: jr.floor}
	}
	return j, nil
}

// roomJudge is one room's events to judge: the room, its purpose, and the resolver of its
// relations (a room's own, so a relation naming another room's event poisons nothing there).
type roomJudge struct {
	room, purpose string
	events        []json.RawMessage
	res           *resolver
	floor         int64
}

type parsed struct {
	v  proto.Value
	id string
}

// roomJudged is a room's verdicts, in its events' order, the Lookup they were judged over and
// the latest origin time among them.
type roomJudged struct {
	evs   []parsed
	vs    []proto.Verdict
	lk    *memLookup
	floor int64
}

// judgeRoom is SY4 with SY4a's resolve first over one room's events, oldest first, over a
// Lookup that answers from memory; a transient failure of a related fetch is the error, before
// any verdict.
func (s *sess) judgeRoom(ctx context.Context, j *judged, rj roomJudge) (roomJudged, error) {
	t := s.t
	var evs []parsed
	for _, raw := range rj.events {
		v, err := proto.Parse(raw)
		if err != nil {
			j.batch.Counts.Dropped++
			j.lines = append(j.lines, "an event that does not parse was dropped")
			continue
		}
		if _, isState := v.Get("state_key"); isState {
			continue
		}
		evs = append(evs, parsed{v, str(v, "event_id")})
	}
	if len(evs) == 0 {
		return roomJudged{floor: rj.floor}, nil
	}
	ids := make([]string, 0, len(evs))
	var rels []relation
	var relIDs []string
	for _, e := range evs {
		ids = append(ids, e.id)
		for _, id := range proto.RelationIDs(e.v, s.roster) {
			rels = append(rels, relation{event: e.id, id: id})
			relIDs = append(relIDs, id)
		}
	}
	view, err := t.cfg.Engine.View(t.cfg.Team, ids, relIDs, s.roster)
	if err != nil {
		return roomJudged{}, err
	}
	resolved, err := rj.res.resolve(ctx, s.resolveInput(view, rj.room), rels)
	if err != nil {
		return roomJudged{}, err
	}
	lk := &memLookup{view: view, resolved: resolved, judged: map[string]bool{}}
	judge := proto.Judge{
		Server: s.a.Server, Self: s.a.Self, RoomID: rj.room, Purpose: rj.purpose,
		Roster: s.roster, NowMs: j.now, Lookup: lk, Flood: &j.flood,
	}
	out := roomJudged{evs: evs, lk: lk, floor: rj.floor, vs: make([]proto.Verdict, 0, len(evs))}
	for _, e := range evs {
		out.vs = append(out.vs, t.judge(judge, e.v))
		if e.id != "" {
			lk.judged[e.id] = true
		}
		out.floor = max(out.floor, originTs(e.v))
	}
	return out, nil
}

func (s *sess) resolveInput(view team.View, room string) resolveInput {
	return resolveInput{
		self: s.a.Self, room: room, roster: s.roster, view: view,
		get: func(ctx context.Context, id string) (json.RawMessage, error) {
			return s.c.GetEvent(ctx, room, id)
		},
	}
}

// take is one verdict's outcome in the batch (the table of SY4), its item in the room of
// purpose room, and UH1's count.
func (s *sess) take(j *judged, v proto.Verdict, id string, restored func(int64) bool, lk proto.Lookup, counted map[string]bool, room string) {
	switch v.Outcome {
	case proto.Deliver:
		if !s.admit(j, v) {
			return
		}
		p := state.Pending{
			ID: v.Event.ID, Room: room, Item: itemKind(v.Kind), SentMS: v.Event.OriginTs,
			From: v.Event.Sender, Restored: restored != nil && restored(v.Event.OriginTs),
		}
		if p.Item == records.KindPing {
			p.Verb = new(v.Content.Ping.Verb)
		}
		j.batch.Items = append(j.batch.Items, team.Item{Pending: p, Verdict: v})
		answered(j, v)
	case proto.Ignore:
		s.ignored(j, v, id, lk, counted)
	case proto.Drop:
		j.batch.Counts.Dropped++
		j.lines = append(j.lines, fmt.Sprintf("dropped %s: %s", id, v.Reason))
	case proto.Redact:
		j.batch.Redacts = append(j.batch.Redacts, v.Redacts)
		j.batch.Counts.Redacted++
	case proto.Record:
		answered(j, v)
	case proto.Index, proto.Fault:
		// n3f's questions and reports.
	}
}

// ignored is an ignored event's count, an own ping's echo (A 7.6 rule 3, n3d-D9) and UH1.
func (s *sess) ignored(j *judged, v proto.Verdict, id string, lk proto.Lookup, counted map[string]bool) {
	if v.Reason != proto.IgnoreSeen {
		j.batch.Counts.Ignored++
	}
	if _, own := lk.OwnPing(id); own && v.Reason == proto.IgnoreSelf {
		j.batch.Echoes = append(j.batch.Echoes, team.Echo{ID: id, OriginMS: v.Event.OriginTs})
	}
	if s.unheard(v, lk) && id != "" && !counted[id] && !s.t.unheard.has(id) {
		counted[id] = true
		j.batch.Counts.Unheard++
		j.unheard = append(j.unheard, id)
		j.lines = append(j.lines, fmt.Sprintf("unheard %s: %s", id, v.Reason))
	}
}

// answered is SY4's answer to an own ping (A 8.2): a recorded ack or stale answer, or a
// delivered nack, done or blocked. The engine judges whether it is on time (n3d-D8).
func answered(j *judged, v proto.Verdict) {
	if v.Answer == nil {
		return
	}
	j.batch.Answered = append(j.batch.Answered, team.Answer{
		Ping: v.Answer.Ping, From: v.Event.Sender, Verb: v.Answer.Verb, ID: v.Event.ID, OriginMS: v.Event.OriginTs,
	})
}

// unheard is UH1: a human's message ignored as no text (`type`, `empty`), or addressed to no
// agent at all.
func (s *sess) unheard(v proto.Verdict, lk proto.Lookup) bool {
	if v.Class != proto.SenderHuman {
		return false
	}
	switch v.Reason {
	case proto.IgnoreType, proto.IgnoreEmpty:
		return true
	case proto.IgnoreNotAddressed:
		return proto.ReachedNoAgent(v.Human, s.roster, lk)
	}
	return false
}

func itemKind(k string) string {
	switch k {
	case proto.KindHuman:
		return records.KindHuman
	case proto.KindPeer:
		return records.KindPeer
	case proto.KindPing:
		return records.KindPing
	case proto.KindTimeout:
		return records.KindTimeout
	}
	return records.KindNotice
}

func copyWindow(w proto.Window) proto.Window {
	c := proto.Window{Times: make(map[string][]int64, len(w.Times))}
	for k, v := range w.Times {
		c.Times[k] = slices.Clone(v)
	}
	return c
}

// knowFloor is the floor a session starts from, which FlushFloor puts. Unless this goroutine
// already put that floor, it is owed: the last link may have ended, or been killed, before its
// coalesced put, and the marker may lag the store (A2).
func (t *Team) knowFloor(purpose string, floor, epoch int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if f := t.markerOf(purpose); floor > 0 && f != nil {
		f.ms, f.epoch, f.known = floor, epoch, true
		f.dirty = !f.sent || f.sentMS != floor || f.sentEpoch != epoch
	}
}

// markerOf is the marker of the room of purpose, nil for a room that has none; t.mu is held.
func (t *Team) markerOf(purpose string) *floorMarker {
	switch purpose {
	case proto.PurposeMain:
		return &t.floor
	case proto.PurposeQuestions:
		return &t.qfloor
	}
	return nil
}

// floorMoved is a persisted floor move of the room of purpose; a move down is owed at once,
// never coalesced, so a seat taken with no store never cuts the item it was lowered for. The
// floor already known changes nothing.
func (t *Team) floorMoved(purpose string, floor, epoch int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f := t.markerOf(purpose)
	if f == nil || f.known && f.ms == floor && f.epoch == epoch {
		return
	}
	f.urgent = f.urgent || f.known && floor < f.ms
	f.ms, f.epoch, f.known, f.dirty = floor, epoch, true, true
}

// qmarked gives a write that held the questions floor to that room's marker: the store's floor,
// which the engine keeps below the room's owed items (B 2.3.2), since Persist answers main's.
func (s *sess) qmarked(j judged) error {
	t := s.t
	if _, held := j.batch.Floors[proto.PurposeQuestions]; !held {
		return nil
	}
	store, err := t.cfg.Engine.Load(t.cfg.Team, s.a.Self)
	if err != nil {
		return err
	}
	if f, ok := store.Sync.FloorMS[proto.PurposeQuestions]; ok {
		t.floorMoved(proto.PurposeQuestions, f, s.epoch)
	}
	return nil
}

// FloorRaised is the store's main floor raised outside a sync answer (team.Team): the marker's
// next put carries it, coalesced as any move up. A floor not above the one known, or with none
// known (so no epoch), changes nothing.
func (t *Team) FloorRaised(floor int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.floor.known && floor > t.floor.ms {
		t.floor.ms, t.floor.dirty = floor, true
	}
}

// markerRoom is a room a marker is put in, and the purpose whose marker it is.
type markerRoom struct{ purpose, id string }

// markerRooms are the rooms the markers are put in now: main, and the questions room while this
// member coordinates it and is joined to it.
func (t *Team) markerRooms(a proto.Anchors) []markerRoom {
	rooms := []markerRoom{{proto.PurposeMain, a.MainRoom}}
	if q := t.qget(); q.room != "" && q.joined {
		rooms = append(rooms, markerRoom{proto.PurposeQuestions, q.room})
	}
	return rooms
}

// putFloorIfDue is SY10's coalescing without a timer, per room: a moved floor is put after an
// answer once floorPutGap has passed since that room's last put, and a floor moved down at once.
func (t *Team) putFloorIfDue(ctx context.Context, c *matrix.Client, a proto.Anchors) {
	for _, r := range t.markerRooms(a) {
		t.mu.Lock()
		f := t.markerOf(r.purpose)
		due := f == nil || f.dirty && (f.urgent || !f.put || t.cfg.Clock.Boot()-f.putAt >= floorPutGap)
		t.mu.Unlock()
		if !due {
			continue
		}
		if err := t.putFloor(ctx, c, a.Self, r); err != nil {
			t.log.Printf("the %s marker in %s was not put: %s", paths.FloorAccountData, r.id, matrix.Class(err))
		}
	}
}

func (t *Team) putFloor(ctx context.Context, c *matrix.Client, self string, r markerRoom) error {
	t.mu.Lock()
	m := t.markerOf(r.purpose)
	if m == nil {
		t.mu.Unlock()
		return fmt.Errorf("teamsync: the %s room has no floor marker", r.purpose)
	}
	f := *m
	if f.known {
		m.put, m.putAt = true, t.cfg.Clock.Boot()
	}
	t.mu.Unlock()
	if !f.known {
		return nil
	}
	if err := c.PutRoomAccountData(ctx, self, r.id, paths.FloorAccountData, markerContent(f.ms, f.epoch)); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	m.sent, m.sentMS, m.sentEpoch = true, f.ms, f.epoch
	if m.ms == f.ms && m.epoch == f.epoch {
		m.dirty, m.urgent = false, false
	}
	return nil
}

// FlushFloor puts every room's floor marker at once, at a removal before the store goes (SY10);
// each room is tried, and the failures are its error.
func (t *Team) FlushFloor(ctx context.Context) error {
	c, pair, err := t.current()
	if err != nil {
		return err
	}
	a := anchorsOf(t.cfg.Team, pair)
	var errs []error
	for _, r := range t.markerRooms(a) {
		if perr := t.putFloor(ctx, c, a.Self, r); perr != nil {
			errs = append(errs, perr)
		}
	}
	return errors.Join(errs...)
}
