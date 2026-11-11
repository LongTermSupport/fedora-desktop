package teamsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The questions room's fixed times (QS2): the gap between join tries, and how long a refused
// join is a passing state.
const (
	joinGap    = 30 * time.Second
	inviteWait = 300 * time.Second
)

// errFilterMoved ends a long-poll whose filter is no longer the one the team would send (QS4).
var errFilterMoved = errors.New("teamsync: the questions room's readiness moved; the long-poll starts again")

// IndexBuilder is QX1's build and catch-up of the question index, run by a goroutine of the
// builder's own, never on the delivery path (n3f-c2 builds it; the team goroutine holds the
// seam).
type IndexBuilder interface {
	// Begin starts the job, ending one still running.
	Begin(ctx context.Context, job IndexJob)
	// Stop ends a running job and waits for it (QS9's demotion, the team's end).
	Stop()
}

// IndexJob is what the builder needs to build or catch the index up: the store's `questions`
// key as it is now (nil for a build from the room's start; a token for a catch-up; a `build`
// record for a build owed or under way), the room, and the questions floor items resume from
// (QS5).
type IndexJob struct {
	Key    *proto.QuestionsState
	RoomID string
	Floor  int64
	// s is the session the job was begun in: its client reads the room, and its judging takes the
	// room's items (Builder).
	s *sess
}

// qroom is the questions room as this link stands to it, guarded by its own lock: the team
// goroutine writes it, the builder hands it the index, and the verbs read it.
type qroom struct {
	mu sync.Mutex
	s  qstate
}

// qstate is the questions room's state. room is the roster's questions room while it names this
// member the non-parked coordinator, "" otherwise; since is QS2's clock (boot time); idx is the
// index, at the head when head is set, whose last folded event is last; chain says the previous
// request's filter held the room and its fold was written (QS7).
type qstate struct {
	room     string
	joined   bool
	verified bool
	refused  bool
	left     bool
	check    string
	head     bool
	chain    bool
	idx      *proto.QuestionIndex
	last     string
	since    time.Duration
	tried    bool
	triedAt  time.Duration
	floor    int64
	begun    bool
	noneSaid bool
}

func (q qstate) ready() bool {
	return q.room != "" && q.joined && q.verified && q.check == "" && q.head
}

func (q qstate) status(now time.Duration) team.QuestionsHealth {
	switch {
	case q.room == "":
		return team.QuestionsHealth{}
	case q.check != "":
		return team.QuestionsHealth{State: team.QuestionsUntrusted, Check: q.check}
	case !q.joined && (q.refused || q.left) && now-q.since >= inviteWait:
		return team.QuestionsHealth{State: team.QuestionsUntrusted, Check: team.CheckInvite}
	case !q.joined && q.left:
		return team.QuestionsHealth{State: team.QuestionsLeft}
	case !q.joined:
		return team.QuestionsHealth{State: team.QuestionsAwaitingInvite}
	case q.verified && q.head:
		return team.QuestionsHealth{State: team.QuestionsReady}
	}
	return team.QuestionsHealth{State: team.QuestionsBuilding}
}

// qget is a copy of the room's state.
func (t *Team) qget() qstate {
	t.q.mu.Lock()
	defer t.q.mu.Unlock()
	return t.q.s
}

// qset changes the room's state while it is still room, and says whether it did.
func (t *Team) qset(room string, change func(*qstate)) bool {
	t.q.mu.Lock()
	defer t.q.mu.Unlock()
	if t.q.s.room != room || room == "" {
		return false
	}
	change(&t.q.s)
	return true
}

// poke tells a long-poll in flight that the filter may have moved (QS4).
func (t *Team) poke() {
	select {
	case t.qmoved <- struct{}{}:
	default:
	}
}

// IndexAtHead is the builder's report that the index is at the head of the room: the index
// whose last folded event is last, which the team takes as its own. The room enters the filter
// at the next request, and one in flight ends (QS4).
func (t *Team) IndexAtHead(idx *proto.QuestionIndex, last string) {
	t.q.mu.Lock()
	if t.q.s.room != "" {
		t.q.s.idx, t.q.s.last, t.q.s.head, t.q.s.chain = idx, last, true, false
	}
	t.q.mu.Unlock()
	t.poke()
}

