package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ttyecho"
)

// screenLog is what a pseudo-terminal's programs drew, read off its master as they draw it.
type screenLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *screenLog) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *screenLog) shows(text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Contains(s.buf.String(), text)
}

// gumPty is a pseudo-terminal of 100 by 30 as tmux gives one: the master a person types into
// (its screen copied into the log), and the terminal the command holds.
func gumPty(t *testing.T) (*os.File, *os.File, *screenLog) {
	t.Helper()
	if _, err := exec.LookPath("gum"); err != nil {
		t.Skip("gum is not installed; this test drives the real gum")
	}
	t.Setenv("TERM", "tmux-256color")
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(fd), "/dev/ptmx")
	if err = unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetUint32(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	if err = unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: 30, Col: 100}); err != nil {
		t.Fatal(err)
	}
	terminal, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	screen := &screenLog{}
	copied := make(chan error, 1)
	go func() {
		_, err := io.Copy(screen, master)
		copied <- err
	}()
	t.Cleanup(func() {
		if err := errors.Join(terminal.Close(), master.Close()); err != nil {
			t.Error(err)
		}
		// The copy ends when the master closes; how it ends says nothing about the test.
		<-copied
	})
	return master, terminal, screen
}

// waitFor waits until the screen shows text.
func waitFor(t *testing.T, screen *screenLog, text string) {
	t.Helper()
	for range 1000 {
		if screen.shows(text) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the screen never showed %q", text)
}

func press(t *testing.T, master *os.File, keys string) {
	t.Helper()
	if _, err := io.WriteString(master, keys); err != nil {
		t.Fatal(err)
	}
}

const (
	stay = "Leave it running"
	keep = "Take it down, keep its data"
)

// confirmEnds tells when the gum confirm's process has ended (its Run returned), so a test can
// place a key in the window between one screen ending and the next drawing.
type confirmEnds struct {
	next  execx.Runner
	ended chan struct{}
}

func (r confirmEnds) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	res, err := r.next.Run(ctx, c)
	if c.Name == "gum" && slices.Contains(c.Args, "confirm") {
		close(r.ended)
	}
	return res, err
}

// takeDownSteps is ccy-teams remove's two questions on the real gum under a real hold: "Back it up
// first?", then, when that is answered and does not cancel, the choice that starts on stay. answer
// is the keys that answer the first; gap after the confirm has ended, a Down is sent on its own,
// as the host agent's tmux send-keys did; Enter takes the choice once it is drawn.
//
// The gap counts from the confirm's end, not from the answer's write. A Down written before gum
// has read the Esc is read with it as one sequence, which gum takes as Alt+Down: the confirm does
// not end, and step 2 never draws (Task 12.6: 7 of 50 under load at a 1 ms gap from the write).
// The confirm ends some 3 ms after the Esc on an idle machine, so the gaps still cover the host's
// 1 to 40 ms after the Esc, and each Down lands where the host's did: after the confirm, before
// the next question draws.
func takeDownSteps(t *testing.T, destructive bool, answer string, gap time.Duration) string {
	t.Helper()
	master, terminal, screen := gumPty(t)
	hold := ttyecho.New(terminal)
	confirm := confirmEnds{next: execx.OS{}, ended: make(chan struct{})}
	g := &Gum{Runner: hold.Runner(confirm), Stdin: terminal, Stderr: terminal}
	type result struct {
		choice string
		err    error
	}
	done := make(chan result, 1)
	// The questions use the terminal, so on a failure the pty is not closed until they have
	// ended (t.Context is cancelled first, which ends gum); cleanups run last in, first out.
	finished := make(chan struct{})
	t.Cleanup(func() { <-finished })
	go func() {
		defer close(finished)
		var r result
		r.err = errors.Join(r.err, hold.Held(func() {
			if destructive {
				defer g.Destructive()()
			}
			if _, err := g.Confirm(t.Context(), Here("Take down"), "Back it up first?", true); err != nil {
				r.err = err
				return
			}
			r.choice, r.err = g.Choose(t.Context(), Here("Take down"), "What happens to it?", stay, []string{stay, keep, "Delete it"})
		}))
		done <- r
	}()
	waitFor(t, screen, "up first?")
	press(t, master, answer)
	select {
	case <-confirm.ended:
	case <-time.After(10 * time.Second):
		t.Fatalf("the confirm did not end on %q", answer)
	}
	time.Sleep(gap)
	press(t, master, "\x1b[B")
	waitFor(t, screen, "What happens to it?")
	// The choice has drawn; give gum a moment to read what reached it before Enter.
	time.Sleep(300 * time.Millisecond)
	press(t, master, "\r")
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	return r.choice
}

// Plan 00177 Task 12.5, the host agent's finding at Task 12.4: in ccy-teams remove, Esc on "Back
// up first?" then a Down sent on its own 1 to 40 ms later moved step 2 off stay, 8 of 8. Task
// 12.3's flush ran the moment the confirm ended; the Down came after it, while the next gum was
// starting, and gum read it before it drew. Each gap here is one the host saw carried, counted
// from the confirm's end (takeDownSteps says why).
func TestOnTheRealGumADownSentJustAfterEscDoesNotReachTheNextQuestion(t *testing.T) {
	for _, gap := range []time.Duration{time.Millisecond, 5 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond} {
		t.Run(gap.String(), func(t *testing.T) {
			if got := takeDownSteps(t, true, "\x1b", gap); got != stay {
				t.Fatalf("Esc, then Down %v later: step 2 took %q, want %q", gap, got, stay)
			}
		})
	}
}

// The other half of the rule: after a plain answer, outside a destructive flow, a key typed while
// the next question loads still reaches it (type-ahead is kept).
func TestOnTheRealGumAKeyTypedAfterAPlainAnswerReachesTheNextQuestion(t *testing.T) {
	if got := takeDownSteps(t, false, "y", 20*time.Millisecond); got != keep {
		t.Fatalf("y, then Down 20ms later: step 2 took %q, want %q", got, keep)
	}
}
