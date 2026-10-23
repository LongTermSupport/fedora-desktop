package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

// The removal's bounds (LN8, n3c-D22): the team goroutine's stop and the floor marker's PUT,
// each at most 2 s, so the answer stays inside the keeper's 10 s whatever the homeserver does.
const (
	stopWait  = 2 * time.Second
	flushWait = 2 * time.Second
)

// held is one team the session holds: its seat, its store and the items' content in memory.
type held struct {
	id     paths.TeamID
	lock   *lock.SeatLock
	writer *state.StoreWriter
	// store is nil until the team's first Load creates it (no store.json yet), and while
	// refused.
	store   *state.Store
	refused bool
	// userID and anchors are seat.json's, "" while it could not be read.
	userID  string
	anchors proto.Anchors
	content map[string]proto.Verdict
	team    team.Team
	cancel  context.CancelFunc
	stopped chan struct{}
	health  team.Health
	counts  team.Counts
	removed bool
	// delivery is what the delivery loop holds of the team: its trailing facts and backoff.
	delivery deliveryState
	// receipts is closed at the next change of the store's receipts; nil while none waits.
	receipts chan struct{}

	// shownMissing are the missed nonces the team's last proven delivery cleared from
	// status.json's count (n3e-D11); seenTwice counts the items recv showed before a late
	// find (P9).
	shownMissing map[string]bool
	seenTwice    int64
}

// Removed is a removal's answer: the team's user id (n3c-D13), "" when the link never read
// one, and its notices, for the leave and presence the link posts afterwards from memory.
type Removed struct {
	UserID  string
	Notices team.Notices
}

var errNotHeld = errors.New("engine: the team is not held")

// Add holds team t on the seat lock l (LN1 step 4, a hot add): it loads the store, returns
// each item handed to a recv the last link never saw acknowledged to pending, marked repeat
// and restored (P11), and starts the team goroutine. A store that does not decode is this
// team's failure (status shows it refused), never the link's; so is a seat.json that does not
// read, which the team goroutine reports. An error means nothing is held: l is closed.
func (e *Engine) Add(ctx context.Context, t paths.TeamID, l *lock.SeatLock, removedUserIDs []string) error {
	if l.Seat().Dir() != e.cfg.Layout.Seat(t).Dir() {
		return errors.Join(fmt.Errorf("engine: the seat lock is not team %s's", t), l.Close())
	}
	h := &held{id: t, lock: l, content: map[string]proto.Verdict{}, stopped: make(chan struct{})}
	var addErr error
	if err := e.do(ctx, func() { addErr = e.load(h) }); err != nil {
		return errors.Join(err, l.Close())
	}
	if addErr != nil {
		return errors.Join(addErr, l.Close())
	}
	tm, err := e.cfg.NewTeam(TeamConfig{
		Team: t, Seat: l.Seat(), Run: e.cfg.Run, Lock: l, RemovedUserIDs: removedUserIDs, Engine: e, Ends: e.ends,
		Root: e.cfg.Root, Plain: e.cfg.Plain, FirstSessionStart: e.cfg.FirstSessionStart,
		Clock: e.cfg.Clock, Log: e.cfg.Log, Receipts: e,
	})
	if err != nil {
		e.logf("team %s: no team goroutine: %v", t, err)
	}
	return e.do(ctx, func() { e.start(ctx, h, tm) })
}

// load is Add's local half, on the state goroutine.
func (e *Engine) load(h *held) error {
	if _, ok := e.teams[h.id]; ok {
		return fmt.Errorf("engine: team %s is already held", h.id)
	}
	w, err := state.NewStoreWriter(e.cfg.Root, h.lock)
	if err != nil {
		return fmt.Errorf("engine: team %s: %w", h.id, err)
	}
	h.writer = w
	if b, bundleErr := e.readBundle(h); bundleErr != nil {
		e.logf("team %s: seat.json: %v", h.id, bundleErr)
	} else {
		h.userID = b.UserID
		h.anchors = proto.Anchors{Server: b.ServerName, Team: h.id.String(), MainRoom: b.MainRoom, Self: b.UserID}
	}
	s, err := w.Load()
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		h.refused = true
		h.health.Stopped = "store"
		e.logf("team %s: the store is refused: %v", h.id, err)
	default:
		if h.userID == "" {
			h.userID = s.UserID
		}
		if restoreHanded(&s) > 0 {
			if err := w.Write(s); err != nil {
				return errors.Join(fmt.Errorf("engine: team %s: return the handed items: %w", h.id, err), w.Close())
			}
		}
		h.store = &s
	}
	e.teams[h.id] = h
	return nil
}