// QuestionsStatus is the questions room as this member stands to it now (QS2, QS8), for the
// verbs and `status`; QS2's 300 s are judged at the call, with no timer.
func (t *Team) QuestionsStatus() team.QuestionsHealth {
	now := t.cfg.Clock.Boot()
	return t.qget().status(now)
}

// qhealth gives the room's status to the engine when it changed.
func (t *Team) qhealth() {
	if st := t.QuestionsStatus(); st != t.health.Questions {
		t.setHealth(func(h *team.Health) { h.Questions = st })
	}
}

func wantedRoom(r proto.Roster, self string) string {
	if c, ok := r.Coordinator(); ok && c == self {
		return r.Rooms.Questions
	}
	return ""
}

// setQRole sets the room this member coordinates from a verified roster: a promotion starts
// QS2's clock, and a demotion is a room that was this member's and is not now (QS9).
func (t *Team) setQRole(r proto.Roster, self string) roleChange {
	want := wantedRoom(r, self)
	t.q.mu.Lock()
	var c roleChange
	if want != t.q.s.room {
		c = roleChange{promoted: want != "", demoted: t.q.s.room != ""}
		t.q.s = qstate{room: want, since: t.cfg.Clock.Boot()}
	}
	t.q.mu.Unlock()
	if c.promoted || c.demoted {
		t.poke()
	}
	return c
}

// roleChange is what a verified roster changed of this member's hold on the questions room.
type roleChange struct{ promoted, demoted bool }

// qsession is what a new session drops: the room's membership, trust and index are read again
// (QS5); the clock and the failed check stay, the link's.
func (t *Team) qsession() {
	t.q.mu.Lock()
	defer t.q.mu.Unlock()
	q := &t.q.s
	q.joined, q.verified, q.head, q.chain, q.idx, q.last, q.tried, q.begun = false, false, false, false, nil, "", false, false
}

// qstart is the questions room at a session's start, after main is verified and before the
// first sync (QS5): a store a member that does not coordinate it still holds is dropped by the
// first write (QS9), and the room of one that does is entered.
func (s *sess) qstart(ctx context.Context, store state.Store) error {
	t := s.t
	t.qsession()
	if _, err := s.qrole(); err != nil {
		return err
	}
	defer t.qhealth()
	if wantedRoom(s.roster, s.a.Self) == "" {
		s.dropOwed = s.dropOwed || holdsQuestions(store)
		return nil
	}
	return s.qenter(ctx, true)
}

// qrole is QS1's step for the roster just verified: a demotion is QS9's before anything else of
// the answer is judged, and promoted says the room is this member's now (QS10).
func (s *sess) qrole() (bool, error) {
	c := s.t.setQRole(s.roster, s.a.Self)
	if c.demoted {
		if err := s.qdemote(); err != nil {
			return false, err
		}
	}
	return c.promoted, nil
}

// qanswer is QS1's step 4: this member's own leave or a kick of the room (QS8), a trust trigger
// in its section (A 6.5), then the join and verification QS2 and QS5 owe. force is a roster
// change or a promotion.
func (s *sess) qanswer(ctx context.Context, a answer, force bool) error {
	t := s.t
	q := t.qget()
	if q.room == "" {
		return nil
	}
	force = force || s.qinvited
	if _, left := a.resp.Leave[q.room]; left {
		now := t.cfg.Clock.Boot()
		t.qset(q.room, func(q *qstate) {
			q.joined, q.verified, q.head, q.chain, q.idx, q.begun, q.left, q.since = false, false, false, false, nil, false, true, now
		})
		t.log.Printf("the questions room %s was left; it is joined again", q.room)
		t.poke()
		force = true
	} else if up, ok := a.resp.Join[q.room]; ok && q.joined && sideTrustTriggered(up) {
		t.qset(q.room, func(q *qstate) { q.verified = false })
		force = true
	}
	return s.qenter(ctx, force)
}

