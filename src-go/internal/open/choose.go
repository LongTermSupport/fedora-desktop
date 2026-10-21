package open

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// maxTries bounds the numbered menu's re-prompts (InteractiveScripts.md rule 2).
const maxTries = 3

// cancelRow is the picker's last row, so cancelling is a visible choice (B19).
const cancelRow = "Cancel — open nothing"

// ttyReader is the controlling terminal, opened once for the run.
type ttyReader struct {
	lines  *bufio.Reader
	closer io.Closer
}

func (r *run) closeTTY() error {
	if r.tty == nil {
		return nil
	}
	err := r.tty.closer.Close()
	r.tty = nil
	if err != nil {
		return fmt.Errorf("close the terminal: %w", err)
	}
	return nil
}

// choose is E16-E19: the index of the candidate the person chose.
func (r *run) choose(ctx context.Context, target, mime string, nth, total int, cands []candidate) (int, error) {
	if err := r.needTerminal(target, mime); err != nil {
		return 0, err
	}
	about := mime
	if total > 1 {
		about = fmt.Sprintf("target %d of %d · %s", nth, total, mime)
	}
	heading := ui.Here("open", filepath.Base(target)).About(about)
	labels := make([]string, len(cands))
	tags := make([]string, len(cands))
	for i, c := range cands {
		labels[i], tags[i] = c.label, strconv.Itoa(i)
	}
	picked, err := r.env.Picker.Pick(ctx, heading, ui.PickSpec{
		Title: "open", Header: ui.Header("Enter open   Esc or q cancel"),
		Rows: labels, Tags: tags, ExitLabel: cancelRow,
	})
	switch {
	case err == nil:
		return tagIndex(picked.Tag, len(cands))
	case errors.Is(err, ui.ErrLeave):
		return 0, errCancelled
	case errors.Is(err, ui.ErrFzfMissing):
		return r.numbered(ctx, heading, labels)
	default:
		return 0, err
	}
}

func tagIndex(tag string, n int) (int, error) {
	i, err := strconv.Atoi(tag)
	if err != nil || i < 0 || i >= n {
		return 0, fmt.Errorf("the picker chose a row it was not shown (tag %q)", tag)
	}
	return i, nil
}

// needTerminal is E16: stderr a terminal and the controlling terminal open.
func (r *run) needTerminal(target, mime string) error {
	if r.env.IsTerminal(r.env.Stderr) && r.tty == nil {
		if rc, err := r.env.OpenTTY(); err == nil {
			r.tty = &ttyReader{lines: bufio.NewReader(rc), closer: rc}
		}
	}
	if r.tty != nil && r.env.IsTerminal(r.env.Stderr) {
		return nil
	}
	return fail("no default handler for '%s' (%s), and no terminal to ask on.\n"+
		"  Run 'open --list %s' to see the options, or, from a terminal, choose one and\n"+
		"  register it as the default for next time with 'open --set-default %s'.", target, mime, target, target)
}

// numbered is E18-E19: the menu without fzf, read from the terminal.
func (r *run) numbered(ctx context.Context, heading ui.Heading, labels []string) (int, error) {
	colour := r.env.Getenv("NO_COLOR") == ""
	drawn, err := heading.Draw(colour, r.columns(ctx))
	if err != nil {
		return 0, err
	}
	r.note(drawn)
	for i, l := range labels {
		r.note(fmt.Sprintf("  %2d) %s", i+1, l))
	}
	// A handler's interrupts are taken when it ends, so one pending here came after it: a
	// cancel before anything runs (E31).
	if r.takeInterrupts().seen {
		return 0, errCancelled
	}
	for attempt := 1; ; attempt++ {
		r.errOut.Print(r.env.Stderr, fmt.Sprintf("Choice [1-%d, q to cancel]: ", len(labels)))
		reply, err := r.readLine()
		if errors.Is(err, io.EOF) || errors.Is(err, errInterrupted) {
			r.note("")
			return 0, errCancelled
		}
		if err != nil {
			return 0, err
		}
		switch reply {
		case "q", "Q", "quit":
			return 0, errCancelled
		}
		if n, err := strconv.Atoi(reply); err == nil && n >= 1 && n <= len(labels) && !strings.HasPrefix(reply, "+") {
			return n - 1, nil
		}
		r.note(fmt.Sprintf("  Not one of the options — enter a number between 1 and %d, or q to cancel.", len(labels)))
		if attempt >= maxTries {
			return 0, fail("giving up after %d attempts — nothing opened.", maxTries)
		}
	}
}

// columns is the terminal's width as tput gives it, for the heading (InteractiveScripts.md
// rule 15); 80 when tput cannot say.
func (r *run) columns(ctx context.Context) int {
	var out bytes.Buffer
	if _, err := r.env.Runner.Run(ctx, execx.Cmd{Name: "tput", Args: []string{"cols"}, Stdout: &out, Stderr: r.env.Stderr}); err != nil {
		return 80
	}
	if n, err := strconv.Atoi(strings.TrimSpace(out.String())); err == nil && n > 0 {
		return n
	}
	return 80
}

// errInterrupted is Ctrl-C (or Ctrl-\) at the prompt.
var errInterrupted = errors.New("interrupted")

// readLine is one reply from the terminal, trimmed; end of input is io.EOF even after a partial
// line, and an interrupt is errInterrupted without waiting for the read.
func (r *run) readLine() (string, error) {
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	lines := r.tty.lines
	go func() {
		line, err := lines.ReadString('\n')
		done <- result{line, err}
	}()
	select {
	case <-r.env.Interrupts:
		return "", errInterrupted
	case res := <-done:
		if res.err != nil {
			if errors.Is(res.err, io.EOF) {
				return "", io.EOF
			}
			return "", fmt.Errorf("read the reply: %w", res.err)
		}
		return strings.TrimSpace(res.line), nil
	}
}
