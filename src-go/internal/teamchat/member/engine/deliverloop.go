package engine

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/deliver"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/inbox"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// B 2.7.3's pace, on the boot clock: one delivery (or membership notice) per paceWait; after a
// failed write the next in retryWait; every write failed for stallAfter is a stall; a team's
// trailing facts alone go at most once per trailingEvery (n3e-D14).
const (
	paceWait      = 2 * time.Second
	retryWait     = 10 * time.Second
	stallAfter    = 120 * time.Second
	trailingEvery = time.Minute
)

// pace is what a turn's work costs the next turn.
type pace int

const (
	// paceNone: nothing was chosen; the next turn is not held.
	paceNone pace = iota
	// paceSlot: a delivery's turn; the next in paceWait.
	paceSlot
	// paceRetry: a failure; the next in retryWait.
	paceRetry
)

// job is one delivery's team, as the turn's look found it.
type job struct {
	id    paths.TeamID
	h     *held
	tm    team.Team
	names namer
	items []state.Pending
	known map[string]bool
}

// looked is one team the turn's look can deliver for.
type looked struct {
	job

	factsDue bool
}

// dial is a built delivery and the session it goes to.
type dial struct {
	target     inbox.Target
	transcript string
	built      deliver.Built
	told       told
}

// deliverLoop is B 2.7.3's delivery, beside the state goroutine until ctx ends: each turn
// takes a queued membership notice or one team's delivery, no sooner than the pace allows,
// and idles while there is nothing to deliver. It makes the network calls; every store change
// is on the state goroutine.
func (e *Engine) deliverLoop(ctx context.Context) {
	var (
		next   time.Duration
		queued *memberReq
	)
	for ctx.Err() == nil {
		var j *job
		if queued == nil {
			var due time.Duration
			if j, due = e.pick(ctx); j == nil {
				queued = e.idle(ctx, due)
				continue
			}
		}
		if e.cfg.Clock.Boot() < next {
			queued = e.waitTurn(ctx, next, queued)
			continue
		}
		start := e.cfg.Clock.Boot()
		var p pace
		if queued != nil {
			p = e.sendMember(ctx, *queued)
			queued = nil
		} else {
			p = e.deliver(ctx, j)
		}
		switch p {
		case paceSlot:
			next = start + paceWait
		case paceRetry:
			next = start + retryWait
		case paceNone:
		}
	}
}

// idle waits for a poke, a membership notice, the earliest due time (trailing facts, a
// re-fetch's backoff; 0 for none) or ctx's end; it is the notice taken, if any.
func (e *Engine) idle(ctx context.Context, due time.Duration) *memberReq {
	timer, release := (<-chan time.Time)(nil), func() bool { return false }
	if due > 0 {
		d := due - e.cfg.Clock.Boot()
		if d <= 0 {
			return nil
		}
		timer, release = e.cfg.Clock.Hold(d)
	}
	defer release()
	e.idles.Add(1)
	select {
	case <-e.kick:
	case req := <-e.members:
		return &req
	case <-timer:
	case <-ctx.Done():
	}
	return nil
}

// waitTurn holds until the boot clock reaches next, taking one membership notice meanwhile.
func (e *Engine) waitTurn(ctx context.Context, next time.Duration, queued *memberReq) *memberReq {
	timer, release := e.cfg.Clock.Hold(next - e.cfg.Clock.Boot())
	defer release()
	for {
		members := e.members
		if queued != nil {
			members = nil
		}
		select {
		case <-timer:
			return queued
		case req := <-members:
			queued = &req
		case <-ctx.Done():
			return queued
		}
	}
}

// pick is a turn's look (steps 1 and 2): nil while delivery is not live or no verified team
// has a deliverable item or due facts, with the earliest time something falls due.
func (e *Engine) pick(ctx context.Context) (*job, time.Duration) {
	var (
		seen []looked
		due  time.Duration
	)
	if err := e.do(ctx, func() {
		if e.liveLocked() {
			seen, due = e.look()
		}
	}); err != nil {
		return nil, 0
	}
	teams := map[paths.TeamID]team.Team{}
	for _, l := range seen {
		teams[l.id] = l.tm
	}
	names := rosters(teams)
	byTeam := map[paths.TeamID][]state.Pending{}
	for _, l := range seen {
		if names[l.id].verified {
			byTeam[l.id] = l.items
		}
	}
	id, ok := deliver.Choose(byTeam)
	for _, l := range seen {
		if !ok && names[l.id].verified && l.factsDue {
			id, ok = l.id, true
		}
		if ok && l.id == id {
			j := l.job
			j.names = names[id]
			return &j, 0
		}
	}
	return nil, due
}

