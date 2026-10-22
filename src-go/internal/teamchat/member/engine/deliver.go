package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/deliver"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// SessionFacts are what the session's hooks and its token message tell the link (DV1): the
// inbox socket, its token and the transcript, and whether the session is idle (its last hook
// was Stop, or SessionStart with no prompt since).
type SessionFacts struct {
	// Socket, Token and Transcript are kept as set before when empty.
	Socket, Token, Transcript string
	// Idle is always taken.
	Idle bool
}

// sessionFacts are the session's facts on the state goroutine; gone is a connect that said
// the socket is gone, until a hook tells a socket again.
type sessionFacts struct {
	socket, token, transcript string
	idle, gone                bool
}

// stall is B 2.7.3's stall: failing since the first failed write after the last full one,
// stalled once every write failed for stallAfter.
type stall struct {
	failing, stalled bool
	since            time.Duration
}

// deliveryState is one team's trailing facts held for its next delivery (answered, held back,
// gap, the stop told), when a delivery of facts alone last went, and until when its restored
// items wait for a re-fetch that failed.
type deliveryState struct {
	answered     []proto.Answered
	heldBack     map[string]int64
	gap          bool
	stopTold     string
	trailingAt   time.Duration
	trailingSent bool
	fetchAfter   time.Duration
}

// hold keeps a persisted batch's trailing facts.
func (d *deliveryState) hold(b team.Batch) {
	d.answered = append(d.answered, b.Answered...)
	for from, n := range b.HeldBack {
		if d.heldBack == nil {
			d.heldBack = map[string]int64{}
		}
		d.heldBack[from] += n
	}
	d.gap = d.gap || b.Gap
}

// stopDue is whether a stopped line is owed: the team is stopped and that stop was not told.
func (d *deliveryState) stopDue(stopped string) bool { return stopped != "" && stopped != d.stopTold }

// factsAt is when the team's trailing facts alone are due as a delivery (n3e-D14: at most once
// per trailingEvery); false with no fact held.
func (d *deliveryState) factsAt(stopped string) (time.Duration, bool) {
	if len(d.answered) == 0 && len(d.heldBack) == 0 && !d.gap && !d.stopDue(stopped) {
		return 0, false
	}
	if !d.trailingSent {
		return 0, true
	}
	return d.trailingAt + trailingEvery, true
}

// told is what one delivery carried of the team's facts, for its full write to take out.
type told struct {
	facts    deliver.Facts
	answered int
	heldBack map[string]int64
	stopped  string
}

// take is the facts for a delivery now; held-back lines sorted by sender.
func (d *deliveryState) take(stopped string, n namer) told {
	t := told{answered: len(d.answered), heldBack: maps.Clone(d.heldBack)}
	t.facts = deliver.Facts{Answered: slices.Clone(d.answered), Gap: d.gap}
	for _, from := range slices.Sorted(maps.Keys(d.heldBack)) {
		t.facts.HeldBack = append(t.facts.HeldBack, proto.HeldBack{From: n.from(from), Count: int(d.heldBack[from])})
	}
	if d.stopDue(stopped) {
		t.facts.Stopped, t.stopped = true, stopped
	}
	return t
}

// sent takes out what a fully written delivery told; one of facts alone starts the minute.
func (d *deliveryState) sent(t told, factsOnly bool, now time.Duration) {
	d.answered = slices.Clone(d.answered[min(t.answered, len(d.answered)):])
	for from, n := range t.heldBack {
		if d.heldBack[from] -= n; d.heldBack[from] <= 0 {
			delete(d.heldBack, from)
		}
	}
	if t.facts.Gap {
		d.gap = false
	}
	if t.stopped != "" {
		d.stopTold = t.stopped
	}
	if factsOnly {
		d.trailingAt, d.trailingSent = now, true
	}
}

// drop forgets facts that do not render, so they never hold the team's deliveries.
func (d *deliveryState) drop(stopped string) {
	d.answered, d.heldBack, d.gap = nil, nil, false
	if stopped != "" {
		d.stopTold = stopped
	}
}

