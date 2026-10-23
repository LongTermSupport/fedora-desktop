package ends

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/notice"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The run's lifecycle notices for one team are posted one at a time, and back never follows
// the run's own end (Plan 00209 n3c: TestE2ECmdKilled and TestE2EKeeperKilled saw `back` after
// the run's `disconnected` under load, the back goroutine having read main after the end).

// priorDisconnected is a previous run's disconnected for worker, so a back is owed.
func priorDisconnected(t *testing.T, w *world) {
	t.Helper()
	w.clock.set(2_000)
	out, err := notice.Disconnected(t.Context(), w.client(t, worker), notice.Notice{Server: server, Team: teamID, MainRoom: w.main, Handle: worker, Token: w.tokens[worker], Run: runID(t, priorRun)})
	if err != nil || !out.Posted {
		t.Fatalf("disconnected: %+v %v", out, err)
	}
	w.clock.set(3_000)
}

func events(got []lifecycle) []string {
	out := make([]string, len(got))
	for i, l := range got {
		out[i] = l.event
	}
	return out
}

// A back that comes after the run's end posts nothing: main's latest stays the end.
func TestABackAfterTheEndPostsNothing(t *testing.T) {
	w := newWorld(t)
	f := newEnds(t, nil)
	alpha := teamOf(t, teamID)
	priorDisconnected(t, w)
	run := state.StoreRun{ID: runID(t, thisRun)}
	errs := f.e.End(t.Context(), team.End{How: wire.EndKilled}, []team.EndTeam{{Team: alpha, Notices: w.notices(t, worker, true), Run: run}})
	if err := errors.Join(errs...); err != nil {
		t.Fatalf("End: %v", err)
	}
	n := w.notices(t, worker, true)
	if err := f.e.Back(t.Context(), alpha, n, run); err != nil {
		t.Fatalf("Back: %v", err)
	}
	if got := events(w.lifecycles(t)); len(got) != 2 || got[0] != proto.EventDisconnected || n.sent != 0 {
		t.Fatalf("main, newest first: %v; back sent %d", got, n.sent)
	}
}

// gatedNotices holds its first Messages until released: a back stopped mid-read.
type gatedNotices struct {
	*fakeNotices

	once             sync.Once
	reading, release chan struct{}
}

func (g *gatedNotices) Messages(ctx context.Context, roomID string, req matrix.MessagesRequest) (matrix.MessagesPage, error) {
	g.once.Do(func() {
		close(g.reading)
		<-g.release
	})
	return g.fakeNotices.Messages(ctx, roomID, req)
}

// An end, or a leave, that comes while back is between its read and its post waits for back's
// turn, then posts: back, then the end, never the other way round.
func TestAnEndWaitsForABackMidRead(t *testing.T) {
	for _, c := range []struct {
		name, event string
		end         ender
	}{
		{"end", proto.EventDisconnected, func(t *testing.T, e *ends, n team.Notices, run state.StoreRun) error {
			t.Helper()
			return errors.Join(e.End(t.Context(), team.End{How: wire.EndKilled}, []team.EndTeam{{Team: teamOf(t, teamID), Notices: n, Run: run}})...)
		}},
		{"leave", proto.EventLeft, func(t *testing.T, e *ends, n team.Notices, _ state.StoreRun) error {
			t.Helper()
			return e.Leave(t.Context(), teamOf(t, teamID), n, "tc-leave")
		}},
	} {
		t.Run(c.name, func(t *testing.T) { midRead(t, c.event, c.end) })
	}
}

// ender is an end or a leave of team alpha, posted through n.
type ender func(t *testing.T, e *ends, n team.Notices, run state.StoreRun) error

func midRead(t *testing.T, event string, end ender) {
	t.Helper()
	w := newWorld(t)
	f := newEnds(t, nil)
	alpha := teamOf(t, teamID)
	priorDisconnected(t, w)
	run := state.StoreRun{ID: runID(t, thisRun)}
	g := &gatedNotices{fakeNotices: w.notices(t, worker, true), reading: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(g.release) }) }
	t.Cleanup(release)
	waiting := make(chan struct{}, 1)
	f.e.turnWait = func() { waiting <- struct{}{} }
	backDone := make(chan error, 1)
	go func() { backDone <- f.e.Back(t.Context(), alpha, g, run) }()
	<-g.reading
	endNotices := w.notices(t, worker, true)
	endDone := make(chan error, 1)
	go func() { endDone <- end(t, f.e, endNotices, run) }()
	select {
	case <-waiting:
	case err := <-endDone:
		t.Fatalf("the %s went ahead of back's read: %v; main %v", event, err, events(w.lifecycles(t)))
	}
	release()
	if err := <-backDone; err != nil {
		t.Fatalf("Back: %v", err)
	}
	if err := <-endDone; err != nil {
		t.Fatalf("the %s: %v", event, err)
	}
	want := []string{event, proto.EventBack, proto.EventDisconnected}
	if got := events(w.lifecycles(t)); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("main, newest first: %v, want %v", got, want)
	}
}

// An end that gives up waiting for the turn (its bound ended) has still begun: a back after it
// posts nothing.
func TestAnEndThatCouldNotWaitStillStopsBack(t *testing.T) {
	w := newWorld(t)
	f := newEnds(t, nil)
	alpha := teamOf(t, teamID)
	priorDisconnected(t, w)
	run := state.StoreRun{ID: runID(t, thisRun)}
	inside, leave := make(chan struct{}), make(chan struct{})
	held := make(chan error, 1)
	go func() {
		held <- f.e.turn(t.Context(), alpha, func() error {
			close(inside)
			<-leave
			return nil
		})
	}()
	<-inside
	ctx, cancel := context.WithCancel(t.Context())
	f.e.turnWait = cancel
	errs := f.e.End(ctx, team.End{How: wire.EndKilled}, []team.EndTeam{{Team: alpha, Notices: w.notices(t, worker, true), Run: run}})
	close(leave)
	if err := errors.Join(errs...); !errors.Is(err, context.Canceled) {
		t.Fatalf("End: %v", err)
	}
	if err := <-held; err != nil {
		t.Fatalf("the held turn: %v", err)
	}
	f.e.turnWait = nil
	n := w.notices(t, worker, true)
	if err := f.e.Back(t.Context(), alpha, n, run); err != nil || n.sent != 0 {
		t.Fatalf("Back: %v, sent %d; main %v", err, n.sent, events(w.lifecycles(t)))
	}
}

// A turn given up by its context is the context's error, and the turn is not taken.
func TestATurnWaitEndsWithItsContext(t *testing.T) {
	f := newEnds(t, nil)
	alpha := teamOf(t, teamID)
	inside, leave := make(chan struct{}), make(chan struct{})
	held := make(chan error, 1)
	go func() {
		held <- f.e.turn(t.Context(), alpha, func() error {
			close(inside)
			<-leave
			return nil
		})
	}()
	<-inside
	ctx, cancel := context.WithCancel(t.Context())
	f.e.turnWait = cancel
	ran := false
	err := f.e.turn(ctx, alpha, func() error {
		ran = true
		return nil
	})
	close(leave)
	if !errors.Is(err, context.Canceled) || ran {
		t.Fatalf("turn: %v, ran %v", err, ran)
	}
	if err := <-held; err != nil {
		t.Fatalf("the held turn: %v", err)
	}
}
