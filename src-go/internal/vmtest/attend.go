package vmtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The attended-run driver (Plan 00228). `./run.bash --consumer` refuses to run headless: it
// asks questions, so it needs a terminal to type in. The guest's sshd gives it one (`ssh -tt`,
// a pseudo-terminal in the guest), and the harness is the person at it: it watches what the
// run prints and types the answer to each question it knows when that question appears, never
// before (a reply typed early could be read, or flushed, by whatever runs first). A question
// the run must never ask, a password prompt above all, ends the run at once rather than leaving
// it waiting for the provisioning limit.

// prompt is one question an attended run can ask, by the text it prints, and its answer.
type prompt struct {
	text  string // the prompt as the guest prints it
	reply string // typed in answer
	// refuse is a question the run must never ask: it ends the run, typing nothing.
	refuse bool
}

// attendant is the writer an attended run's terminal output passes through on its way to the
// transcript: carriage returns dropped (the pseudo-terminal turns every newline into CR LF),
// each known prompt answered once per appearance.
type attendant struct {
	out     io.Writer // the transcript
	keys    io.Writer // the terminal's input
	prompts []prompt
	stop    func() // ends the run
	seen    []byte // output not yet matched, cut to a tail shorter than the longest prompt
	refused string // the text of the refused prompt that ended the run, if one did
}

// Write implements io.Writer.
func (a *attendant) Write(p []byte) (int, error) {
	clean := bytes.ReplaceAll(p, []byte("\r"), nil)
	if _, err := a.out.Write(clean); err != nil {
		return 0, fmt.Errorf("the transcript: %w", err)
	}
	if a.refused != "" {
		return len(p), nil
	}
	a.seen = append(a.seen, clean...)
	for {
		at, pr := a.first()
		if at < 0 {
			break
		}
		a.seen = a.seen[at+len(pr.text):]
		if pr.refuse {
			a.refused, a.seen = pr.text, nil
			a.stop()
			return len(p), nil
		}
		if _, err := io.WriteString(a.keys, pr.reply); err != nil {
			return 0, fmt.Errorf("typing the answer to %q: %w", pr.text, err)
		}
	}
	// A prompt wholly in the tail would have matched above, so the tail need hold only the
	// start of one split across writes.
	if keep := a.longest() - 1; len(a.seen) > keep {
		a.seen = a.seen[len(a.seen)-keep:]
	}
	return len(p), nil
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

// attended runs remote in the guest at a terminal, bounded by limit, answering prompts; its
// output goes to w. It returns the remote status (124 for the bound, as stream does), or the
// text of the refused prompt that ended it, and an error only when ssh could not be run at
// all or the CLI itself was cancelled.
func (g *guest) attended(ctx context.Context, limit time.Duration, remote string, w io.Writer, prompts []prompt) (int, string, error) {
	bounded, cancel := g.c.bound(ctx, limit)
	defer cancel()
	// An *os.File is handed to ssh as its stdin itself, so no copying goroutine outlives it
	// waiting for an answer nobody will type.
	keysIn, keysOut, err := os.Pipe()
	if err != nil {
		return 0, "", failf("cannot make the terminal's input: %v", err)
	}
	a := &attendant{out: w, keys: keysOut, prompts: prompts, stop: cancel}
	cmd := execx.Cmd{
		Name:  sshBin,
		Args:  append([]string{"-tt", "-e", "none", "-o", "ServerAliveInterval=30"}, g.sshArgs(nil, remote)...),
		Stdin: keysIn, Stdout: a, Stderr: a,
	}
	_, runErr := g.c.env.Runner.Run(bounded, cmd)
	if closeErr := errors.Join(keysIn.Close(), keysOut.Close()); closeErr != nil {
		return 0, "", failf("cannot close the terminal's input: %v", closeErr)
	}
	if a.refused != "" {
		return 0, a.refused, nil
	}
	rc, err := statusOf(ctx, bounded, runErr)
	return rc, "", err
}
