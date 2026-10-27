package reclaim

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// maxTries bounds a value question's re-asks and the menu's invalid choices
// (InteractiveScripts.md rule 2).
const maxTries = 3

// errCancelled is the run's context ending (Ctrl-C, or another signal SignalContext caught)
// while a line was awaited (E30).
var errCancelled = errors.New("cancelled")

// lineReader reads the terminal one line at a time on a goroutine of its own, so a wait for a
// line ends when the run's context does. A read a cancel abandoned is never answered: the run
// ends.
type lineReader struct {
	want    chan struct{}
	replies chan lineRead
	pending bool
	// ended is the read that ended the input, answered again to every later wait.
	ended *lineRead
}

type lineRead struct {
	line string
	err  error
}

func newLineReader(in io.Reader) *lineReader {
	l := &lineReader{want: make(chan struct{}), replies: make(chan lineRead, 1)}
	go func() {
		r := bufio.NewReader(in)
		for range l.want {
			line, err := r.ReadString('\n')
			l.replies <- lineRead{line: line, err: err}
			if err != nil {
				return
			}
		}
	}()
	return l
}

// next is the next read of the terminal, or errCancelled when ctx ends first.
func (l *lineReader) next(ctx context.Context) (lineRead, error) {
	if l.ended != nil {
		return *l.ended, nil
	}
	if !l.pending {
		l.want <- struct{}{}
		l.pending = true
	}
	select {
	case got := <-l.replies:
		l.pending = false
		if got.err != nil {
			l.ended = &got
		}
		return got, nil
	case <-ctx.Done():
		return lineRead{}, errCancelled
	}
}

// stop ends the reading goroutine once its current read returns.
func (l *lineReader) stop() { close(l.want) }

// readLine is one line typed at the terminal, trimmed; end of input, even after part of a
// line, is io.EOF; the end of ctx is errCancelled.
func (r *run) readLine(ctx context.Context) (string, error) {
	if ctx.Err() != nil {
		return "", errCancelled
	}
	got, err := r.tty.next(ctx)
	if err != nil {
		return "", err
	}
	if errors.Is(got.err, io.EOF) {
		return "", io.EOF
	}
	if got.err != nil {
		return "", fmt.Errorf("read the terminal: %w", got.err)
	}
	return strings.TrimSpace(got.line), nil
}

// prompt is a question on stderr, its text bold, then its hint.
func (r *run) prompt(question, hint string) {
	r.errOut.Print(r.env.Stderr, r.paint(styleBold, question)+" "+hint+" ")
}

// confirm is E15: yes only for y or yes in any case; anything else, end of input and a cancel
// are no.
func (r *run) confirm(ctx context.Context, question string) bool {
	r.prompt(question, "[y/N]")
	reply, err := r.readLine(ctx)
	if err != nil {
		r.endOfAnswer(err)
		return false
	}
	switch strings.ToLower(reply) {
	case "y", "yes":
		return true
	}
	return false
}

// endOfAnswer ends the question's line after a read that gave no answer, naming a read that
// failed; a cancel is said once, by the run (E30).
func (r *run) endOfAnswer(err error) {
	if errors.Is(err, errCancelled) {
		return
	}
	r.say("")
	if !errors.Is(err, io.EOF) {
		r.failf("%v", err)
	}
}

// askValue is E16: the answer, or the default on an empty line, asked again while check
// names what is wrong with it, three tries in all; false when the action is to end.
func (r *run) askValue(ctx context.Context, question, def string, check func(string) string) (string, bool) {
	for range maxTries {
		r.prompt(question, "["+def+"]")
		reply, err := r.readLine(ctx)
		if err != nil {
			r.endOfAnswer(err)
			if !errors.Is(err, errCancelled) {
				r.warn("  Cancelled.")
			}
			return "", false
		}
		if reply == "" {
			reply = def
		}
		wrong := check(reply)
		if wrong == "" {
			return reply, true
		}
		r.errLine("  " + wrong)
	}
	r.warn(fmt.Sprintf("  Giving up on this action after %d tries.", maxTries))
	return "", false
}
