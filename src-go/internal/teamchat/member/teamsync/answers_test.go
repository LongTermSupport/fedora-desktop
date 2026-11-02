package teamsync

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// ref is the n-th commit reference of the org's repository, each its own SHA.
func ref(n int) string { return fmt.Sprintf("commit:example-org/myrepo@%040x", n) }

// noLookup is a sender's store that knows no earlier event, for composing an answer.
type noLookup struct{}

func (noLookup) Seen(string) bool                     { return false }
func (noLookup) Event(string) (proto.Related, bool)   { return proto.Related{}, false }
func (noLookup) OwnPing(string) (proto.OwnPing, bool) { return proto.OwnPing{}, false }

// ownPing posts my review ping to local, and puts it in the engine's outbox as D6a would.
func (w *world) ownPing(local string) string {
	w.t.Helper()
	out, err := w.composer(me).Ping(proto.Ping{Verb: "review", To: []string{user(local)}, Ref: ref(0)}, "", "")
	id := w.post(me, out, err)
	w.eng.ownPing(id, proto.OwnPing{Verb: "review", To: []string{user(local)}, OriginTs: w.ts(id)})
	return id
}

// answer posts peer's verb ping to me in the thread of ping.
func (w *world) answer(verb, ping, reference string) string {
	w.t.Helper()
	c := w.composer(peer)
	c.Lookup = noLookup{}
	out, err := c.Ping(proto.Ping{Verb: verb, To: []string{user(me)}, Ref: reference}, ping, "")
	return w.post(peer, out, err)
}

// SY4-n3d-1: an ack in an own ping's thread gives one Answer and no item; a done, one Answer and
// one PING item; a done whose forge check fails, a drop and no Answer (n3d-D25).
func TestSY4n3d1AnswersToAnOwnPing(t *testing.T) {
	w := newWorld(t)
	f := newFakeForge(map[string]forgeAnswer{ref(2): {outcome: proto.ReasonProvenance}})
	own := w.ownPing(peer)
	r, release := w.held(w.newTeam(withForge(f)))
	ack := w.answer(proto.VerbAck, own, "")
	done := w.answer(proto.VerbDone, own, ref(1))
	bad := w.answer(proto.VerbDone, own, ref(2))
	release()
	b := w.batch()
	r.poll(t)
	want := []team.Answer{
		{Ping: own, From: user(peer), Verb: proto.VerbAck, ID: ack, OriginMS: w.ts(ack)},
		{Ping: own, From: user(peer), Verb: proto.VerbDone, ID: done, OriginMS: w.ts(done)},
	}
	if !slices.Equal(b.Answered, want) {
		t.Errorf("answered %+v, want %+v", b.Answered, want)
	}
	if got := itemIDs(b); len(got) != 1 || got[0] != done {
		t.Errorf("items %v", got)
	}
	if b.Counts.Dropped != 1 || w.log.count("dropped "+bad+": provenance") != 1 {
		t.Errorf("counts %+v, log %q", b.Counts, w.log.String())
	}
	if calls := f.checked(); len(calls) != 2 {
		t.Errorf("checked %v: an ack has no reference and is never checked", calls)
	}
}

// SY4-n3d-2 (n3d-D9): my own ping come back in sync gives one Echo; my own text gives none.
func TestSY4n3d2AnOwnPingsEcho(t *testing.T) {
	w := newWorld(t)
	_, release := w.held(w.newTeam())
	own := w.ownPing(peer)
	w.agentText(me, "my say", user(coord))
	release()
	b := w.batch()
	if want := []team.Echo{{ID: own, OriginMS: w.ts(own)}}; !slices.Equal(b.Echoes, want) {
		t.Errorf("echoes %+v, want %+v", b.Echoes, want)
	}
	if len(b.Answered) != 0 || len(b.Items) != 0 {
		t.Errorf("batch %+v", b)
	}
}

// SY4-n3d-3 (SY7 case 4, teamsync's half): an answer inside an answer SY6 discards is in no
// persisted batch, so its ping keeps its TIMEOUT.
func TestSY4n3d3ADiscardedAnswerIsInNoBatch(t *testing.T) {
	w := newWorld(t)
	own := w.ownPing(peer)
	snap := w.srv.Snapshot()
	tm := w.newTeam()
	var once sync.Once
	tm.judge = func(j proto.Judge, v proto.Value) proto.Verdict {
		verdict := j.Judge(v)
		if verdict.Answer != nil {
			once.Do(func() { check(t, w.srv.RestoreTo(snap)) })
		}
		return verdict
	}
	r, release := w.held(tm)
	w.answer(proto.VerbAck, own, "")
	release()
	for {
		b := w.batchOrWait(r)
		if len(b.Answered) != 0 {
			t.Fatalf("a discarded answer persisted: %+v", b.Answered)
		}
		if b.RestoreEpoch == w.roster.RestoreEpoch+1 {
			break
		}
	}
	if resets := w.eng.resetEpochs(); len(resets) != 1 {
		t.Errorf("resets %v", resets)
	}
}

