package engine

import (
	"reflect"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
)

// SL1: status.json is written at most once a second, and once a minute while a team is held,
// so its written_ms tells the status line the link still answers.
const (
	statusGap       = time.Second
	statusHeartbeat = time.Minute
)

// statusPacer is when status.json was last written and what with; the state goroutine's alone.
// One timer is armed at a time: for the owed write, else for the heartbeat.
type statusPacer struct {
	wrote bool
	// last is the status last written, its written_ms left zero; at is that write's boot time.
	last state.Status
	at   time.Duration
	// dirty is a change not yet written, owed a second after the last write.
	dirty bool
	// stop disarms the armed timer, due at due; gen tells a stale fire from the armed one.
	stop func() bool
	due  time.Duration
	gen  uint64
}

// offerStatus takes the newest status: written at once when the last write is a second old or
// there was none, else owed until then.
func (e *Engine) offerStatus(st state.Status) {
	p := &e.statusPace
	switch {
	case p.wrote && reflect.DeepEqual(st, p.last):
		p.dirty = false
	case !p.wrote || e.cfg.Clock.Boot()-p.at >= statusGap:
		e.writeStatus(st)
		return
	default:
		p.dirty = true
	}
	e.armStatus()
}

// flushStatus makes a write still owed now.
func (e *Engine) flushStatus() {
	if e.statusPace.dirty {
		e.writeStatus(e.buildStatus())
	}
}

// writeStatus writes st with a fresh written_ms. A failed write is logged once and not retried
// at once, which would log a line each second; the heartbeat writes it again.
func (e *Engine) writeStatus(st state.Status) {
	p := &e.statusPace
	p.wrote, p.last, p.at, p.dirty = true, st, e.cfg.Clock.Boot(), false
	st.WrittenMS = e.cfg.Clock.WallMS()
	if err := state.Write(e.cfg.RunRoot, e.cfg.Layout.Run(e.cfg.Run).Status(), st); err != nil {
		e.logf("status.json not written: %v", err)
	}
	e.armStatus()
}

// armStatus arms the one timer for what is next due: the owed write, else the heartbeat while
// a team is held, else nothing.
func (e *Engine) armStatus() {
	p := &e.statusPace
	var due time.Duration
	switch {
	case p.dirty:
		due = p.at + statusGap
	case e.holdsTeam():
		due = p.at + statusHeartbeat
	default:
		e.disarmStatus()
		return
	}
	if p.stop != nil && p.due == due {
		return
	}
	e.disarmStatus()
	p.gen++
	gen := p.gen
	p.due = due
	p.stop = e.cfg.Clock.AfterFunc(due-e.cfg.Clock.Boot(), func() { e.onGoroutine(func() { e.statusDue(gen) }) })
}

// onGoroutine runs f on the state goroutine and waits for it, for a timer's fire, which has no
// request's context; an ended goroutine runs nothing.
func (e *Engine) onGoroutine(f func()) {
	finished := make(chan struct{})
	select {
	case e.ops <- func() {
		defer close(finished)
		f()
	}:
		<-finished
	case <-e.done:
	}
}

func (e *Engine) disarmStatus() {
	p := &e.statusPace
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
}

// statusDue is the armed timer's fire: the owed write or the heartbeat, with the status as it
// is now.
func (e *Engine) statusDue(gen uint64) {
	p := &e.statusPace
	if gen != p.gen || p.stop == nil {
		return
	}
	p.stop = nil
	if p.dirty || e.holdsTeam() {
		e.writeStatus(e.buildStatus())
	}
}

// holdsTeam is whether a team is held and not removed; a refused add is not held.
func (e *Engine) holdsTeam() bool {
	for _, h := range e.teams {
		if !h.removed {
			return true
		}
	}
	return false
}
