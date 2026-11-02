//go:build tuwunel

package teamsync

// The team goroutine against a real Tuwunel: `go test -tags tuwunel` with FAKEHS_TUWUNEL_BIN
// naming the binary. SY1 (b)'s first answer, a delivered mention, the floor marker put and read
// back, and a second link with no store resuming after the marker.

import (
	"context"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/tuwunelhs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const tuwunelTestLimit = 2 * time.Minute

// tuwunelWorld is the world on a real Tuwunel, its admin registered through the key.
func tuwunelWorld(t *testing.T) *world {
	t.Helper()
	ctx := t.Context()
	hs := tuwunelhs.Start(ctx, t, server)
	w := emptyWorld(t, hs.Base)
	anon, err := matrix.NewAdmin(matrix.Config{BaseURL: hs.Base})
	check(t, err)
	t.Cleanup(anon.Close)
	sess, err := anon.Register(ctx, hs.RegKey, matrix.Registration{Username: proto.AdminLocalpart, Password: tuwunelhs.RandomKey(t), Admin: true})
	check(t, err)
	w.adm, err = anon.WithToken(sess.AccessToken.Reveal())
	check(t, err)
	for _, local := range accounts {
		if _, err := w.adm.CreateAccount(ctx, user(local), tuwunelhs.RandomKey(t), ""); err != nil {
			t.Fatalf("CreateAccount %s: %v", local, err)
		}
	}
	w.populate(func(id string) (string, error) {
		tok, err := w.adm.MintToken(ctx, id)
		return tok.Reveal(), err
	})
	return w
}

// runReal runs a team on the system clock until the test ends.
func (w *world) runReal(ctx context.Context) *Team {
	w.t.Helper()
	first := make(chan struct{})
	close(first)
	tm, err := New(Config{
		Team: w.team, Seat: w.seat, Root: w.root, Run: runID(w.t, linkRun), Engine: w.eng, Ends: w.ends,
		Clock: clock.System(), FirstSessionStart: first, Log: w.log,
	})
	check(w.t, err)
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- tm.Run(ctx) }()
	w.t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			w.t.Errorf("Run: %v", err)
		}
	})
	return tm
}

func TestTuwunelSync(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), tuwunelTestLimit)
	defer cancel()
	w := tuwunelWorld(t)
	tm := w.runReal(ctx)
	if b := w.batch(); b.NextBatch == "" || len(b.Items) != 0 {
		t.Fatalf("first batch %+v", b)
	}
	id := w.say(alice, human("hello", user(me)))
	b := w.batch()
	if got := itemIDs(b); len(got) != 1 || got[0] != id {
		t.Fatalf("items %v", got)
	}
	check(t, tm.FlushFloor(ctx))
	if f := w.marker(); f != b.Items[0].Pending.SentMS-1 {
		t.Fatalf("marker %d, want %d: just below the item still owed", f, b.Items[0].Pending.SentMS-1)
	}

	// A second link with no store resumes after the marker, which keeps the owed mention in reach.
	w.eng = newFakeEngine(t, w.team, runID(t, linkRun))
	w.runReal(ctx)
	if got := itemIDs(w.batch()); len(got) != 1 || got[0] != id {
		t.Errorf("resumed items %v, want the owed %s", got, id)
	}
	next := w.say(alice, human("again", user(me)))
	if got := itemIDs(w.batch()); len(got) != 1 || got[0] != next {
		t.Errorf("items %v", got)
	}
}