// look is every team delivery can go to now (held, with a store, a team goroutine and a
// handle), its deliverable items and whether its facts alone are due; on the state goroutine.
// A restored item still to be fetched waits out a failed re-fetch's backoff.
func (e *Engine) look() ([]looked, time.Duration) {
	now := e.cfg.Clock.Boot()
	var (
		out []looked
		due time.Duration
	)
	for _, id := range e.order() {
		h := e.teams[id]
		if !eligible(h, nil) || h.team == nil || h.handle() == "" {
			continue
		}
		l, at := lookAt(h, now)
		if at > 0 && (due == 0 || at < due) {
			due = at
		}
		out = append(out, l)
	}
	return out, due
}

// lookAt is one team's look at now, and the earliest time it falls due (0 for none).
func lookAt(h *held, now time.Duration) (looked, time.Duration) {
	l := looked{job: job{id: h.id, h: h, tm: h.team, known: map[string]bool{}}}
	var due time.Duration
	for _, p := range h.store.Pending {
		if p.Handed != nil || p.Held {
			continue
		}
		_, known := h.content[p.ID]
		if needsContent(p) && !known && now < h.delivery.fetchAfter {
			due = h.delivery.fetchAfter
			continue
		}
		l.items = append(l.items, p)
		l.known[p.ID] = known
	}
	if at, has := h.delivery.factsAt(h.health.Stopped); has {
		if l.factsDue = now >= at; !l.factsDue && (due == 0 || at < due) {
			due = at
		}
	}
	return l, due
}

