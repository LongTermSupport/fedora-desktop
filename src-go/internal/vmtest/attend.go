package vmtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The attended-run driver (Plan 00228). `./run.bash --consumer` refuses to run headless: it
// asks questions, so it needs a terminal to type in. The guest's sshd gives it one (`ssh -tt`,
// a pseudo-terminal in the guest), and the harness is the person at it: it watches what the
// run prints and types the answer to each question it knows when that question appears, never
// before (a reply typed early could be read, or flushed, by whatever runs first). A question
// the run must never ask, a password prompt above all, ends the run at once. A question it
// does not know ends the run once the output has stood at it for promptIdle, rather than
// leaving it waiting for the provisioning limit.

// prompt is one question an attended run can ask, by the text it prints, and its answer.
type prompt struct {
	text  string // the prompt as the guest prints it
	reply string // typed in answer
	// refuse is a question the run must never ask: it ends the run, typing nothing.
	refuse bool
}

// halt is why the attendant ended a run itself; the zero value is that it did not.
type halt struct {
	refused string // the refused prompt the run printed
	// unanswered is the last lines of a run that stood at a question no prompt answers.
	unanswered string
}

func (h halt) ended() bool { return h.refused != "" || h.unanswered != "" }

const (
	lineKeep  = 256  // of the line being printed; its end is what makes it prompt-shaped
	tailKeep  = 1024 // of the output, which an unanswered question is reported by
	tailLines = 3
)

// attendant is the writer an attended run's terminal output passes through on its way to the
// transcript: carriage returns dropped (the pseudo-terminal turns every newline into CR LF),
// each known prompt answered once per appearance.
type attendant struct {
	out     io.Writer // the transcript
	keys    io.Writer // the terminal's input
	prompts []prompt
	stop    func() // ends the run
	// wake tells watch the output has stopped at a prompt-shaped line (buffered, one deep).
	wake chan struct{}

	mu   sync.Mutex
	seen []byte // output not yet matched, cut to a tail shorter than the longest prompt
	// line is the output since its last newline or answered prompt, cut to lineKeep.
	line   []byte
	tail   []byte // the output's last tailKeep bytes
	writes int    // writes so far, so watch can tell output came while it waited
	halt   halt
}

// Write implements io.Writer.
func (a *attendant) Write(p []byte) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	clean := bytes.ReplaceAll(p, []byte("\r"), nil)
	if _, err := a.out.Write(clean); err != nil {
		return 0, fmt.Errorf("the transcript: %w", err)
	}
	if a.halt.ended() {
		return len(p), nil
	}
	a.writes++
	a.tail = lastBytes(append(a.tail, clean...), tailKeep)
	a.seen = append(a.seen, clean...)
	answered := false
	for {
		at, pr := a.first()
		if at < 0 {
			break
		}
		a.seen = a.seen[at+len(pr.text):]
		if pr.refuse {
			a.halt.refused, a.seen = pr.text, nil
			a.stop()
			return len(p), nil
		}
		if _, err := io.WriteString(a.keys, pr.reply); err != nil {
			return 0, fmt.Errorf("typing the answer to %q: %w", pr.text, err)
		}
		answered = true
	}
	// After an answer, a.seen is everything printed since it, so the line restarts there.
	if answered {
		a.line = append([]byte(nil), a.seen...)
	} else {
		a.line = append(a.line, clean...)
	}
	if i := bytes.LastIndexByte(a.line, '\n'); i >= 0 {
		a.line = a.line[i+1:]
	}
	a.line = lastBytes(a.line, lineKeep)
	// A prompt wholly in the tail would have matched above, so the tail need hold only the
	// start of one split across writes.
	a.seen = lastBytes(a.seen, a.longest()-1)
	if promptShaped(a.line) {
		select {
		case a.wake <- struct{}{}:
		default:
		}
	}
	return len(p), nil
}

// lastBytes is the last n bytes of b, copied when cut so the dropped head is freed.
func lastBytes(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return append([]byte(nil), b[len(b)-n:]...)
}

