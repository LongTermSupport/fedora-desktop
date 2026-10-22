package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// retryHold is how long a send that failed transiently waits before it is tried again, unless
// a newer sync comes first (n3d-D1).
const retryHold = 30 * time.Second

// recoveryPage is how many of main's latest events a recovery reads for its tried sends; one
// page serves every entry (n3d-D4).
const recoveryPage = 100

const (
	msgLoginRefused exits.Template = "team {}'s homeserver refused this member's login at the send; tell a human"
	msgForbidden    exits.Template = "team {}'s homeserver forbids this member's send; tell a human"
)

// sendHold is why the sender waits with sends queued: it posts again once a sync newer than
// afterSync was answered, or, after a transient failure, once its timer fired.
type sendHold struct {
	afterSync int64
	timer     <-chan time.Time
	stop      func() bool
	fired     bool
}

func (s *sendHold) release() {
	if s != nil && s.stop != nil {
		s.stop()
	}
}

// nextPost is the oldest send and what the sender posts it with, read on the state goroutine.
type nextPost struct {
	id         paths.TeamID
	entry      state.Sending
	tm         team.Team
	main, self string
	server     string
	cached     *proto.Roster
	lookup     storeLookup
}

// The sender's readings of its team: a send to post, nothing to post yet, a hold to set (the
// tried mark was not written), or the team gone.
const (
	sendIdle = iota
	sendGo
	sendRetry
	sendGone
)

// sendLoop is a team's sender (SD3, n3d-D1): it posts the team's sends oldest first, one at a
// time, off the state goroutine, until ctx ends. It posts nothing before the run's recovery,
// while the team is down or stopped, or while a hold stands; a poke (a charge, a health, the
// recovery) makes it look again.
func (e *Engine) sendLoop(ctx context.Context, h *held) {
	var hold *sendHold
	defer func() { hold.release() }()
	for {
		var (
			next    nextPost
			reading int
		)
		if err := e.do(ctx, func() { next, reading, hold = e.nextSend(h, hold) }); err != nil || reading == sendGone {
			return
		}
		if reading == sendGo {
			hold.release()
			hold = e.post(ctx, h, next)
			continue
		}
		var timer <-chan time.Time
		if hold != nil {
			timer = hold.timer
		}
		select {
		case <-h.poke:
		case <-timer:
			hold.fired, hold.timer = true, nil
		case <-ctx.Done():
			return
		}
	}
}

// nextSend is the sender's reading on the state goroutine, and its hold: the oldest send,
// marked tried in a store write before its PUT (n3d-D4).
func (e *Engine) nextSend(h *held, hold *sendHold) (nextPost, int, *sendHold) {
	if h.removed || e.teams[h.id] != h {
		return nextPost{}, sendGone, hold
	}
	if !h.mayPost(hold) {
		return nextPost{}, sendIdle, hold
	}
	entry := h.store.Outbox.Sending[0]
	if !entry.Tried {
		if err := e.changeLocked(h.id, func(_ *held, s *state.Store) (bool, error) {
			i := sendingIndex(s, entry.Txn)
			if i < 0 {
				return false, nil
			}
			s.Outbox.Sending[i].Tried = true
			return true, nil
		}); err != nil {
			e.logf("team %s: a send was not marked tried, so it waits: %v", h.id, err)
			hold.release()
			return nextPost{}, sendRetry, e.holdLocked(h, true)
		}
		entry.Tried = true
	}
	p := nextPost{
		id: h.id, entry: entry, tm: h.team, main: h.anchors.MainRoom, self: h.store.UserID,
		server: h.anchors.Server, lookup: lookupOf(h),
	}
	if h.roster != nil {
		r := *h.roster
		p.cached = &r
	}
	return p, sendGo, hold
}

// mayPost is whether the sender posts now: the run's recovery done, a send waiting, the team
// up and running, and no hold standing (a newer sync, or a fired timer, lifts one).
func (h *held) mayPost(hold *sendHold) bool {
	hl := h.health
	switch {
	case !h.recovered || h.team == nil || h.store == nil || len(h.store.Outbox.Sending) == 0:
		return false
	case hl.Down || hl.Stopped != "":
		return false
	}
	return hold == nil || hold.fired || hl.LastSyncOKMS > hold.afterSync
}

