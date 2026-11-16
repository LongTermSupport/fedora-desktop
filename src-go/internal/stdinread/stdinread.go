// Package stdinread is the one reader of a process's stdin for a command that reads input from
// it (issue #260; project_stdin_seam holds every other read of stdin to it). A read of stdin
// returns at end of input, and an open pipe that nothing writes to and nothing closes, which is
// what an agent's shell gives a command run with no input, never ends: the command waits for ever
// and says nothing. This reader says on stderr, once nothing has arrived for NoticeAfter, that
// it is waiting and how to end the input; a caller that must not wait for ever gives it a bound
// (GiveUpAfter), after which a read nothing has arrived on is refused; and a caller whose input
// is never typed refuses a terminal before reading (RefuseTerminal). The bound is on silence: once
// input has started it is read to its end, however slowly it comes.
//
// The pattern is the Python reader's, helpers/stdin_text, which pingbus and the helpers use.
package stdinread

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// NoticeAfter is how long nothing may arrive before the reader says it is waiting.
const NoticeAfter = time.Second

// Clock is the one clock call the reader needs: a channel that receives once d has passed, and
// its stop. internal/teamchat/clock's clocks are one, and System is its real one.
type Clock interface {
	Hold(d time.Duration) (<-chan time.Time, func() bool)
}

// ErrTerminal is a read refused because stdin is a terminal (Options.RefuseTerminal).
var ErrTerminal = errors.New("stdin is a terminal; give the input with a pipe or a file (< FILE)")

// NoInputError is a read given up on: nothing arrived within After (Options.GiveUpAfter).
type NoInputError struct{ After time.Duration }

func (e *NoInputError) Error() string {
	return "no input arrived on stdin within " + strconv.FormatFloat(e.After.Seconds(), 'g', -1, 64) + " s"
}

// Options is one read. Only Prog is usually set; the zero values are the defaults.
type Options struct {
	// Prog names the command in the notice ("teamchat say").
	Prog string
	// Err is where the notice goes; nil is os.Stderr.
	Err io.Writer
	// Limit is the most bytes read; 0 reads to the end. A caller with a cap reads one byte past
	// it, so a read that reaches Limit is over the cap.
	Limit int64
	// GiveUpAfter refuses a read nothing has arrived on after this long; 0 waits for ever (the
	// notice still comes).
	GiveUpAfter time.Duration
	// RefuseTerminal refuses a terminal before any read: for input that is never typed.
	RefuseTerminal bool
	// Clock times the notice and the give-up; nil is System().
	Clock Clock
	// IsTerminal is the terminal test; nil is term.FileIsTerminal.
	IsTerminal func(any) bool

	// onStart is called once the first byte, or the end, has arrived (the package's tests).
	onStart func()
}

// Read is stdin's input, at most o.Limit bytes, read by the rules in the package doc.
func Read(r io.Reader, o Options) ([]byte, error) {
	o = o.withDefaults()
	if o.RefuseTerminal && o.IsTerminal(r) {
		return nil, ErrTerminal
	}
	src := r
	if o.Limit > 0 {
		src = io.LimitReader(r, o.Limit)
	}
	w := &watched{r: src, started: make(chan struct{}), onStart: o.onStart}
	done := make(chan result, 1)
	go w.readAll(done)
	return o.wait(w, done)
}

// result is the background read's answer.
type result struct {
	data []byte
	err  error
}

// wait is the read's answer: the notice once nothing has arrived for NoticeAfter, the refusal
// once nothing has arrived for GiveUpAfter, and otherwise the input, read to its end.
func (o Options) wait(w *watched, done <-chan result) ([]byte, error) {
	notice, stopNotice := o.Clock.Hold(NoticeAfter)
	defer stopNotice()
	var giveUp <-chan time.Time
	if o.GiveUpAfter > 0 {
		c, stop := o.Clock.Hold(o.GiveUpAfter)
		defer stop()
		giveUp = c
	}
	started := w.started
	for {
		select {
		case res := <-done:
			if res.err != nil {
				return nil, fmt.Errorf("read stdin: %w", res.err)
			}
			return res.data, nil
		case <-started:
			notice, giveUp, started = nil, nil, nil
		case <-notice:
			notice = nil
			o.say()
		case <-giveUp:
			// The background read stays blocked on the pipe until it ends or the process does;
			// a command refused here exits, which ends it.
			return nil, &NoInputError{After: o.GiveUpAfter}
		}
	}
}

// say is the notice that the read is waiting. A stderr that cannot be written has nowhere left to
// say so; the wait goes on either way.
func (o Options) say() {
	prefix := ""
	if o.Prog != "" {
		prefix = o.Prog + ": "
	}
	_, err := io.WriteString(o.Err, prefix+"waiting for input on stdin, which has not ended; pipe it in, or end it (ctrl-D, or < /dev/null for none)\n")
	if err != nil {
		return
	}
}

// withDefaults fills the zero values.
func (o Options) withDefaults() Options {
	if o.Err == nil {
		o.Err = os.Stderr
	}
	if o.Clock == nil {
		o.Clock = System()
	}
	if o.IsTerminal == nil {
		o.IsTerminal = term.FileIsTerminal
	}
	return o
}

// watched is the reader, which closes started on the first byte or the end.
type watched struct {
	r       io.Reader
	started chan struct{}
	once    sync.Once
	onStart func()
}

func (w *watched) Read(p []byte) (int, error) {
	n, err := w.r.Read(p)
	if n > 0 || err != nil {
		w.once.Do(func() {
			close(w.started)
			if w.onStart != nil {
				w.onStart()
			}
		})
	}
	return n, err
}

// readAll reads to the end and sends the answer on a channel with room for it, so it never
// blocks once the read ends.
func (w *watched) readAll(done chan<- result) {
	data, err := io.ReadAll(w)
	done <- result{data, err}
}

// System is the real clock: teamchat's, whose Hold is the one real timer.
func System() Clock { return clock.System() }
