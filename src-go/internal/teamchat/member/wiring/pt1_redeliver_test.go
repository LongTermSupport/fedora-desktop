//go:build busfailpoints

package wiring

import (
	"bytes"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
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
func (r *pt1Runner) recvFail() {
	r.t.Helper()
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
	held := r.copiesByID()
	for _, id := range ids {
		r.marked[id] = held[id]
	}
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
