package reclaim

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// blockingTTY is a terminal on which nothing is ever typed: its first read calls onRead (the
// Ctrl-C), then every read waits until it is closed, as a terminal's does, or the test ends.
type blockingTTY struct {
	onRead  func()
	done    chan struct{}
	closing sync.Once
	// returned is closed when a read has returned.
	returned  chan struct{}
	returning sync.Once
}

func (b *blockingTTY) Read([]byte) (int, error) {
	if b.onRead != nil {
		f := b.onRead
		b.onRead = nil
		f()
	}
	<-b.done
	b.returning.Do(func() { close(b.returned) })
	return 0, os.ErrClosed
}

func (b *blockingTTY) Close() error {
	b.closing.Do(func() { close(b.done) })
	return nil
}

func newBlockingTTY(t *testing.T, onRead func()) *blockingTTY {
	t.Helper()
	b := &blockingTTY{onRead: onRead, done: make(chan struct{}), returned: make(chan struct{})}
	t.Cleanup(func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	return b
}

// E30: Ctrl-C while a root command runs waits for that command (its context is not ended under
// it, so it is not signalled or killed by reclaim), runs nothing after it, says so and exits 130.
func TestCtrlCDuringRootCommandWaitsForIt(t *testing.T) {
	w := newWorld(t)
	w.install("dnf")
	w.answer([]string{"dnf", "-y", "autoremove"}, "", 0)
	w.during["sudo -n dnf -y autoremove"] = w.interrupt
	w.typed("3", "y", "q")
	wantCode(t, w.run(), term.ExitCancelled)
	if len(w.cancelledUnder) != 0 {
		t.Errorf("commands abandoned mid-run: %q", w.cancelledUnder)
	}
	if got, want := w.changes(), []string{"root dnf -y autoremove"}; !slices.Equal(got, want) {
		t.Errorf("changes %q, want %q", got, want)
	}
	w.wantErr("\nCancelled.\n")
	w.wantNoErr("Bye.", "Package cleanup complete.", "action(s) failed")
}

// E30: Ctrl-C between the sweep's steps ends the sweep there.
func TestCtrlCDuringSweep(t *testing.T) {
	w := newWorld(t)
	w.install("dnf", "journalctl")
	w.answer([]string{"dnf", "-y", "autoremove"}, "", 0)
	w.answer([]string{"dnf", "clean", "all"}, "", 0)
	w.during["sudo -n dnf clean all"] = w.interrupt
	w.typed("10", "y", "y", "", "y", "q")
	wantCode(t, w.run(), term.ExitCancelled)
	if w.ran("sudo -n journalctl") {
		t.Error("the sweep went on after Ctrl-C")
	}
	w.wantErr("Cancelled.")
	w.wantNoErr("Safe sweep finished.")
}

// E30: Ctrl-C at the menu or at a question ends the run at once, exit 130.
func TestCtrlCAtAPrompt(t *testing.T) {
	w := newWorld(t)
	w.ttyIn = newBlockingTTY(t, w.interrupt)
	wantCode(t, w.run(), term.ExitCancelled)
	w.wantErr("Choose [1-10, r, q]: ", "\nCancelled.\n")
	w.wantNoErr("No more input", "Bye.")

	w = newWorld(t)
	w.install("journalctl")
	answered := false
	w.ttyIn = &lineThenBlock{line: "5\n", then: newBlockingTTY(t, w.interrupt), answered: &answered}
	wantCode(t, w.run(), term.ExitCancelled)
	w.wantErr("Vacuum journal down to what size? [500M] ", "Cancelled.")
	if strings.Count(w.stderr.String(), "Cancelled.") != 1 {
		t.Errorf("cancelled said more than once:\n%s", w.stderr.String())
	}
}

// E30: the run ends the terminal's reader before it ends: the terminal is closed, which ends the
// read the Ctrl-C left waiting, and the reader is waited for.
func TestCtrlCAtAPromptEndsTheTerminalsReader(t *testing.T) {
	w := newWorld(t)
	tty := newBlockingTTY(t, w.interrupt)
	w.ttyIn = tty
	wantCode(t, w.run(), term.ExitCancelled)
	select {
	case <-tty.returned:
	default:
		t.Fatal("the run ended with a read of the terminal still waiting")
	}
}

// lineThenBlock types line, then behaves as then.
type lineThenBlock struct {
	line     string
	then     *blockingTTY
	answered *bool
}

func (l *lineThenBlock) Read(p []byte) (int, error) {
	if !*l.answered {
		*l.answered = true
		return copy(p, l.line), nil
	}
	return l.then.Read(p)
}

func (l *lineThenBlock) Close() error { return l.then.Close() }

// E30: Ctrl-C while ~/.cache is cleared stops after the entry being removed and says the clear
// is only partly done.
func TestCtrlCDuringCacheClear(t *testing.T) {
	w := newWorld(t)
	cache := w.dir(".cache")
	for _, n := range []string{"a", "b", "c"} {
		w.file(".cache/"+n, "x")
	}
	w.answer([]string{"du"}, "4096\t"+cache+"\n", 0)
	removed := 0
	w.removeAll = func(p string) error {
		removed++
		w.interrupt()
		return os.RemoveAll(p)
	}
	w.typed("8", "y", "q")
	wantCode(t, w.run(), term.ExitCancelled)
	if removed != 1 || len(entries(t, cache)) != 2 {
		t.Errorf("removed %d, left %v", removed, entries(t, cache))
	}
	w.wantErr("  ~/.cache is only partly cleared: cancelled after 1 of 3 entries.", "Cancelled.")
	w.wantNoErr("~/.cache cleared.")
}

// E30: Ctrl-C during the report ends it with 130.
func TestCtrlCDuringReport(t *testing.T) {
	w := newWorld(t)
	w.dir(".cache")
	w.answer([]string{"du"}, "4096\t"+filepath.Join(w.home, ".cache")+"\n", 0)
	w.during["du -s -B1 "+filepath.Join(w.home, ".cache")] = w.interrupt
	wantCode(t, w.run("report"), term.ExitCancelled)
	w.wantErr("Cancelled.")
}
