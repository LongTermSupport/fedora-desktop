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

// PipedGiveUp is the bound on silence of input a command must not wait for ever on: a verb's
// text, and each piped answer to a prompt (internal/ui).
const PipedGiveUp = time.Minute

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
	w, err := o.watch(r)
	if err != nil {
		return nil, err
	}
	done := make(chan result, 1)
	go w.readAll(done)
	return o.wait(w, done)
}

// Copy is Read for input shown as it arrives (a child's live output, such as a sign-in URL
// before the token that follows it): the same refusal, limit, notice and bound on silence, but
// each piece is written to dst as it comes rather than once the input has ended.
func Copy(dst io.Writer, r io.Reader, o Options) (int64, error) {
	o = o.withDefaults()
	w, err := o.watch(r)
	if err != nil {
		return 0, err
	}
	done := make(chan copyResult, 1)
	go func() {
		n, err := io.Copy(dst, w)
		done <- copyResult{n, err}
	}()
	if _, err := Await(o, w.started, nil); err != nil {
		return 0, err
	}
	res := <-done
	if res.err != nil {
		return res.n, fmt.Errorf("read stdin: %w", res.err)
	}
	return res.n, nil
}

// copyResult is the background copy's answer.
type copyResult struct {
	n   int64
	err error
}

// watch is r made ready to read by the rules in the package doc: a terminal refused when asked,
// the limit applied, and the first byte (or the end) watched for.
func (o Options) watch(r io.Reader) (*watched, error) {
	if o.RefuseTerminal && o.IsTerminal(r) {
		return nil, ErrTerminal
	}
	src := r
	if o.Limit > 0 {
		src = io.LimitReader(r, o.Limit)
	}
	return &watched{r: src, started: make(chan struct{}), onStart: o.onStart}, nil
}

// result is the background read's answer.
type result struct {
	data []byte
	err  error
}

// wait is the read's answer: Await on the first byte (or the end), then the input, read to its
// end however slowly it comes.
func (o Options) wait(w *watched, done <-chan result) ([]byte, error) {
	if _, err := Await(o, w.started, nil); err != nil {
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

// ErrStopped is an Await its stop channel ended.
var ErrStopped = errors.New("stopped waiting for input on stdin")

// Await is the one wait for input on stdin, shared by Read and the prompt seam (internal/ui,
// whose piped answers it bounds a line at a time): what arrived first returned as it came. Once
// nothing has arrived for NoticeAfter it says on o.Err that it is waiting and how to end the
// input; with o.GiveUpAfter it refuses once nothing has arrived for that long (*NoInputError);
// a receive from stop (nil: never) ends it with ErrStopped.
func Await[T any](o Options, arrived <-chan T, stop <-chan struct{}) (T, error) {
	o = o.withDefaults()
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
	var zero T
	for {
		select {
		case v := <-arrived:
			return v, nil
		case <-stop:
			return zero, ErrStopped
		case <-notice:
			notice = nil
			o.say()
		case <-giveUp:
			// Input that arrived by the bound wins over it: select picks at random among
			// cases ready together.
			select {
			case v := <-arrived:
				return v, nil
			default:
				return zero, &NoInputError{After: o.GiveUpAfter}
			}
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