// plan is the job's re-fetch candidates: HUMAN items first, then by sent_ms.
func (j *job) plan() plan {
	its := slices.Clone(j.items)
	slices.SortStableFunc(its, func(a, b state.Pending) int {
		if ah, bh := a.Item == records.KindHuman, b.Item == records.KindHuman; ah != bh {
			if ah {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.SentMS, b.SentMS)
	})
	p := plan{teams: map[paths.TeamID]team.Team{j.id: j.tm}}
	for _, it := range its {
		p.cands = append(p.cands, candidate{team: j.id, id: it.ID, room: it.Room, sent: it.SentMS, fetch: needsContent(it) && !j.known[it.ID]})
	}
	return p
}

// deliver is one team's delivery (steps 3 to 8): its restored items re-fetched, the delivery
// built, the socket dialled, the awaiting entry persisted before the user line, and the user
// line's outcome persisted after it.
func (e *Engine) deliver(ctx context.Context, j *job) pace {
	got := e.refetch(ctx, j.plan())
	var (
		d dial
		p pace
	)
	if err := e.do(ctx, func() { d, p = e.prepare(j, got) }); err != nil || d.built.Text == "" {
		return p
	}
	conn, closeConn, p := e.connect(ctx, d.target)
	if conn == nil {
		return p
	}
	defer closeConn()
	e.hit(pointAfterAuth)
	size, err := transcriptSize(d.transcript)
	if err != nil {
		e.logf("delivery to team %s not written, retry in %s: %v", j.id, retryWait, err)
		return paceRetry
	}
	var proceed, entry bool
	if doErr := e.do(ctx, func() { proceed, entry = e.await(j, d, size) }); doErr != nil || !proceed {
		return paceSlot
	}
	if entry {
		e.hit(pointAfterAwaiting)
	}
	err = conn.WriteUser(d.built.Text)
	if err == nil {
		e.hit(pointAfterUserLine)
	}
	out := paceRetry
	if doErr := e.do(ctx, func() { out = e.after(j, d, err) }); doErr != nil {
		return paceRetry
	}
	if err == nil {
		if ferr := conn.Finish(); ferr != nil {
			e.logf("delivery to team %s: the user line was written, its connection did not finish: %v", j.id, ferr)
		}
	}
	return out
}

// prepare is step 4, on the state goroutine: the re-fetched items judged again, then the
// delivery built. No text means nothing to dial: the failed slots are dropped in one write.
func (e *Engine) prepare(j *job, got map[key]fetched) (dial, pace) {
	h := e.teams[j.id]
	if h != j.h || !eligible(h, nil) || !e.liveLocked() {
		return dial{}, paceNone
	}
	now := e.cfg.Clock.Boot()
	for _, f := range got {
		if f.err != nil && !errors.Is(f.err, team.ErrItemGone) {
			h.delivery.fetchAfter = now + retryWait
		}
	}
	c := copies{e: e, stores: map[paths.TeamID]*state.Store{}, raised: map[paths.TeamID]int64{}}
	e.rejudgeAll(got, &c)
	if failed := e.commit(c.stores); failed[j.id] {
		return dial{}, paceRetry
	}
	for id, floor := range c.raised {
		e.floorRaised(id, floor)
	}
	if len(c.stores) > 0 {
		e.publish()
	}
	t := h.delivery.take(h.health.Stopped, j.names)
	built, err := deliver.Build(j.id, h.handle(), deliveryItems(h), t.facts, j.names.sender, func() (string, error) { return newNonce(h.store) })
	if err != nil {
		e.logf("team %s: its trailing facts are dropped, they do not render: %v", j.id, err)
		h.delivery.drop(h.health.Stopped)
		return dial{}, paceSlot
	}
	if built.Text == "" {
		e.dropFailed(h, built.Failed)
		return dial{}, paceSlot
	}
	return dial{
		target: inbox.Target{Socket: e.sess.socket, Token: e.sess.token}, transcript: e.sess.transcript,
		built: built, told: t,
	}, paceSlot
}

// dropFailed drops the items whose entry failed a slot, in one write; on the state goroutine.
func (e *Engine) dropFailed(h *held, ids []string) {
	if len(ids) == 0 {
		return
	}
	var (
		floor int64
		moved bool
	)
	if err := e.changeLocked(h.id, func(_ *held, s *state.Store) (bool, error) {
		left := takePending(s, ids)
		floor, moved = raiseFloors(s, left)
		return len(left) > 0, nil
	}); err != nil {
		e.logf("team %s: the items that do not render stay pending: %v", h.id, err)
		return
	}
	if moved {
		e.floorRaised(h.id, floor)
	}
	e.countRender(h, ids)
}

// connect dials the session's socket (step 5): a socket gone stops delivery until a hook tells
// a new one; any other failure is a failed write, retried. The connection closes when ctx ends,
// so a shutdown never waits on a phase; closeConn closes it otherwise.
func (e *Engine) connect(ctx context.Context, t inbox.Target) (inbox.Conn, func(), pace) {
	conn, err := e.cfg.Inbox.Dial(ctx, t)
	if err != nil {
		gone := errors.Is(err, inbox.ErrGone)
		if doErr := e.do(ctx, func() {
			if gone {
				e.sess.gone = true
				e.logf("delivery stops: the session's inbox socket is gone, until a hook tells a new one: %v", err)
				return
			}
			e.failed()
			e.logf("delivery: no connection to the session's inbox, retry in %s: %v", retryWait, err)
		}); doErr != nil || !gone {
			return nil, nil, paceRetry
		}
		return nil, nil, paceSlot
	}
	stop := context.AfterFunc(ctx, func() { e.closeConn(conn) })
	return conn, func() {
		if stop() {
			e.closeConn(conn)
		}
	}, paceSlot
}

func (e *Engine) closeConn(conn inbox.Conn) {
	if err := conn.Close(); err != nil {
		e.logf("delivery: the inbox connection did not close: %v", err)
	}
}

// await is step 6, on the state goroutine: nothing goes when the team or any item changed
// meanwhile (a recv took them); else one write drops the failed slots and, with items to show,
// adds the awaiting entry, written unknown, its items handed to the nonce. It is whether the
// user line goes, and whether an entry was written.
func (e *Engine) await(j *job, d dial, size int64) (bool, bool) {
	h := e.teams[j.id]
	if h != j.h || !eligible(h, nil) {
		return false, false
	}
	ids, failed := d.built.IDs, d.built.Failed
	for _, id := range slices.Concat(ids, failed) {
		if !deliverable(h.store, id) {
			return false, false
		}
	}
	if len(ids) == 0 && len(failed) == 0 {
		return true, false
	}
	var (
		floor int64
		moved bool
	)
	if err := e.changeLocked(j.id, func(_ *held, s *state.Store) (bool, error) {
		floor, moved = raiseFloors(s, takePending(s, failed))
		if len(ids) == 0 {
			return true, nil
		}
		at := e.cfg.Clock.Boot().Milliseconds()
		a := state.Awaiting{
			Nonce: d.built.Delivery.Nonce, IDs: slices.Clone(ids),
			Transcript: state.TranscriptMark{Path: d.transcript, Offset: size}, AtMS: at, Written: state.WrittenUnknown,
		}
		if e.sess.idle {
			a.StopMS = &at
		}
		if err := s.AddAwaiting(a); err != nil {
			return false, err
		}
		for i := range s.Pending {
			if slices.Contains(ids, s.Pending[i].ID) {
				nonce := a.Nonce
				s.Pending[i].Handed = &nonce
			}
		}
		return true, nil
	}); err != nil {
		e.logf("team %s: the delivery is not sent, its awaiting entry was not persisted: %v", j.id, err)
		return false, false
	}
	if moved {
		e.floorRaised(j.id, floor)
	}
	e.countRender(h, failed)
	return true, len(ids) > 0
}

// after is step 7's outcome, on the state goroutine. A full write is `written: "yes"` and the
// items into the ledger, in one write, and the facts told; nothing written takes the entry
// back, its items pending with no repeat; a cut line leaves the entry unknown for its proof.
func (e *Engine) after(j *job, d dial, err error) pace {
	switch {
	case err == nil:
		e.wrote()
		e.delivered(j, d)
		return paceSlot
	case errors.Is(err, inbox.ErrRetry):
		e.failed()
		e.logf("delivery to team %s: nothing of the user line was written, retry in %s: %v", j.id, retryWait, err)
		e.unawait(j, d)
		return paceRetry
	}
	e.failed()
	e.logf("delivery to team %s: the user line was cut, its proof settles it: %v", j.id, err)
	return paceRetry
}

// delivered is a full write's store write and the facts it told.
func (e *Engine) delivered(j *job, d dial) {
	h := e.teams[j.id]
	if h != j.h || !eligible(h, nil) {
		return
	}
	ids := d.built.IDs
	h.delivery.sent(d.told, len(ids) == 0, e.cfg.Clock.Boot())
	if len(ids) == 0 {
		return
	}
	now := e.cfg.Clock.WallMS()
	if err := e.changeLocked(j.id, func(_ *held, s *state.Store) (bool, error) {
		i := slices.IndexFunc(s.Awaiting, func(a state.Awaiting) bool { return a.Nonce == d.built.Delivery.Nonce })
		if i < 0 {
			return false, nil
		}
		s.Awaiting[i].Written = state.WrittenYes
		for _, p := range s.Pending {
			if slices.Contains(ids, p.ID) {
				s.AddLedger(state.Ledger{ID: p.ID, Room: p.Room, Item: p.Item, From: p.From, AtMS: now})
			}
		}
		return true, nil
	}); err != nil {
		e.logf("team %s: delivery %s was written, its written yes was not persisted: %v", j.id, d.built.Delivery.Nonce, err)
	}
}

// unawait takes back an entry of which nothing reached the session: its items pending again,
// with no repeat, in one write, and every wait woken.
func (e *Engine) unawait(j *job, d dial) {
	if len(d.built.IDs) == 0 {
		return
	}
	nonce := d.built.Delivery.Nonce
	if err := e.changeLocked(j.id, func(_ *held, s *state.Store) (bool, error) {
		s.Awaiting = slices.DeleteFunc(s.Awaiting, func(a state.Awaiting) bool { return a.Nonce == nonce })
		for i := range s.Pending {
			if h := s.Pending[i].Handed; h != nil && *h == nonce {
				s.Pending[i].Handed = nil
			}
		}
		return true, nil
	}); err != nil {
		e.logf("team %s: delivery %s was not written and its entry stays unknown: %v", j.id, nonce, err)
		return
	}
	e.wake()
}

// sendMember is a membership notice's turn: rendered with a fresh nonce, dialled and written.
func (e *Engine) sendMember(ctx context.Context, req memberReq) pace {
	var (
		live   bool
		target inbox.Target
	)
	if err := e.do(ctx, func() {
		live, target = e.liveLocked(), inbox.Target{Socket: e.sess.socket, Token: e.sess.token}
	}); err != nil {
		req.answer(false, err)
		return paceNone
	}
	if !live {
		req.answer(false, nil)
		return paceNone
	}
	nonce, err := newNonce(nil)
	text := ""
	if err == nil {
		text, err = req.m.Render(nonce)
	}
	if err != nil {
		req.answer(false, err)
		return paceNone
	}
	conn, closeConn, p := e.connect(ctx, target)
	if conn == nil {
		req.answer(false, nil)
		return p
	}
	defer closeConn()
	err = conn.WriteUser(text)
	if doErr := e.do(ctx, func() {
		if err == nil {
			e.wrote()
			return
		}
		e.failed()
		e.logf("delivery: the membership notice was not written, retry in %s: %v", retryWait, err)
	}); doErr != nil {
		req.answer(false, doErr)
		return paceRetry
	}
	if err != nil {
		req.answer(false, nil)
		return paceRetry
	}
	if ferr := conn.Finish(); ferr != nil {
		e.logf("delivery: the membership notice was written, its connection did not finish: %v", ferr)
	}
	req.answer(true, nil)
	return paceSlot
}
