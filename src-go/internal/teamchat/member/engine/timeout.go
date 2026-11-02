package engine

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// timeoutGraceMS is A 8.3's grace past a ping's deadline, as proto.TimeoutDue counts it: a
// TIMEOUT is due from the deadline plus the grace plus 1 ms.
const timeoutGraceMS = 30_000

// timeoutRetry is how soon a TIMEOUT write the store refused is tried again (n3d-D7).
const timeoutRetry = time.Minute

// arm sets the one TIMEOUT timer for the earliest TIMEOUT any held team owes, or none when
// nothing is owed (never a tick, B 2.6 step 07). It is armed from wall milliseconds, since
// deadlines are server times (CK1), and re-armed after every change that can move it; on the
// state goroutine.
func (e *Engine) arm() {
	if e.timer != nil {
		e.timer()
		e.timer = nil
	}
	at, owed := e.nextTimeout()
	if !owed {
		return
	}
	wait := time.Duration(max(at-e.cfg.Clock.WallMS(), 0)) * time.Millisecond
	e.timer = e.cfg.Clock.AfterFunc(wait, e.fire)
}

// nextTimeout is the wall time the earliest owed TIMEOUT falls due: a ping's first moment past
// its grace with an addressee neither answered nor reported, or a refused team's retry if later.
// A removed or refused store owes nothing.
func (e *Engine) nextTimeout() (int64, bool) {
	var (
		at   int64
		owed bool
	)
	for _, h := range e.teams {
		if !eligible(h, nil) {
			continue
		}
		for _, p := range h.store.Outbox.Pings {
			if settled(p) {
				continue
			}
			due := max(p.DueMS+timeoutGraceMS+1, h.timeoutRetryMS)
			if !owed || due < at {
				at, owed = due, true
			}
		}
	}
	return at, owed
}

// fire is the timer's callback, on the clock's goroutine: the TIMEOUTs due are made on the
// state goroutine.
func (e *Engine) fire() {
	if err := e.do(context.Background(), e.makeTimeouts); err != nil {
		e.logf("the TIMEOUT timer did not run: %v", err)
	}
}

// makeTimeouts writes, per team with any due, every due TIMEOUT and its addressee's reported
// mark in one store write, so a crash makes each once or not at all (n3d-D7). The wall clock is
// read again here, so a timer that fired early on it (a clock stepped back) only re-arms. A
// store that refuses the write is tried again a minute later, logged once until it succeeds.
func (e *Engine) makeTimeouts() {
	now := e.cfg.Clock.WallMS()
	made := false
	for _, id := range e.order() {
		h := e.teams[id]
		if !eligible(h, nil) || h.timeoutRetryMS > now {
			continue
		}
		var (
			n      int
			pruned []string
		)
		err := e.changeLocked(id, func(_ *held, s *state.Store) (bool, error) {
			var err error
			if n, err = owedTimeouts(s, now); err != nil || n == 0 {
				return false, err
			}
			pruned = pruneOutbox(s, now)
			return true, nil
		})
		if err != nil {
			h.timeoutRetryMS = now + timeoutRetry.Milliseconds()
			if !h.timeoutRefused {
				e.logf("team %s: the TIMEOUTs due were not written, tried again every %s: %v", id, timeoutRetry, err)
				h.timeoutRefused = true
			}
			continue
		}
		h.timeoutRetryMS, h.timeoutRefused = 0, false
		h.forget(pruned)
		made = made || n > 0
	}
	if made {
		e.wake()
	}
	e.arm()
}

// owedTimeouts adds to s's pending one TIMEOUT per silent addressee of every ping past its
// grace at now, each addressee marked reported (A 8.3); it is how many it added.
func owedTimeouts(s *state.Store, now int64) (int, error) {
	server := paths.ServerName(s.Team)
	n := 0
	for pi := range s.Outbox.Pings {
		p := &s.Outbox.Pings[pi]
		if !proto.TimeoutDue(p.DueMS, now) {
			continue
		}
		for i, to := range p.To {
			if slices.Contains(p.Answered, i) || slices.Contains(p.Reported, i) {
				continue
			}
			local, ok := proto.Localpart(to, server)
			if !ok {
				return 0, fmt.Errorf("engine: ping %s's addressee %s is not of the team's server", p.ID, to)
			}
			if err := s.AddPending(state.Pending{
				ID: proto.TimeoutKey(p.ID, local), Room: proto.PurposeMain, Item: records.KindTimeout,
				SentMS: p.DueMS + timeoutGraceMS, From: to,
			}); err != nil {
				return 0, fmt.Errorf("engine: team %s: %w", s.Team, err)
			}
			p.Reported = addIndex(p.Reported, i)
			n++
		}
	}
	return n, nil
}
