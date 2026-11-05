// Package pollpure is project_poll_pure's fixture: its tests hand leakcheck.Poll and
// leakcheck.Drive conditions that read and conditions that act.
package pollpure

import "time"

// Fake is a stub of a fake clock: it moves only when a test moves it.
type Fake struct{ now time.Duration }

// Advance moves the clock by d.
func (f *Fake) Advance(d time.Duration) { f.now += d }

// Suspend moves the clock without firing what falls due.
func (f *Fake) Suspend(d time.Duration) { f.now += d }

// StepWall moves the wall clock alone.
func (f *Fake) StepWall(ms int64) { f.now += time.Duration(ms) * time.Millisecond }

// Waiters is how many waiters are armed: a read.
func (f *Fake) Waiters() int { return 0 }

// Other has an Advance that is not a fake clock's.
type Other struct{}

// Advance is not a clock's.
func (Other) Advance(time.Duration) {}