// holdLocked is a hold from now: past the last sync answered, and for a transient failure
// also retryHold; on the state goroutine, so no sync can slip between the failure and it.
func (e *Engine) holdLocked(h *held, transient bool) *sendHold {
	s := &sendHold{afterSync: h.health.LastSyncOKMS}
	if transient {
		s.timer, s.stop = e.cfg.Clock.Hold(retryHold)
	}
	return s
}

// hold is holdLocked for a sender off the state goroutine; nil once the engine or ctx ended.
func (e *Engine) hold(ctx context.Context, h *held, transient bool) *sendHold {
	var s *sendHold
	if err := e.do(ctx, func() { s = e.holdLocked(h, transient) }); err != nil {
		return nil
	}
	return s
}

func sendingIndex(s *state.Store, txn string) int {
	return slices.IndexFunc(s.Outbox.Sending, func(x state.Sending) bool { return x.Txn == txn })
}

// post is one PUT of p's send by its txn, after the re-check against the current roster
// (n3d-D27), and what follows it: the hold the sender keeps, nil for none.
func (e *Engine) post(ctx context.Context, h *held, p nextPost) *sendHold {
	n := p.tm.Notices()
	r, ok := n.Roster()
	switch {
	case ok:
	case p.cached != nil:
		r = *p.cached
	default:
		return e.hold(ctx, h, true)
	}
	if err := e.recheck(p, n.Token(), r); err != nil {
		return e.unposted(ctx, h, p.entry, err)
	}
	id, err := n.Send(ctx, p.main, p.entry.Type, p.entry.Txn, p.entry.Content)
	if err != nil {
		return e.failed(ctx, h, p.entry, err)
	}
	failpoint.Hit(failpoint.LinkAfterSendBeforeRecord)
	// The resolver knows the send as this member's before the op that made it is answered.
	n.RecordOwnSend(id, ownSend(p.self, p.main, p.entry.Content, r))
	var recErr error
	if err := e.do(ctx, func() { recErr = e.recordPosted(p.id, p.entry, id, e.cfg.Clock.WallMS()) }); err != nil {
		return nil
	}
	if recErr != nil {
		e.logf("team %s: a posted send was not recorded; it is put again by its txn: %v", p.id, recErr)
		return e.hold(ctx, h, true)
	}
	return nil
}

// recheck composes the queued content again with roster r, so a send the team no longer
// allows (an addressee parked or gone, a role changed) is refused at its post.
func (e *Engine) recheck(p nextPost, token string, r proto.Roster) error {
	v, err := proto.Parse(p.entry.Content)
	if err != nil {
		return fmt.Errorf("engine: a queued send does not parse: %w", err)
	}
	dc, reason := proto.DecodeContent(v, proto.MainKeys)
	if reason != "" {
		return &proto.RefusalError{Reason: reason, Detail: "the queued content does not decode"}
	}
	c := proto.Composer{
		Server: p.server, Self: p.self, Token: token, Roster: r, Lookup: p.lookup.with(r), NowMs: e.cfg.Clock.WallMS(),
	}
	if dc.Key == proto.KeyPing {
		answers := ""
		if dc.Relation.IsAnswer() {
			answers = dc.Relation.Root
		}
		_, err = c.Ping(dc.Ping, answers, dc.Body)
		return err
	}
	_, err = c.Text(dc.Text, dc.Relation.InReplyTo, dc.Body)
	return err
}

// unposted removes a send the re-check refused: logged, counted, and its waiting op answered
// with the refusal.
func (e *Engine) unposted(ctx context.Context, h *held, entry state.Sending, why error) *sendHold {
	reason := "it does not compose"
	if re, ok := errors.AsType[*proto.RefusalError](why); ok {
		reason = re.Reason
	}
	var s *sendHold
	if err := e.do(ctx, func() {
		s = e.dropSend(h, entry, sendResult{err: refusal(h.id, why)}, "a queued "+entry.Kind+" was not posted: "+reason)
	}); err != nil {
		return nil
	}
	return s
}

// dropSend removes entry unposted, logs why and answers its waiting op with r; a store that is
// not written keeps it, behind a transient hold. On the state goroutine.
func (e *Engine) dropSend(h *held, entry state.Sending, r sendResult, why string) *sendHold {
	if err := e.changeLocked(h.id, func(_ *held, s *state.Store) (bool, error) {
		i := sendingIndex(s, entry.Txn)
		if i < 0 {
			return false, nil
		}
		s.Outbox.Sending = slices.Delete(s.Outbox.Sending, i, i+1)
		return true, nil
	}); err != nil {
		e.logf("team %s: an unsent send was not removed: %v", h.id, err)
		return e.holdLocked(h, true)
	}
	e.logf("team %s: %s", h.id, why)
	h.unsent++
	e.answer(h, entry.Txn, r)
	e.publish()
	return nil
}