// readBundle is seat.json alone; the token is the team goroutine's to read.
func (e *Engine) readBundle(h *held) (seat.Bundle, error) {
	f, err := e.cfg.Root.Read(h.lock.Seat().Bundle(), maxBundle)
	if err != nil {
		return seat.Bundle{}, err
	}
	b, err := seat.DecodeMemberBundle(f.Data)
	if err != nil {
		return seat.Bundle{}, err
	}
	if b.Bundle.Team != h.id {
		return seat.Bundle{}, fmt.Errorf("seat.json is team %s's", b.Bundle.Team)
	}
	return b.Bundle, nil
}

// maxBundle bounds seat.json's read.
const maxBundle = 16 << 10

// restoreHanded returns every handed item to pending, marked repeat and restored (B 2.4.4: a
// link that crashed while items were handed), and is how many it returned. An item awaiting
// proof stays: its delivery's proof settles it.
func restoreHanded(s *state.Store) int {
	n := 0
	awaiting := awaitingIDs(s)
	for i := range s.Pending {
		if s.Pending[i].Handed != nil && !awaiting[s.Pending[i].ID] {
			s.Pending[i].Handed = nil
			s.Pending[i].Repeat = true
			s.Pending[i].Restored = true
			n++
		}
	}
	return n
}

// start runs tm for h, unless the team was removed meanwhile or its store is refused. The team
// goroutine outlives the add's request: its context keeps ctx's values, never its end.
func (e *Engine) start(ctx context.Context, h *held, tm team.Team) {
	h.team = tm
	if tm == nil {
		h.health.Stopped = "bundle"
	}
	if tm == nil || h.refused || h.removed {
		close(h.stopped)
		e.publish()
		return
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	h.cancel = cancel
	go func() {
		defer close(h.stopped)
		if err := tm.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
			e.logf("team %s: the team goroutine ended: %v", h.id, err)
		}
	}()
	e.publish()
}

// Remove is LN8's link half: the team goroutine stopped (its request cancelled), the floor
// marker put within 2 s at best, the team's pending, handed and awaiting items dropped with
// one log line, its store deleted (never seat.lock), and its seat descriptor closed. It
// returns within about 4 s whatever the homeserver does.
func (e *Engine) Remove(ctx context.Context, t paths.TeamID) (Removed, error) {
	var (
		h      *held
		tm     team.Team
		cancel context.CancelFunc
		userID string
	)
	if err := e.do(ctx, func() {
		h = e.teams[t]
		if h != nil {
			h.removed = true
			tm, cancel, userID = h.team, h.cancel, h.userID
		}
	}); err != nil {
		return Removed{}, err
	}
	if h == nil {
		return Removed{}, fmt.Errorf("%w: %s", errNotHeld, t)
	}
	failpoint.Hit(failpoint.LinkInRemove)
	e.stopAndFlush(ctx, t, tm, cancel, h.stopped, "before the removal")
	var notices team.Notices
	if tm != nil {
		notices = tm.Notices()
	}
	var removeErr error
	if err := e.do(ctx, func() { removeErr = e.drop(h) }); err != nil {
		return Removed{}, err
	}
	return Removed{UserID: userID, Notices: notices}, removeErr
}

