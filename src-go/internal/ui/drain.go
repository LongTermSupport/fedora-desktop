package ui

import (
	"errors"
	"sync"
)

// Drain says which screens start on no key typed before they drew. internal/ttyecho keeps keys
// typed between screens for the next screen to read (type-ahead), and that is the rule; these
// screens discard them instead (execx.Cmd.DrainInput):
//
//   - the screen after one the person left: Esc or Ctrl-C on a gum question, a No on a gum
//     confirm (gum gives Esc the same exit), leaving an fzf screen. A key typed just after Esc
//     would otherwise act on a screen the person has not seen (Plan 00177 Task 12.3).
//   - every screen of a destructive flow, from Destructive until the func it returns is called:
//     a team taken down or its data deleted, a backup restored over it, a session ended.
//
// A Gum and a Picker of one command share one Drain, so a leave on either drains the next screen
// of both; one with no Drain makes its own on first use.
type Drain struct {
	mu          sync.Mutex
	destructive int
	afterLeave  bool
}

// Destructive drains every screen until the returned func is called; calls nest.
func (d *Drain) Destructive() func() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.destructive++
	var once sync.Once
	return func() {
		once.Do(func() {
			d.mu.Lock()
			defer d.mu.Unlock()
			d.destructive--
		})
	}
}

// next is whether the screen about to run drains; it is the one after a leave, so the leave is
// spent.
func (d *Drain) next() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	drain := d.destructive > 0 || d.afterLeave
	d.afterLeave = false
	return drain
}

// left records that the person left the screen that just ended.
func (d *Drain) left() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.afterLeave = true
}

// leftOn records a leave when err is one: a cancelled question, a left picker or menu.
func (d *Drain) leftOn(err error) {
	moved, back := errors.AsType[*BackError](err)
	if (back && moved != nil) || errors.Is(err, ErrCancelled) || errors.Is(err, ErrLeave) || errors.Is(err, ErrBack) {
		d.left()
	}
}

func (g *Gum) drain() *Drain {
	if g.Drain == nil {
		g.Drain = &Drain{}
	}
	return g.Drain
}

func (p *Picker) drain() *Drain {
	if p.Drain == nil {
		p.Drain = &Drain{}
	}
	return p.Drain
}

// Destructive drains every question of a destructive flow until the returned func is called.
func (g *Gum) Destructive() func() { return g.drain().Destructive() }

// Destructive drains every screen of a destructive flow until the returned func is called.
func (p *Picker) Destructive() func() { return p.drain().Destructive() }