// promptShaped is whether a line still being printed reads as a question waiting for its
// answer: it ends, blanks aside, in ':', '?', ']', ')' or '>', as an Ansible pause (a bare
// ':' after its text), a read -p, a [y/N] and a (yes/no) do. Progress does not.
func promptShaped(line []byte) bool {
	t := bytes.TrimRight(line, " \t")
	return len(t) > 0 && strings.IndexByte(":?])>", t[len(t)-1]) >= 0
}

// watch ends the run once its output has stood at a prompt-shaped line for promptIdle with
// nothing printed after it. Output during the wait starts it again, so an unknown question
// ends the run between one and two bounds after it appears. It returns when ctx ends or once
// it has ended the run.
func (a *attendant) watch(ctx context.Context, sleep func(context.Context, time.Duration) error) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.wake:
		}
		a.mu.Lock()
		mark := a.writes
		a.mu.Unlock()
		if sleep(ctx, promptIdle) != nil {
			return
		}
		if a.expire(mark) {
			return
		}
	}
}

// expire ends the run if nothing was written since mark and the output still stands at a
// prompt-shaped line, and says whether the run is over.
func (a *attendant) expire(mark int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.halt.ended() {
		return true
	}
	if a.writes != mark || !promptShaped(a.line) {
		return false
	}
	a.halt.unanswered = lastLines(a.tail, tailLines)
	a.stop()
	return true
}

// lastLines is the last n lines of out that are not blank, trailing blanks dropped.
func lastLines(out []byte, n int) string {
	var keep [][]byte
	for _, l := range slices.Backward(bytes.Split(out, []byte("\n"))) {
		if l = bytes.TrimRight(l, " \t"); len(l) > 0 {
			keep = append(keep, l)
		}
		if len(keep) == n {
			break
		}
	}
	slices.Reverse(keep)
	return string(bytes.Join(keep, []byte("\n")))
}

// first is the earliest prompt in the unmatched output, and where it starts; -1 for none.
func (a *attendant) first() (int, prompt) {
	at, found := -1, prompt{}
	for _, pr := range a.prompts {
		if i := bytes.Index(a.seen, []byte(pr.text)); i >= 0 && (at < 0 || i < at) {
			at, found = i, pr
		}
	}
	return at, found
}

func (a *attendant) longest() int {
	n := 1
	for _, pr := range a.prompts {
		n = max(n, len(pr.text))
	}
	return n
}

// attended runs remote in the guest at a terminal, bounded by provisionLimit, answering
// prompts; its output goes to w. It returns the remote status (124 for the bound, as stream
// does), or why the attendant ended it, and an error only when ssh could not be run at all or
// the CLI itself was cancelled.
func (g *guest) attended(ctx context.Context, remote string, w io.Writer, prompts []prompt) (int, halt, error) {
	bounded, cancel := g.c.bound(ctx, provisionLimit)
	defer cancel()
	// An *os.File is handed to ssh as its stdin itself, so no copying goroutine outlives it
	// waiting for an answer nobody will type.
	keysIn, keysOut, err := os.Pipe()
	if err != nil {
		return 0, halt{}, failf("cannot make the terminal's input: %v", err)
	}
	a := &attendant{out: w, keys: keysOut, prompts: prompts, stop: cancel, wake: make(chan struct{}, 1)}
	watchCtx, endWatch := context.WithCancel(bounded)
	watched := make(chan struct{})
	go func() {
		a.watch(watchCtx, g.c.env.Sleep)
		close(watched)
	}()
	cmd := execx.Cmd{
		Name:  sshBin,
		Args:  append([]string{"-tt", "-e", "none", "-o", "ServerAliveInterval=30"}, g.sshArgs(nil, remote)...),
		Stdin: keysIn, Stdout: a, Stderr: a,
	}
	_, runErr := g.c.env.Runner.Run(bounded, cmd)
	endWatch()
	<-watched
	if closeErr := errors.Join(keysIn.Close(), keysOut.Close()); closeErr != nil {
		return 0, halt{}, failf("cannot close the terminal's input: %v", closeErr)
	}
	a.mu.Lock()
	h := a.halt
	a.mu.Unlock()
	if h.ended() {
		return 0, h, nil
	}
	rc, err := statusOf(ctx, bounded, runErr)
	return rc, halt{}, err
}