// answer gives a waiting op its result, once.
func (*Engine) answer(h *held, txn string, r sendResult) {
	if wait, ok := h.waiting[txn]; ok {
		wait <- r
		delete(h.waiting, txn)
	}
}

// failed is a PUT that failed (SD3-8, the offline rule): a refused token or a forbidden send
// answers and removes only the sends an op waits on, the queued ones kept until the team runs
// again; another refusal drops the send; any other failure is tried again later.
func (e *Engine) failed(ctx context.Context, h *held, entry state.Sending, err error) *sendHold {
	code, msg := "", exits.Template("")
	switch {
	case errors.Is(err, matrix.ErrAuthRefused):
		code, msg = exits.CodeToken, msgLoginRefused
	case errors.Is(err, matrix.ErrForbidden):
		code, msg = exits.CodeForbidden, msgForbidden
	}
	if code != "" {
		var s *sendHold
		if doErr := e.do(ctx, func() {
			e.refuseWaiting(h, exits.New(code, &h.id, msg, exits.Team(h.id)))
			s = e.holdLocked(h, false)
		}); doErr != nil {
			return nil
		}
		e.logf("team %s: a send was refused (%s); the queued sends wait for the team to run again", h.id, code)
		return s
	}
	if me, ok := errors.AsType[*matrix.Error](err); ok && !me.Transient() {
		var s *sendHold
		if doErr := e.do(ctx, func() {
			s = e.dropSend(h, entry, sendResult{err: exits.New(exits.CodeInternal, &h.id, msgServerRefused)},
				fmt.Sprintf("the homeserver refused a %s, which is not posted: %v", entry.Kind, me))
		}); doErr != nil {
			return nil
		}
		return s
	}
	if ctx.Err() != nil {
		return nil
	}
	e.logf("team %s: a send was not answered; it is tried again by its txn: %v", h.id, err)
	return e.hold(ctx, h, true)
}

// refuseWaiting removes every send an op waits on and answers each with refused, in one store
// write; on the state goroutine. The queued sends no op waits on stay (the offline rule).
func (e *Engine) refuseWaiting(h *held, refused error) {
	var gone []string
	if err := e.changeLocked(h.id, func(_ *held, s *state.Store) (bool, error) {
		s.Outbox.Sending = slices.DeleteFunc(s.Outbox.Sending, func(x state.Sending) bool {
			if _, ok := h.waiting[x.Txn]; ok {
				gone = append(gone, x.Txn)
				return true
			}
			return false
		})
		return len(gone) > 0, nil
	}); err != nil {
		e.logf("team %s: the refused sends were not removed: %v", h.id, err)
		return
	}
	for _, txn := range gone {
		e.answer(h, txn, sendResult{err: refused})
	}
}

// recordPosted is a posted send in one store write: its entry removed, an answer-expecting
// ping added to the outbox (sent at sentMS, due its timeout later), expired pings pruned; then
// the TIMEOUT timer re-armed and the waiting op answered with id. On the state goroutine.
func (e *Engine) recordPosted(t paths.TeamID, entry state.Sending, id string, sentMS int64) error {
	var pruned []string
	now := e.cfg.Clock.WallMS()
	if err := e.changeLocked(t, func(_ *held, s *state.Store) (bool, error) {
		i := sendingIndex(s, entry.Txn)
		if i < 0 {
			return false, nil
		}
		s.Outbox.Sending = slices.Delete(s.Outbox.Sending, i, i+1)
		if p := entry.Ping; p != nil {
			if err := s.AddPing(state.Ping{
				ID: id, Verb: p.Verb, Ref: p.Ref, To: slices.Clone(p.To), SentMS: sentMS, DueMS: sentMS + p.TimeoutMS,
				Answered: []int{}, Reported: []int{},
			}); err != nil {
				return false, err
			}
		}
		pruned = pruneOutbox(s, now)
		return true, nil
	}); err != nil {
		return err
	}
	h := e.teams[t]
	h.forget(pruned)
	e.arm()
	e.answer(h, entry.Txn, sendResult{id: id})
	return nil
}