// SY4a: a human's thread reply under an own say recorded through Notices is delivered, with a
// store holding no record of the say and no fetch of it.
func TestSY4aAThreadUnderARecordedOwnSend(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	root := w.agentText(me, "my say", user(coord))
	tm.Notices().RecordOwnSend(root, proto.Related{Sender: user(me), RoomID: w.main})
	_, release := w.held(tm)
	id := w.say(alice, inThread("under your say", root))
	release()
	if got := itemIDs(w.batch()); len(got) != 1 || got[0] != id {
		t.Errorf("items %v", got)
	}
	if n := w.fetches(); n != 0 {
		t.Errorf("%d fetches", n)
	}
}

// n3d-D3: the first verification saves its rev's roster as read; a session that verifies the
// same rev again does not; a new rev is saved once.
func TestN3dD3ANewRevIsSavedOnce(t *testing.T) {
	w := newWorld(t)
	r := w.start()
	w.batch()
	r.poll(t)
	rev := w.roster.Rev
	if got := w.eng.savedRevs(); !slices.Equal(got, []int64{rev}) {
		t.Fatalf("saved %v, want [%d]", got, rev)
	}
	w.eng.mu.Lock()
	content := w.eng.contents[0]
	w.eng.mu.Unlock()
	v, err := proto.Parse(content)
	check(t, err)
	saved, err := proto.DecodeRoster(v, server, teamName, w.main)
	check(t, err)
	if saved.Rev != rev || saved.Name != w.roster.Name {
		t.Errorf("saved roster %+v", saved)
	}
	// A suspend starts the session again, which verifies the same rev.
	w.clk.Suspend(5 * time.Second)
	w.say(alice, human("after the suspend", user(me)))
	r.poll(t)
	w.batch()
	r.poll(t)
	if got := w.eng.savedRevs(); len(got) != 1 {
		t.Errorf("the same rev saved again: %v", got)
	}
	next := w.roster
	next.Name = "Alpha renamed"
	w.writeRoster(next)
	r.poll(t)
	if got := w.eng.savedRevs(); !slices.Equal(got, []int64{rev, rev + 1}) {
		t.Errorf("saved %v, want [%d %d]", got, rev, rev+1)
	}
}

// n3d-D3: a roster cache write that fails is said and blocks nothing.
func TestN3dD3AFailedSaveBlocksNothing(t *testing.T) {
	w := newWorld(t)
	w.eng.saveErr = errors.New("disk full")
	r := w.start()
	w.batch()
	r.poll(t)
	if !strings.Contains(w.log.String(), "was not cached: disk full") {
		t.Errorf("log %q", w.log.String())
	}
}

// n3d-D4 (review A M3): a session under a pair other than the last recovery's (a token reissued
// within the run) recovers the sends again before its long-poll; the same pair does not.
func TestN3dD4ANewPairRecoversAgain(t *testing.T) {
	w := newWorld(t)
	r := w.start()
	w.batch()
	r.poll(t)
	old := w.client(me)
	token, err := w.srv.MintToken(user(me))
	check(t, err)
	w.replace(w.seat.Token(), []byte(token))
	check(t, old.Logout(t.Context()))
	w.tokens[me] = token
	w.say(alice, human("after the reissue", user(me)))
	w.batch()
	r.poll(t)
	if n, _ := w.eng.recoverCalls(); n != 2 {
		t.Errorf("recovers %d, want one per pair", n)
	}
}

// recoverCalls are RecoverSends' calls and the batches persisted at each.
func (e *fakeEngine) recoverCalls() (int, []int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.recovers, slices.Clone(e.persistedAtRecover)
}

// n3d-D4: RecoverSends is called once per start, after the verification and before anything is
// judged or polled; its transient failure is SY8's backoff and sends no long-poll.
func TestN3dD4RecoverSendsOncePerStart(t *testing.T) {
	w := newWorld(t)
	w.eng.recoverErr = errors.New("a transient fault")
	r := w.start()
	d := r.wait(t)
	w.noBatch()
	if len(r.polls) != 0 {
		t.Error("a long-poll after a failed recovery")
	}
	w.clk.Advance(d)
	w.batch()
	r.poll(t)
	if n, at := w.eng.recoverCalls(); n != 2 || !slices.Equal(at, []int{0, 0}) {
		t.Errorf("recovers %d, persisted at each %v", n, at)
	}
	w.clk.Suspend(5 * time.Second)
	w.say(alice, human("after the suspend", user(me)))
	r.poll(t)
	w.batch()
	if n, at := w.eng.recoverCalls(); n != 2 {
		t.Errorf("a new session recovered again: %d calls (%v)", n, at)
	}
}