// sideTrustTriggered is A 6.5's re-verification of a side room: a limited timeline, a state
// section or a trust-bearing type in its timeline.
func sideTrustTriggered(up matrix.RoomUpdate) bool {
	if up.Timeline.Limited || len(up.State) > 0 {
		return true
	}
	return slices.ContainsFunc(up.Timeline.Events, func(raw json.RawMessage) bool {
		v, err := proto.Parse(raw)
		return err == nil && slices.Contains(proto.StateTypes, str(v, "type"))
	})
}

func (t *Team) stopBuilder() {
	if t.cfg.Builder != nil {
		t.cfg.Builder.Stop()
	}
}

// qdemote is QS9 before anything else of the answer: the build stopped and what the write drops
// logged by id; the write itself is the answer's, with DropQuestions.
func (s *sess) qdemote() error {
	t := s.t
	t.stopBuilder()
	store, err := t.cfg.Engine.Load(t.cfg.Team, s.a.Self)
	if err != nil {
		return err
	}
	for _, p := range store.Pending {
		if p.Room == proto.PurposeQuestions {
			t.log.Printf("demoted: the questions item %s is dropped", p.ID)
		}
	}
	s.dropOwed = true
	return nil
}

// holdsQuestions says whether a store holds anything of the questions room that a member who
// no longer coordinates it must drop (QS9).
func holdsQuestions(st state.Store) bool {
	o := st.Outbox
	return st.Questions != nil || st.RemindedMS != 0 || len(st.RemindLate) > 0 || o.QuestionsUnechoed != nil ||
		o.QuestionsHandover || o.QuestionsUncounted != (state.Uncounted{}) ||
		slices.ContainsFunc(st.Pending, func(p state.Pending) bool { return p.Room == proto.PurposeQuestions })
}

// qenter is QS2's join and QS5's start: the room's state read, joined by the roster's id when
// this member is not in it, verified by VerifySide, then its index loaded or built. force is a
// trigger (a start, a roster change, an invite), which the 30 s between tries do not hold back,
// and which re-verifies a room that failed its check. Only the engine's errors are returned;
// the room's own failures are its state (QS8).
func (s *sess) qenter(ctx context.Context, force bool) error {
	t := s.t
	room := t.qdue(force)
	if room == "" {
		return nil
	}
	raw, ok := s.qread(ctx, room)
	if !ok {
		return nil
	}
	if verr := proto.VerifySide(proto.PurposeQuestions, room, proto.StateEvents(raw), s.roster, s.a); verr != nil {
		s.quntrusted(room, sideCheck(verr))
		return nil
	}
	joinTS := max(ownMember(raw, s.a.Self).ts, 0)
	owesIndex := false
	t.qset(room, func(q *qstate) {
		q.joined, q.verified, q.check, q.refused, q.left = true, true, "", false, false
		owesIndex, q.begun = !q.begun, true
	})
	t.poke()
	if !owesIndex {
		return nil
	}
	return s.qindex(ctx, room, joinTS)
}

// qdue is the room when a join or verification of it is owed and due, else "": owed while it is
// not joined or not verified, or, at a trigger, failed its check; due at a trigger, or 30 s
// after the last try. The try is taken.
func (t *Team) qdue(force bool) string {
	now := t.cfg.Clock.Boot()
	t.q.mu.Lock()
	defer t.q.mu.Unlock()
	q := &t.q.s
	owed := !q.joined || !q.verified && q.check == "" || force && q.check != ""
	if q.room == "" || !owed || !force && q.tried && now-q.triedAt < joinGap {
		return ""
	}
	q.tried, q.triedAt = true, now
	return q.room
}

// qread is the room's full state, joined first when this member is not in it; a refused or
// missing state is the check `state`, any other failure a log line (QS8).
func (s *sess) qread(ctx context.Context, room string) ([]json.RawMessage, bool) {
	raw, err := s.c.RoomState(ctx, room)
	if err == nil && ownMember(raw, s.a.Self).membership != "join" || errors.Is(err, matrix.ErrForbidden) {
		if !s.qjoin(ctx, room) {
			return nil, false
		}
		raw, err = s.c.RoomState(ctx, room)
	}
	switch {
	case errors.Is(err, matrix.ErrForbidden) || errors.Is(err, matrix.ErrNotFound):
		s.quntrusted(room, "state")
		return nil, false
	case err != nil:
		s.t.log.Printf("the questions room %s not verified: %s", room, matrix.Class(err))
		return nil, false
	}
	return raw, true
}

