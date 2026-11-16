package placeissued

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
)

// lockedBuffer is a stderr Place and the test both touch.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// An issued seat that never comes on an open pipe (the owner's pipe from the host left open) is
// said on stderr, then refused naming the bound, and nothing is placed.
func TestAnIssuedSeatThatNeverComesIsRefusedNamingTheBound(t *testing.T) {
	w := newWorld(t)
	r, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, f := range []*os.File{pw, r} {
			if cerr := f.Close(); cerr != nil && !errors.Is(cerr, os.ErrClosed) {
				t.Error(cerr)
			}
		}
	})
	clk, errs := clock.NewFake(0, 0), &lockedBuffer{}
	w.deps.Watch = stdinread.Options{Err: errs, Clock: clk}
	got := make(chan error, 1)
	go func() {
		_, perr := Place(context.Background(), w.deps, r)
		got <- perr
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, d := range []time.Duration{stdinread.NoticeAfter, stdinread.PipedGiveUp - stdinread.NoticeAfter} {
		if derr := clk.AwaitDue(ctx, d); derr != nil {
			t.Fatal(derr)
		}
		clk.Advance(d)
	}
	err = <-got
	if e, ok := errors.AsType[*exits.Error](err); !ok || e.Code != exits.CodeBundle || !strings.Contains(e.Message, "no input arrived on stdin within 60 s") {
		t.Fatalf("err %v", err)
	}
	if !errors.As(err, new(*stdinread.NoInputError)) {
		t.Fatalf("err %v does not carry the bound", err)
	}
	if !strings.HasPrefix(errs.String(), "teamchat seat place-issued: waiting for input on stdin") {
		t.Fatalf("stderr %q", errs.String())
	}
	if _, statErr := os.Stat(w.deps.StateHome); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the seat store was touched: %v", statErr)
	}
}