// stop cancels a team goroutine and waits for it at most stopWait: a team stuck in a call that
// ignores its context is left to end on its own, and its later writes are refused.
func (e *Engine) stop(t paths.TeamID, cancel context.CancelFunc, stopped <-chan struct{}) {
	if cancel != nil {
		cancel()
	}
	timeout, release := e.cfg.Clock.Hold(stopWait)
	defer release()
	select {
	case <-stopped:
	case <-timeout:
		e.logf("team %s: the team goroutine did not stop within %s", t, stopWait)
	}
}

// stopAndFlush stops t's team goroutine and puts its floor marker within flushWait, logging a
// marker not put (when says at what end). The put is bounded by flushWait alone, not by ctx.
func (e *Engine) stopAndFlush(ctx context.Context, t paths.TeamID, tm team.Team, cancel context.CancelFunc, stopped <-chan struct{}, when string) {
	e.stop(t, cancel, stopped)
	if tm == nil {
		return
	}
	fctx, cancelFlush := context.WithTimeout(context.WithoutCancel(ctx), flushWait)
	defer cancelFlush()
	if err := tm.FlushFloor(fctx); err != nil {
		e.logf("team %s: the floor marker was not put %s: %v", t, when, err)
	}
}

// drop is Remove's local half, on the state goroutine.
func (e *Engine) drop(h *held) error {
	if h.store != nil {
		handed := 0
		for _, p := range h.store.Pending {
			if p.Handed != nil {
				handed++
			}
		}
		e.logf("team %s removed: dropped %d pending (%d handed) and %d awaiting items", h.id,
			len(h.store.Pending)-handed, handed, len(h.store.Awaiting))
	}
	err := h.writer.Remove()
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	delete(e.teams, h.id)
	delete(e.sent, h.id)
	h.tellReceipts(nil)
	e.publish()
	return errors.Join(err, h.writer.Close(), h.lock.Close())
}

// Shutdown stops every team goroutine, puts each running team's floor marker (SY10's clean
// end; the teams in parallel, each bounded as a removal's is, so an offline homeserver delays
// the link's exit by about 4 s at most), and closes every store writer and seat descriptor: the
// link's last act before it exits. The 4 s add to the link's time in finish, and the keeper
// kills the link 5 s after End, so on a slow homeserver this put can be killed; the put at the
// next session's start (A2) covers it, and only a seat taken with no store between replays.
func (e *Engine) Shutdown(ctx context.Context) error {
	type running struct {
		id      paths.TeamID
		tm      team.Team
		cancel  context.CancelFunc
		stopped chan struct{}
	}
	var live []running
	if err := e.do(ctx, func() {
		for _, id := range e.order() {
			if h := e.teams[id]; h.cancel != nil && !h.removed {
				live = append(live, running{id: id, tm: h.team, cancel: h.cancel, stopped: h.stopped})
			}
		}
	}); err != nil {
		return err
	}
	var wg sync.WaitGroup
	for _, r := range live {
		wg.Go(func() { e.stopAndFlush(ctx, r.id, r.tm, r.cancel, r.stopped, "at the shutdown") })
	}
	wg.Wait()
	var errs []error
	if err := e.do(ctx, func() {
		for _, h := range e.teams {
			if h.cancel != nil {
				h.cancel()
			}
			errs = append(errs, h.writer.Close(), h.lock.Close())
		}
		clear(e.teams)
	}); err != nil {
		return err
	}
	return errors.Join(errs...)
}

// EndTeams are the held teams with a store, for the ends' End (EN1): each with its notices and
// its store's run.
func (e *Engine) EndTeams(ctx context.Context) ([]team.EndTeam, error) {
	type pending struct {
		tm  team.Team
		end team.EndTeam
	}
	var got []pending
	if err := e.do(ctx, func() {
		for _, id := range e.order() {
			h := e.teams[id]
			if h.store != nil && h.team != nil {
				got = append(got, pending{tm: h.team, end: team.EndTeam{Team: id, Run: h.store.Run}})
			}
		}
	}); err != nil {
		return nil, err
	}
	out := make([]team.EndTeam, 0, len(got))
	for _, g := range got {
		g.end.Notices = g.tm.Notices()
		out = append(out, g.end)
	}
	return out, nil
}