// qjoin is QS2's `POST /join/<questions id>`: a 403 is not invited yet; another failure is a
// log line, and the next try is 30 s on.
func (s *sess) qjoin(ctx context.Context, room string) bool {
	_, err := s.c.Join(ctx, room)
	switch {
	case errors.Is(err, matrix.ErrForbidden):
		s.t.qset(room, func(q *qstate) { q.joined, q.refused = false, true })
		return false
	case err != nil:
		s.t.log.Printf("the questions room %s not joined: %s", room, matrix.Class(err))
		return false
	}
	s.t.qset(room, func(q *qstate) { q.joined, q.refused, q.left = true, false, false })
	return true
}

// quntrusted is QS8's failed check: the room out of the filter, and its index owed again, so
// that the recovery loads it or catches it up from its token (QX1).
func (s *sess) quntrusted(room, check string) {
	if s.t.qset(room, func(q *qstate) { q.verified, q.head, q.check, q.begun = false, false, check, false }) {
		s.t.log.Printf("the questions room %s is untrusted: the check %s failed", room, check)
		s.t.poke()
	}
}

// qindex is QS5's index: loaded from the store when its key is at the store's position with no
// build owed, else QX1's build or catch-up begun; and the floor items resume from, the later
// of the store's (with none, qresume's) and this member's own latest join.
func (s *sess) qindex(ctx context.Context, room string, joinTS int64) error {
	t := s.t
	store, err := t.cfg.Engine.Load(t.cfg.Team, s.a.Self)
	if err != nil {
		return err
	}
	floor, ok := store.Sync.FloorMS[proto.PurposeQuestions]
	if !ok {
		if floor, err = s.qresume(ctx, room); err != nil {
			return err
		}
	}
	floor = max(floor, joinTS)
	key := store.Questions
	if key != nil && key.Build == nil && key.Index != nil && key.Token != nil && key.Last != nil && *key.Token == store.Sync.NextBatch {
		idx, lerr := proto.LoadQuestionIndex(s.a.Server, s.roster.QuestionFloor, *key.Index)
		if lerr == nil {
			t.qset(room, func(q *qstate) { q.idx, q.last, q.head, q.chain, q.floor = idx, *key.Last, true, true, floor })
			t.poke()
			return nil
		}
		t.log.Printf("the stored question index does not load; it is built again: %v", lerr)
	}
	t.qset(room, func(q *qstate) { q.floor = floor })
	t.qbegin(ctx, IndexJob{Key: key, RoomID: room, Floor: floor, s: s})
	return nil
}

// qresume is QS5's floor with no store floor: the earlier of the room's own marker (SY10, per
// room) and the seat's release time, either alone when only one exists, 0 with neither.
func (s *sess) qresume(ctx context.Context, room string) (int64, error) {
	marker, hasMarker, err := s.marker(ctx, room)
	if err != nil {
		return 0, err
	}
	h := s.pair.Bundle.Held
	hasReleased := h != nil && h.ReleasedMS != nil
	switch {
	case hasMarker && hasReleased:
		return min(marker, *h.ReleasedMS), nil
	case hasReleased:
		return *h.ReleasedMS, nil
	}
	return marker, nil
}

// stateMember is self's membership among a room's state events, and the time of that event.
type stateMember struct {
	membership string
	ts         int64
}

func ownMember(raw []json.RawMessage, self string) stateMember {
	for _, r := range raw {
		v, err := proto.Parse(r)
		if err == nil && str(v, "type") == proto.MemberType && str(v, "state_key") == self {
			return stateMember{str(member(v, "content"), "membership"), originTs(v)}
		}
	}
	return stateMember{ts: -1}
}