// sender is deliver's Namer over the verified roster: an agent's handle and its role, the
// admin as `admin` twice, a human's localpart with no role.
func (n namer) sender(id string) (string, string) { return n.from(id), n.role(id) }

func (n namer) from(id string) string {
	if id == proto.AdminID(n.server) {
		return proto.RoleAdmin
	}
	if h, ok := proto.AgentHandle(id, n.server); ok {
		return h.String()
	}
	if local, ok := proto.Localpart(id, n.server); ok {
		return local
	}
	return id
}

func (n namer) role(id string) string {
	if id == proto.AdminID(n.server) {
		return proto.RoleAdmin
	}
	return n.roster.Members[id].Role
}

// awaitingIDs are the items of s awaiting a delivery's proof (n3e-6 D1): they stay in pending,
// handed to the delivery's nonce, and no recv, ack, return, redaction or restart moves them.
func awaitingIDs(s *state.Store) map[string]bool {
	out := map[string]bool{}
	if s == nil {
		return out
	}
	for _, a := range s.Awaiting {
		for _, id := range a.IDs {
			out[id] = true
		}
	}
	return out
}

// deliverable is whether item id of s can still be delivered: pending, not handed, not held.
func deliverable(s *state.Store, id string) bool {
	return slices.ContainsFunc(s.Pending, func(p state.Pending) bool { return p.ID == id && p.Handed == nil && !p.Held })
}

// takePending removes ids from s's pending, and is the items removed.
func takePending(s *state.Store, ids []string) []state.Pending {
	var left []state.Pending
	s.Pending = slices.DeleteFunc(s.Pending, func(p state.Pending) bool {
		if slices.Contains(ids, p.ID) {
			left = append(left, p)
			return true
		}
		return false
	})
	return left
}

// deliveryItems are h's deliverable items for deliver.Build, in the store's order: a verdict
// from memory (nil for a restored item not fetched again), a TIMEOUT from its key and the
// outbox. Question is nil (`question: none`): the engine holds no question index yet.
func deliveryItems(h *held) []deliver.Item {
	var out []deliver.Item
	for _, p := range h.store.Pending {
		if p.Handed != nil || p.Held {
			continue
		}
		it := deliver.Item{Pending: p}
		if p.Item == records.KindTimeout {
			it.Timeout = timeoutOf(p, h.store.Outbox.Pings)
		} else if v, ok := h.content[p.ID]; ok {
			it.Verdict = &v
		}
		out = append(out, it)
	}
	return out
}

// timeoutOf is a TIMEOUT's account from its key (proto.TimeoutKey); nil for a key without one,
// which fails its slot.
func timeoutOf(p state.Pending, pings []state.Ping) *deliver.Timeout {
	event, silent, keyed := strings.Cut(p.ID, "~")
	if !keyed {
		return nil
	}
	t := &deliver.Timeout{Silent: silent}
	for _, ping := range pings {
		if ping.ID == event {
			t.Ping = ping
		}
	}
	return t
}

// newNonce is a delivery's nonce, 8 hex from crypto/rand, distinct from every nonce s still
// watches (awaiting and missed); s nil for a membership notice.
func newNonce(s *state.Store) (string, error) {
	for {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", fmt.Errorf("engine: the nonce: %w", err)
		}
		n := hex.EncodeToString(b[:])
		if s == nil || !nonceUsed(s, n) {
			return n, nil
		}
	}
}

func nonceUsed(s *state.Store, n string) bool {
	return slices.ContainsFunc(s.Awaiting, func(a state.Awaiting) bool { return a.Nonce == n }) ||
		slices.ContainsFunc(s.Missed, func(m state.Missed) bool { return m.Nonce == n })
}

// transcriptSize is the transcript's size before a delivery, from an lstat; 0 for a transcript
// not yet created (n3e-D10).
func transcriptSize(path string) (int64, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("engine: the transcript's size: %w", err)
	}
	return fi.Size(), nil
}