// ownSend is a posted send as the resolver's own-sends map holds it (SY4a): the flags the
// lookup gives this member's outbox pings.
func ownSend(self, main string, content json.RawMessage, r proto.Roster) proto.Related {
	rel := proto.Related{Sender: self, RoomID: main}
	v, err := proto.Parse(content)
	if err != nil {
		return rel
	}
	if dc := decoded(v); dc.Key == proto.KeyPing {
		rel.Verb = dc.Ping.Verb
	}
	rel.AddressedToRosterAgent = slices.ContainsFunc(contentTo(v), r.HumanReader)
	return rel
}

// recovery is what RecoverSends reads of its team on the state goroutine.
type recovery struct {
	h          *held
	main, self string
	cached     *proto.Roster
	tried      []state.Sending
}

// recovered is a tried send found among main's events: its event id and origin_server_ts.
type recovered struct {
	entry  state.Sending
	id     string
	origin int64
}

// RecoverSends finds a restarted link's tried sends among its own events in main (n3d-D4),
// with one read of main's latest page for all of them: a send the server holds is recorded as
// posted at its origin time, never put again. Then the sender may post. A read that fails is
// returned, and the team calls again; nothing is posted until a recovery is done.
func (e *Engine) RecoverSends(ctx context.Context, t paths.TeamID, n team.Notices) error {
	var (
		rc      recovery
		heldErr error
	)
	if err := e.do(ctx, func() { rc, heldErr = e.recovery(t) }); err != nil {
		return err
	}
	if heldErr != nil {
		return heldErr
	}
	if len(rc.tried) > 0 {
		if err := e.recoverTried(ctx, t, n, rc); err != nil {
			return err
		}
	}
	return e.do(ctx, func() {
		if h := e.teams[t]; h == rc.h && !h.removed {
			h.recovered = true
			h.pokeSender()
		}
	})
}

// recoverTried is the one read of main and each tried send found in it recorded as posted.
func (e *Engine) recoverTried(ctx context.Context, t paths.TeamID, n team.Notices, rc recovery) error {
	page, err := n.Messages(ctx, rc.main, matrix.MessagesRequest{Dir: matrix.Backward, Limit: recoveryPage})
	if err != nil {
		return fmt.Errorf("engine: team %s: read main for the tried sends: %w", t, err)
	}
	r, ok := n.Roster()
	if !ok && rc.cached != nil {
		r = *rc.cached
	}
	for _, f := range matchTried(page.Chunk, rc.tried, rc.self) {
		var recErr error
		if err := e.do(ctx, func() { recErr = e.recordPosted(t, f.entry, f.id, f.origin) }); err != nil {
			return err
		}
		if recErr != nil {
			return recErr
		}
		n.RecordOwnSend(f.id, ownSend(rc.self, rc.main, f.entry.Content, r))
	}
	return nil
}

func (e *Engine) recovery(t paths.TeamID) (recovery, error) {
	h, err := e.heldFor(t)
	if err != nil {
		return recovery{}, err
	}
	rc := recovery{h: h, main: h.anchors.MainRoom, self: h.userID}
	if h.roster != nil {
		r := *h.roster
		rc.cached = &r
	}
	if h.store != nil {
		rc.self = h.store.UserID
		for _, s := range h.store.Outbox.Sending {
			if s.Tried {
				rc.tried = append(rc.tried, s)
			}
		}
	}
	return rc, nil
}

// matchTried pairs each tried send with an event of chunk that is its: sent by self, a
// message, content equal as JSON (whatever its key order), at or after the charge. Each event
// and each send pairs at most once.
func matchTried(chunk []json.RawMessage, tried []state.Sending, self string) []recovered {
	want := make([]*proto.Value, len(tried))
	for i, s := range tried {
		if v, err := proto.Parse(s.Content); err == nil {
			want[i] = &v
		}
	}
	var out []recovered
	for _, raw := range chunk {
		ev, err := proto.Parse(raw)
		if err != nil {
			continue // not an event this member sent
		}
		content, okContent := ev.Get("content")
		id, ts := field(ev, "event_id").Str(), field(ev, "origin_server_ts").Int()
		if !okContent || id == "" || field(ev, "sender").Str() != self || field(ev, "type").Str() != proto.MessageType {
			continue
		}
		for i, s := range tried {
			if want[i] != nil && ts >= s.SinceMS && content.Equal(*want[i]) {
				out = append(out, recovered{entry: s, id: id, origin: ts})
				want[i] = nil
				break
			}
		}
	}
	return out
}

// field is v's member key, the null value when it has none.
func field(v proto.Value, key string) proto.Value {
	if x, ok := v.Get(key); ok {
		return x
	}
	return proto.Null()
}