// splitInvites are main's invite and the side rooms', for QS1's steps 1 and 3.
func splitInvites(all map[string]matrix.InvitedRoom, mainRoom string) (map[string]matrix.InvitedRoom, map[string]matrix.InvitedRoom) {
	mainInv, side := map[string]matrix.InvitedRoom{}, map[string]matrix.InvitedRoom{}
	for id, inv := range all {
		if id == mainRoom {
			mainInv[id] = inv
		} else {
			side[id] = inv
		}
	}
	return mainInv, side
}

// polled is a long-poll's outcome: its answer, whether its filter held the questions room, and
// moved, a request ended by a change of its filter, with no answer to handle.
type polled struct {
	resp  matrix.SyncResponse
	held  bool
	moved bool
}

// longPoll is SY2's request with QS4's filter; a change of the filter while it is in flight
// ends it.
func (s *sess) longPoll(ctx context.Context, since string) (polled, error) {
	held := s.t.qget().ready()
	sent := s.filterFor(held)
	s.t.onFilter(sent)
	rctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var wg sync.WaitGroup
	wg.Go(func() { s.watchFilter(rctx, cancel, sent) })
	resp, err := s.c.Sync(rctx, matrix.SyncRequest{Since: since, Timeout: longPoll, FilterJSON: sent})
	cancel(nil)
	wg.Wait()
	if err != nil && ctx.Err() == nil && errors.Is(context.Cause(rctx), errFilterMoved) {
		return polled{moved: true}, nil
	}
	return polled{resp: resp, held: held}, err
}

// qfold is an answer's fold of the questions room: a copy of the index with the answer's events
// folded, which replaces the live one only once the answer is persisted (QX4), and its last
// folded event.
type qfold struct {
	room   string
	idx    *proto.QuestionIndex
	last   string
	events bool
}

// qjudge is QS6: the questions room's events of an answer whose filter held it, judged as the
// questions room's after main's: each human post attached by the index whatever its verdict
// (n3f-D18), an agent's question or thread post folded, a delivery's item in `room: questions`
// from the room's floor on (QS5). A limited timeline is filled first (QS7).
func (s *sess) qjudge(ctx context.Context, j *judged, since string, a answer) error {
	q := s.t.qget()
	if !a.qheld || !q.ready() {
		return nil
	}
	events, reached, err := s.qgap(ctx, q, since, a.resp.Join[q.room].Timeline)
	switch {
	case errors.Is(err, matrix.ErrForbidden) || errors.Is(err, matrix.ErrNotFound):
		s.quntrusted(q.room, "state")
		return nil
	case err != nil:
		return err
	case !reached:
		return s.qcatchup(ctx, q)
	}
	jr, err := s.judgeRoom(ctx, j, roomJudge{room: q.room, purpose: proto.PurposeQuestions, events: events, res: s.t.qres, floor: q.floor})
	if err != nil {
		return err
	}
	f := &qfold{room: q.room, idx: q.idx.Copy(), last: q.last, events: len(events) > 0}
	if last := s.qtake(j, jr, f.idx, q.floor, func(string) bool { return true }); last != "" {
		f.last = last
	}
	j.q = f
	return nil
}

// qtake is the questions room's judged events folded into idx and taken (QX2, QS6), the one
// fold of the sync, the build and the catch-up: each event fed, each human post attached
// whatever its verdict (n3f-D18), and each event the room owes taken as an item in `room:
// questions` from the room's floor on (QS5); the questions floor raised to the newest. It gives
// the last folded event, "" for none.
func (s *sess) qtake(j *judged, jr roomJudged, idx *proto.QuestionIndex, floor int64, owes func(string) bool) string {
	last := ""
	counted := map[string]bool{}
	for i, e := range jr.evs {
		v := jr.vs[i]
		idx.Feed(v)
		if v.Human.Body != "" {
			idx.Attach(v.Event, v.Human)
		}
		if e.id != "" {
			last = e.id
		}
		if !owes(e.id) || v.Outcome == proto.Deliver && v.Event.OriginTs <= floor {
			continue
		}
		s.take(j, v, e.id, nil, jr.lk, counted, proto.PurposeQuestions)
	}
	if jr.floor > floor {
		if j.batch.Floors == nil {
			j.batch.Floors = map[string]int64{}
		}
		j.batch.Floors[proto.PurposeQuestions] = jr.floor
	}
	return last
}

