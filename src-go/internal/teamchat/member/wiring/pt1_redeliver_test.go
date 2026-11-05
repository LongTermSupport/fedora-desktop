//go:build busfailpoints

package wiring

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// A redelivery that reaches the transcript in gate time (SPEC-n3 PT1, P2): a recv hands items
// out and its output fails, so the client does not acknowledge, the link returns them to pending
// marked repeat, and the next socket delivery of them must carry the `repeat:` line. No fault of
// the fake Claude Code can do it: the link's one write of a delivery completes into the socket
// buffer before the fake reads it, so a transcript never holds two copies of an item the link
// delivered once, and the `awaiting` outcome of a crash is 180 s of quiet.

// errPT1Stdout is what a failing recv's stdout fails with.
var errPT1Stdout = errors.New("pt1: the recv's stdout fails")

// pt1FailingOut is the stdout of a recv whose output fails: it keeps what it is given, as a
// reader of a pipe would have read it, and fails the write.
type pt1FailingOut struct{ got bytes.Buffer }

func (o *pt1FailingOut) Write(p []byte) (int, error) {
	if _, err := o.got.Write(p); err != nil {
		return 0, err
	}
	return 0, errPT1Stdout
}

// getenv is the environment of a client verb the session's CMD would run, as exec gives it.
func (s *session) getenv() func(string) string {
	env := map[string]string{}
	for _, kv := range s.w.env(paths.EnvRuntime + "=" + s.runtime) {
		if name, value, ok := strings.Cut(kv, "="); ok {
			env[name] = value
		}
	}
	return func(name string) string { return env[name] }
}

// recvFail is recv, run in this process with a stdout that fails: what it handed out is printed
// to nobody, never acknowledged, and returned by the link. Each such item is marked with the
// number of its copies the transcript holds now. A recv that handed nothing out is no fault.
// The mark's record (pt1Mark) is the store read before the recv runs and again just before the
// transcript is read for the count, each at its own sequence number.
func (r *pt1Runner) recvFail() {
	r.t.Helper()
	before, beforeSeq, beforeErr := r.storeAt()
	out, errOut := &pt1FailingOut{}, &bytes.Buffer{}
	code := cli.Run(cli.Env{
		Args: []string{"recv", "--team", teamName}, Stdin: strings.NewReader(""), Stdout: out, Stderr: errOut,
		Digest: testDigest, Getenv: r.s.getenv(), Verbs: Verbs(),
	})
	ids := pt1IDs(out.got.Bytes())
	if len(ids) == 0 {
		return
	}
	if code == term.ExitOK {
		r.t.Fatalf("PT1: a recv whose output fails handed out %v and exited 0 (stderr %q)", ids, errOut.String())
	}
	r.mark(ids, func(id string) pt1Snap { return pt1SnapOf(beforeSeq, before, beforeErr, id) })
}

// mark marks ids with the copies the transcript holds now, recording the store read just before
// the transcript, and before, each id's snapshot taken before the hand-out.
func (r *pt1Runner) mark(ids []string, before func(string) pt1Snap) {
	r.t.Helper()
	at, atSeq, atErr := r.storeAt()
	held := r.copiesByID()
	for _, id := range ids {
		r.marked[id] = held[id]
		r.marks[id] = pt1Mark{copies: held[id], before: before(id), at: pt1SnapOf(atSeq, at, atErr, id)}
	}
}

// storeAt reads the team's store at the next sequence number. A store it cannot read is part of
// the record, not a failure of the run: the record never changes what the run is judged on.
func (r *pt1Runner) storeAt() (state.Store, int, error) {
	r.seq++
	var st state.Store
	data, err := os.ReadFile(r.w.storePath())
	if err == nil {
		err = json.Unmarshal(data, &st)
	}
	return st, r.seq, err
}

// pt1SnapOf is the snapshot of st for id, read at seq (err when it was not read): the item's state
// in pending and the ledger, and the deliveries in flight holding it (awaiting, or missed).
func pt1SnapOf(seq int, st state.Store, err error, id string) pt1Snap {
	if err != nil {
		return pt1Snap{seq: seq, err: err.Error()}
	}
	item := "not pending"
	for _, p := range st.Pending {
		if p.ID != id {
			continue
		}
		item = "pending"
		if p.Repeat {
			item += " repeat"
		}
		if p.Handed != nil {
			item += ", handed to " + *p.Handed
		}
	}
	if slices.ContainsFunc(st.Ledger, func(l state.Ledger) bool { return l.ID == id }) {
		item += ", in the ledger"
	}
	inFlight := []string{}
	for _, a := range st.Awaiting {
		if slices.Contains(a.IDs, id) {
			inFlight = append(inFlight, a.Nonce)
		}
	}
	for _, m := range st.Missed {
		if slices.Contains(m.IDs, id) {
			inFlight = append(inFlight, m.Nonce)
		}
	}
	return pt1Snap{seq: seq, read: true, item: item, inFlight: inFlight}
}

