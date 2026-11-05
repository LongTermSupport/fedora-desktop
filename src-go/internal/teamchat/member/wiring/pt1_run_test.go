//go:build busfailpoints

package wiring

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
)

// PT1 over real processes (SPEC-n3 PT1, B 2.15, handoff section 15): a seed draws a schedule
// (pt1_model_test.go) of sync batches (a human's and a peer's posts), recv and wait, sends, socket
// faults (the fake Claude Code's inbox held, released or refusing), homeserver restarts and a
// SIGKILL of the link; the schedule runs against the world's real keeper and link, with the fake
// Claude Code as CMD; then the run is drained and pt1Check judges what it saw.
//
// What it does not decide: the processes' own scheduling. A seed fixes the operations and their
// order, not which of the link's goroutines wins a race with a recv, so a seed is a replay of the
// schedule and the faults, not of the interleaving below them; a failing seed usually fails
// again, and the schedule it prints is the thing to read.

var (
	pt1Only   = flag.Uint64("pt1.seed", 0, "run only this PT1 seed: a failing run printed it, and its replay is this flag")
	pt1Random = flag.Int("pt1.random", 0, "run this many further PT1 seeds, each drawn at random and printed (a manual target, not the gate's)")
)

// pt1Fixed are the seeds the gate runs: about two seconds each.
var pt1Fixed = []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

func TestPT1Seeds(t *testing.T) {
	seeds := pt1Fixed
	if *pt1Only != 0 {
		seeds = []uint64{*pt1Only}
	}
	for _, seed := range seeds {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) { pt1Run(t, seed) })
	}
}

// TestPT1Random is the long run: -pt1.random=<n> runs n seeds drawn at random, each printed
// before it runs so that a failure names it.
func TestPT1Random(t *testing.T) {
	if *pt1Random <= 0 {
		t.Skip("PT1's long run is manual: -pt1.random=<n>")
	}
	for range *pt1Random {
		var word [8]byte
		if _, err := rand.Read(word[:]); err != nil {
			t.Fatal(err)
		}
		seed := binary.LittleEndian.Uint64(word[:]) | 1
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) { pt1Run(t, seed) })
	}
}

// pt1Run is one seed's run.
func pt1Run(t *testing.T, seed uint64) {
	t.Helper()
	ops := pt1Schedule(seed)
	t.Logf("PT1 seed %d: %v", seed, ops)
	defer func() {
		if t.Failed() {
			t.Logf("PT1 seed %d failed; replay it with: go test -race -tags busfailpoints -run 'TestPT1Seeds$' ./internal/teamchat/member/wiring/ -pt1.seed=%d", seed, seed)
		}
	}()
	w := newWorld(t, fakeServer(t))
	r := &pt1Runner{t: t, seed: seed, w: w, s: w.claudeSeated(w.desiredFlags(state.InboundAccept)...), recv: map[string]int{}}
	for _, op := range ops {
		r.do(op)
		r.s.oneLink()
	}
	r.drain()
	seen := r.seen()
	t.Logf("PT1 seed %d saw: %d items posted, %d copies by recv, %d in the transcript, %d sends, %d link crashes", seed, len(seen.posted), pt1Total(seen.recv), len(seen.transcript), len(seen.sends), seen.crashes)
	if bad := pt1Check(seen); len(bad) > 0 {
		t.Fatalf("PT1 seed %d broke %d checks:\n%s\nschedule: %v\n%s", seed, len(bad), strings.Join(bad, "\n"), ops, r.s.describe())
	}
}

// pt1Runner is a run in progress: the world, its one session, and what the operations have seen.
type pt1Runner struct {
	t    *testing.T
	seed uint64
	w    *world
	s    *session
	// posted are the ids the homeserver accepted for the seat, recv the copies recv and wait printed.
	posted []string
	recv   map[string]int
	sends  []pt1Sent
	held   bool
}

// pt1PeerShort is the peer's short name, which a send names it by (P69).
const pt1PeerShort = "myrepo.3"

func (r *pt1Runner) do(op pt1Op) {
	r.t.Helper()
	switch op.kind {
	case pt1Say:
		r.posted = append(r.posted, r.w.say(pt1Text(r.seed, op)))
	case pt1Peer:
		r.posted = append(r.posted, r.w.peerText(pt1Text(r.seed, op)))
	case pt1Recv:
		r.take(r.s.recvAny())
	case pt1Wait:
		r.take(pt1IDs(r.s.exec("wait", "--team", teamName, "--timeout", "1").Stdout))
	case pt1Send:
		r.send(op)
	case pt1Hold, pt1Release:
		r.held = op.kind == pt1Hold
		ccAsk[bool](r.s, pt1Names[op.kind])
	case pt1Refuse:
		ccAsk[bool](r.s, pt1Names[op.kind])
	case pt1Restart:
		check(r.t, r.w.hs.fake.Restart())
	case pt1KillLink:
		r.killLink()
	}
}

