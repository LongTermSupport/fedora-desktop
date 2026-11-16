package keyrelay

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// pipe is a pair of pipe ends both closed when the test ends.
func pipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, f := range []*os.File{r, w} {
			if err := f.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				t.Error(err)
			}
		}
	})
	return r, w
}

// ended waits for the relay to end, failing the test if it has not within the bound.
func ended(t *testing.T, r *Relay) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	select {
	case <-r.done:
	case <-ctx.Done():
		t.Fatal("the relay did not end once stopped")
	}
}

// The property that makes the relay unbounded but safe: on a terminal nothing is ever typed on,
// it waits in poll, never in a read, so stopping it ends it at once.
func TestStoppingEndsTheRelayWhenNoKeyIsEverTyped(t *testing.T) {
	terminal, quiet := pipe(t)
	if quiet == nil {
		t.Fatal("no writer")
	}
	_, out := pipe(t)
	r, err := Start(out, terminal, int(terminal.Fd()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	ended(t, r)
	if err := r.Wait(); err != nil {
		t.Fatal(err)
	}
}

// What is typed is relayed through the filter until the input ends.
func TestTheRelayCopiesThroughTheFilter(t *testing.T) {
	to, out := pipe(t)
	drop := func(p []byte) []byte { return bytes.ReplaceAll(p, []byte("x"), nil) }
	r, err := Start(out, strings.NewReader("axbxc"), -1, drop)
	if err != nil {
		t.Fatal(err)
	}
	ended(t, r)
	if err = errors.Join(r.Stop(), r.Wait(), out.Close()); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 16)
	n, err := to.Read(got)
	if err != nil || string(got[:n]) != "abc" {
		t.Fatalf("%q %v", got[:n], err)
	}
}