// copiesByID is how many copies of each item the transcript holds.
func (r *pt1Runner) copiesByID() map[string]int {
	r.t.Helper()
	out := map[string]int{}
	for _, c := range r.copies() {
		out[c.id]++
	}
	return out
}

// owes is whether the link owes the seat the item id: pending, not yet delivered.
func (r *pt1Runner) owes(id string) bool {
	r.t.Helper()
	return slices.Contains(pt1IDs(r.s.exec("inbox", "--team", teamName).Stdout), id)
}

// pt1RedeliveryTries is how many items the redelivery row offers a failing recv before it gives
// up: an item a delivery took first is not pending when the recv asks.
const pt1RedeliveryTries = 3

// pt1RowSeed numbers the texts of the redelivery row, which is no seed's schedule.
const pt1RowSeed uint64 = 1_000_001

// TestPT1Redelivery is P2 over a real run, which no drawn schedule is sure to reach: a first
// item is delivered (the link then waits its pace before the next), a second is posted and
// waited for until the link owes it, and a recv whose output fails hands it out and loses it.
// The link returns it marked repeat and delivers it again once the pace allows (about two
// seconds), and the check then judges the transcript's second copy. A recv that found nothing
// pending, because a delivery took the item first, is tried again with a new item, up to
// pt1RedeliveryTries times; a row that never handed one out fails, so it cannot pass vacuously.
func TestPT1Redelivery(t *testing.T) {
	r := newPT1Runner(t, pt1RowSeed, "")
	ops := []pt1Op{{pt1Say, 0}}
	r.do(ops[0])
	first := r.posted[0]
	eventually(t, func() bool { return r.copiesByID()[first] > 0 }, r.s.describe)
	for len(r.marked) == 0 {
		if len(ops) > 2*pt1RedeliveryTries {
			t.Fatalf("no item was pending for a failing recv in %d tries: %s", pt1RedeliveryTries, r.s.describe())
		}
		say := pt1Op{pt1Say, len(ops)}
		ops = append(ops, say, pt1Op{pt1RecvFail, len(ops) + 1})
		r.do(say)
		id := r.posted[len(r.posted)-1]
		eventually(t, func() bool { return r.owes(id) }, r.s.describe)
		r.recvFail()
	}
	for _, id := range slices.Sorted(maps.Keys(r.marked)) {
		eventually(t, func() bool { return r.copiesByID()[id] > r.marked[id] }, r.s.describe)
	}
	r.judge(ops)
}

// TestPT1MarkBeforeAnInFlightDelivery is issue 156's reading (b) forced over a real run, so the
// record the runner takes is shown to name it: the inbox held, an item's delivery is in flight
// (its awaiting entry in the store, its user line unread), the item is marked then, as a mark
// sampled before a delivery in flight lands, and the inbox released. P2 fails on the copy, with
// its first line as it always was, and reads (b); the same item marked once the copy has landed
// passes. No recv runs: the mark is the harness's, which is the point of the reading.
func TestPT1MarkBeforeAnInFlightDelivery(t *testing.T) {
	r := newPT1Runner(t, pt1RowSeed+1, "")
	ccAsk[bool](r.s, "hold")
	id := r.w.say(pt1Text(r.seed, pt1Op{pt1Say, 0}))
	r.posted = append(r.posted, id)
	eventually(t, func() bool { return slices.Contains(r.awaitingIDs(), id) }, r.s.describe)
	before, seq, err := r.storeAt()
	early := func(id string) pt1Snap { return pt1SnapOf(seq, before, err, id) }
	r.mark([]string{id}, early)
	ccAsk[bool](r.s, "release")
	eventually(t, func() bool { return r.copiesByID()[id] > 0 }, r.s.describe)
	r.drain()
	got := pt1Check(r.seen())
	first := "P2: " + id + " was returned to pending marked repeat and its copy 1 in the transcript carries no repeat line"
	if len(got) != 1 || !strings.HasPrefix(got[0], first+"\n") || !strings.Contains(got[0], string(pt1Harness)) {
		t.Fatalf("findings %q, want one, %q read as %q", got, first, pt1Harness)
	}
	t.Logf("the finding a mark before an in-flight delivery prints:\n%s", got[0])
	r.mark([]string{id}, early)
	if got := pt1Check(r.seen()); len(got) != 0 {
		t.Fatalf("marked after its copy landed: findings %q", got)
	}
}
