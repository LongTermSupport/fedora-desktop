// Package stdinread is the QA module's twin of src-go/internal/stdinread, which this module
// cannot import: the one reader of a process's stdin for a command that reads input from it
// (issue #260; project_stdin_seam holds every other read of stdin to it). A read of stdin
// returns at end of input, and an open pipe that nothing writes to and nothing closes, which is
// what an agent's shell gives a command run with no input, never ends: the command waits for
// ever and says nothing. This reader says on stderr, once nothing has arrived for NoticeAfter,
// that it is waiting and how to end the input; a caller that must not wait for ever gives it a
// bound (GiveUpAfter), after which a read nothing has arrived on is refused. The bound is on
// silence: once input has started it is read to its end, however slowly it comes.
//
// It holds only what the QA module's commands use: no terminal refusal and no prompt wait.
package stdinread

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"
)

// NoticeAfter is how long nothing may arrive before the reader says it is waiting.
const NoticeAfter = time.Second

// PipedGiveUp is the bound on silence of input a command must not wait for ever on.
const PipedGiveUp = time.Minute

// Clock is the one clock call the reader needs: a channel that receives once d has passed, and
// its stop. System is the real one; stdinreadtest's is a test's.
type Clock interface {
	Hold(d time.Duration) (<-chan time.Time, func() bool)
}

// NoInputError is a read given up on: nothing arrived within After (Options.GiveUpAfter).
type NoInputError struct{ After time.Duration }

func (e *NoInputError) Error() string {
	return "no input arrived on stdin within " + strconv.FormatFloat(e.After.Seconds(), 'g', -1, 64) + " s"
}

// Options is one read; the zero values are the defaults.
type Options struct {
	// Prog names the command in the notice ("qa-push-gate").
	Prog string
	// Err is where the notice goes; nil is os.Stderr.
	Err io.Writer
	// Limit is the most bytes read; 0 reads to the end.
	Limit int64
	// GiveUpAfter refuses a read nothing has arrived on after this long; 0 waits for ever (the
	// notice still comes).
	GiveUpAfter time.Duration
	// Clock times the notice and the give-up; nil is System().
	Clock Clock
}

// Read is stdin's input, at most o.Limit bytes, read by the rules in the package doc.
func Read(stdin io.Reader, o Options) ([]byte, error) {
	if o.Err == nil {
		o.Err = os.Stderr
	}
	if o.Clock == nil {
		o.Clock = System()
	}
	src := stdin
	if o.Limit > 0 {
		src = io.LimitReader(stdin, o.Limit)
	}
	w := &watched{r: src, started: make(chan struct{})}
	done := make(chan result, 1)
	go w.readAll(done)
	if err := o.await(w.started); err != nil {
		// The background read stays blocked on the pipe until it ends or the process does; a
		// command refused here exits, which ends it.
		return nil, err
	}
	res := <-done
	if res.err != nil {
		return nil, fmt.Errorf("read stdin: %w", res.err)
	}
	return res.data, nil
}

// await waits for started: the notice once NoticeAfter has passed, the refusal at GiveUpAfter.
func (o Options) await(started <-chan struct{}) error {
	// The bound is armed before the notice, so once the notice's timer is armed both are: a
	// test that waits for the notice's timer and then moves the clock moves it past both.
	var giveUp <-chan time.Time
	if o.GiveUpAfter > 0 {
		c, stopGiveUp := o.Clock.Hold(o.GiveUpAfter)
		defer stopGiveUp()
		giveUp = c
	}
	notice, stopNotice := o.Clock.Hold(NoticeAfter)
	defer stopNotice()
	for {
		select {
		case <-started:
			return nil
		case <-notice:
			notice = nil
			o.say()
		case <-giveUp:
			// Input that started by the bound wins over it: select picks at random among
			// cases ready together.
			select {
			case <-started:
				return nil
			default:
			}
			// A bound that passed with the notice still pending (both ready together) says
			// the notice first, so the refusal never comes without it.
			if notice != nil && o.GiveUpAfter >= NoticeAfter {
				o.say()
			}
			return &NoInputError{After: o.GiveUpAfter}
		}
	}
}

// say is the notice that the read is waiting. A stderr that cannot be written has nowhere left
// to say so; the wait goes on either way.
func (o Options) say() {
	prefix := ""
	if o.Prog != "" {
		prefix = o.Prog + ": "
	}
	if _, err := io.WriteString(o.Err, prefix+"waiting for input on stdin, which has not ended; pipe it in, or end it (ctrl-D, or < /dev/null for none)\n"); err != nil {
		return
	}
}

// result is the background read's answer.
type result struct {
	data []byte
	err  error
}

// watched is the reader, which closes started on the first byte or the end.
type watched struct {
	r       io.Reader
	started chan struct{}
	once    sync.Once
}

func (w *watched) Read(p []byte) (int, error) {
	n, err := w.r.Read(p)
	if n > 0 || err != nil {
		w.once.Do(func() { close(w.started) })
	}
	return n, err
}

// readAll reads to the end and sends the answer on a channel with room for it.
func (w *watched) readAll(done chan<- result) {
	data, err := io.ReadAll(w)
	done <- result{data, err}
}

// System is the real clock.
func System() Clock { return systemClock{} }

type systemClock struct{}

// Hold implements Clock with a real timer.
func (systemClock) Hold(d time.Duration) (<-chan time.Time, func() bool) {
	t := time.NewTimer(d)
	return t.C, t.Stop
}