// take counts the copies recv or wait printed.
func (r *pt1Runner) take(ids []string) {
	for _, id := range ids {
		r.recv[id]++
	}
}

// pt1Total is the sum of a count by id.
func pt1Total(counts map[string]int) int {
	n := 0
	for _, c := range counts {
		n += c
	}
	return n
}

// pt1IDs are the ids of the records a verb printed, whatever its exit.
func pt1IDs(stdout []byte) []string {
	var ids []string
	for line := range strings.SplitSeq(string(stdout), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if id, ok := rec["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// send says text to the peer as the seat's agent would; a send the link was not there to take
// (its exit is not 0) is recorded as not answered, and may still reach main once.
func (r *pt1Runner) send(op pt1Op) {
	text := pt1Text(r.seed, op)
	res := r.s.send(&text, "say", "--to", pt1PeerShort, "--team", teamName)
	answered := false
	if res.ExitCode == 0 {
		recs := parseRecords(r.t, res.Stdout)
		answered = len(recs) == 1 && recs[0]["kind"] == "sent"
	}
	r.sends = append(r.sends, pt1Sent{text: text, answered: answered})
}

// killLink is SIGKILL of the link the keeper started last; one already gone is no error.
func (r *pt1Runner) killLink() {
	if err := syscall.Kill(r.s.lastLink(), syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		r.t.Fatal(err)
	}
}

// copies is the transcript's item copies, in the order the fake wrote them.
func (r *pt1Runner) copies() []pt1Copy {
	r.t.Helper()
	copies, err := pt1Copies(ccAsk[string](r.s, "transcript"))
	check(r.t, err)
	return copies
}

// drain ends the faults that would hold a run for ever, and waits for what must settle: every
// accepted item shown by recv or the transcript, nothing owed or queued, every answered send in
// main. Each wait is on its condition, with a recv meanwhile that is itself a channel under test.
func (r *pt1Runner) drain() {
	r.t.Helper()
	if r.held {
		r.held = false
		ccAsk[bool](r.s, "release")
	}
	r.s.seatedIn()
	eventually(r.t, r.allShown, r.s.describe)
	eventually(r.t, r.settled, r.s.describe)
	within(r.t, backoffCap+settle, r.sendsPosted, r.s.describe)
}

// allShown receives once, and is whether every accepted item has a copy somewhere.
func (r *pt1Runner) allShown() bool {
	r.take(r.s.recvAny())
	seen := map[string]bool{}
	for _, c := range r.copies() {
		seen[c.id] = true
	}
	for _, id := range r.posted {
		if r.recv[id] == 0 && !seen[id] {
			return false
		}
	}
	return true
}

// settled is whether the link owes nothing and queues nothing for the seat's team.
func (r *pt1Runner) settled() bool {
	owed := r.s.exec("inbox", "--team", teamName)
	queued := r.s.exec("status", "--team", teamName)
	return owed.ExitCode == 0 && len(pt1IDs(owed.Stdout)) == 0 && queued.ExitCode == 0 && !strings.Contains(string(queued.Stdout), `"kind":"queued"`)
}

// sendsPosted is whether every send the verb answered is in main.
func (r *pt1Runner) sendsPosted() bool {
	for _, s := range r.sends {
		if s.answered && len(r.w.textsFrom(me, s.text)) == 0 {
			return false
		}
	}
	return true
}

// seen is the run's observations once it is drained.
func (r *pt1Runner) seen() pt1Seen {
	r.t.Helper()
	notices := map[string]int{}
	for _, n := range r.w.lifecycle() {
		notices[n]++
	}
	alive := 0
	links := r.s.links()
	for _, pid := range links {
		if !gone(pid) {
			alive++
		}
	}
	inMain := map[string]int{}
	for _, s := range r.sends {
		inMain[s.text] = len(r.w.textsFrom(me, s.text))
	}
	return pt1Seen{
		posted: r.posted, recv: r.recv, transcript: r.copies(), crashes: max(len(links)-1, 0), linksAlive: alive,
		notices: notices, sends: r.sends, inMain: inMain,
	}
}