// delivers is whether this link delivers to a socket: socket mode with a dialer.
func (e *Engine) delivers() bool {
	return e.cfg.Delivery == state.DeliverySocket && e.cfg.Inbox != nil
}

// liveLocked is Live on the state goroutine.
func (e *Engine) liveLocked() bool {
	s := e.sess
	return e.delivers() && s.socket != "" && s.token != "" && s.transcript != "" && !s.gone
}

// Live is whether delivery is live (DV1): socket mode with a dialer, the socket, its token and
// the transcript all told, and no connect since has found the socket gone. An ended engine is
// not live.
func (e *Engine) Live() bool {
	live := false
	if err := e.do(context.Background(), func() { live = e.liveLocked() }); err != nil {
		return false
	}
	return live
}

// SetSession takes the session's facts (the link calls it on every accepted hook op and token
// message) and pokes the delivery loop. A socket told clears gone.
func (e *Engine) SetSession(ctx context.Context, f SessionFacts) error {
	return e.do(ctx, func() {
		if f.Socket != "" {
			e.sess.socket, e.sess.gone = f.Socket, false
		}
		if f.Token != "" {
			e.sess.token = f.Token
		}
		if f.Transcript != "" {
			e.sess.transcript = f.Transcript
		}
		e.sess.idle = f.Idle
		e.poke()
	})
}

// poke wakes the delivery loop's idle wait; pokes while it works are one.
func (e *Engine) poke() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// failed is a delivery's or notice's failed write, on the state goroutine: the stall starts at
// the first, and is shown once every write failed for stallAfter.
func (e *Engine) failed() {
	now := e.cfg.Clock.Boot()
	if !e.stall.failing {
		e.stall.failing, e.stall.since = true, now
	}
	if !e.stall.stalled && now-e.stall.since >= stallAfter {
		e.stall.stalled = true
		e.publish()
	}
}

// wrote is a full write, on the state goroutine: the stall ends.
func (e *Engine) wrote() {
	e.stall.failing = false
	if e.stall.stalled {
		e.stall.stalled = false
		e.publish()
	}
}

// countRender counts items dropped because a slot of their entry fails (DV2's
// unusable.render), with a link.log line for each, on the state goroutine.
func (e *Engine) countRender(h *held, ids []string) {
	if len(ids) == 0 {
		return
	}
	for _, id := range ids {
		delete(h.content, id)
		e.logf("team %s: item %s is dropped: a slot of its entry does not render (unusable.render)", h.id, id)
	}
	e.unusable.Render += int64(len(ids))
	if e.cfg.OnUnusable != nil {
		e.cfg.OnUnusable(e.unusable)
	}
}

// memberReq is a SendMembership waiting for its turn in the delivery loop.
type memberReq struct {
	m     proto.Membership
	reply chan memberAnswer
}

type memberAnswer struct {
	written bool
	err     error
}

func (r memberReq) answer(written bool, err error) { r.reply <- memberAnswer{written, err} }

// SendMembership is DV6's session-level send: the notice rendered with a fresh nonce and
// written to the session's socket in the delivery loop's turn, sharing its 2 s pace. It is true
// only after a full write; false with no error when the link is not in socket mode or delivery
// is not live, or the socket refused or is gone; an error is a notice that does not render or a
// context that ended. Nothing is persisted (n3e-D7).
func (e *Engine) SendMembership(ctx context.Context, m proto.Membership) (bool, error) {
	live := false
	if err := e.do(ctx, func() { live = e.liveLocked() }); err != nil {
		return false, err
	}
	if !live {
		return false, nil
	}
	req := memberReq{m: m, reply: make(chan memberAnswer, 1)}
	select {
	case e.members <- req:
	case <-ctx.Done():
		return false, ctx.Err()
	case <-e.done:
		return false, ErrStopped
	}
	select {
	case a := <-req.reply:
		return a.written, a.err
	case <-ctx.Done():
		return false, ctx.Err()
	case <-e.done:
		return false, ErrStopped
	}
}