// qgap is QS7's fill of a limited questions timeline, SY3's within its page bound: read back
// to the page holding the index's last folded event, or, when the previous request's fold was
// written (the chain), to the position the answer was asked from. The events after that event
// come oldest first; reached is false when the fill did not reach it (past the bound, or the
// room's start with no such event), which only QX1's catch-up can then place.
func (s *sess) qgap(ctx context.Context, q qstate, since string, tl matrix.Timeline) ([]json.RawMessage, bool, error) {
	if !tl.Limited {
		return tl.Events, true, nil
	}
	if tl.PrevBatch == "" {
		return nil, false, nil
	}
	isLast := func(raw json.RawMessage) bool {
		v, err := proto.Parse(raw)
		return err == nil && q.last != "" && str(v, "event_id") == q.last
	}
	to := ""
	if q.chain {
		to = since
	}
	older, gap, err := s.fill(ctx, q.room, tl.PrevBatch, to, func(page []json.RawMessage) bool {
		return slices.ContainsFunc(page, isLast)
	})
	if err != nil {
		return nil, false, err
	}
	events := slices.Concat(older, tl.Events)
	if i := slices.IndexFunc(events, isLast); i >= 0 {
		return events[i+1:], true, nil
	}
	return events, q.chain && !gap, nil
}

// qcatchup is QS7 past the bound: the room leaves the filter (QS4) and QX1's catch-up is begun
// from the store's key; the answer takes nothing of the room and leaves the key as it was
// (n3f-D19).
func (s *sess) qcatchup(ctx context.Context, q qstate) error {
	t := s.t
	if !t.qset(q.room, func(q *qstate) { q.head = false }) {
		return nil
	}
	t.log.Printf("the questions room %s has a gap the sync cannot fill; it is caught up apart", q.room)
	t.poke()
	store, err := t.cfg.Engine.Load(t.cfg.Team, s.a.Self)
	if err != nil {
		return err
	}
	t.qbegin(ctx, IndexJob{Key: store.Questions, RoomID: q.room, Floor: q.floor, s: s})
	return nil
}

// qbegin hands QX1 its job; with no builder the room stays building, said once.
func (t *Team) qbegin(ctx context.Context, job IndexJob) {
	if t.cfg.Builder != nil {
		t.cfg.Builder.Begin(ctx, job)
		return
	}
	said := false
	t.qset(job.RoomID, func(q *qstate) { said, q.noneSaid = q.noneSaid, true })
	if !said {
		t.log.Printf("no index builder: the questions room stays building")
	}
}

// qkey is the `questions` key the answer's write carries (QX5, n3f-D19): set only by an
// answer whose filter held the room and that is written anyway or folded an event, at the
// answer's position.
func qkey(j *judged, first bool) {
	f := j.q
	if f == nil || !f.events && !first && empty(j.batch) {
		return
	}
	st, tok, last := f.idx.State(), j.batch.NextBatch, f.last
	j.batch.Questions = &proto.QuestionsState{Index: &st, Token: &tok, Last: &last}
}

// qcommit is a persisted answer's fold made the live index; a write that held the key keeps the
// chain (QS7).
func (t *Team) qcommit(j judged) {
	f := j.q
	if f == nil {
		return
	}
	written := j.batch.Questions != nil
	t.qset(f.room, func(q *qstate) {
		q.idx, q.last, q.chain = f.idx, f.last, q.chain || written
		if fl, ok := j.batch.Floors[proto.PurposeQuestions]; ok {
			q.floor = max(q.floor, fl)
		}
	})
}

func (s *sess) watchFilter(ctx context.Context, cancel context.CancelCauseFunc, sent json.RawMessage) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.t.qmoved:
			if !bytes.Equal(s.filterOf(), sent) {
				cancel(errFilterMoved)
				return
			}
		}
	}
}
