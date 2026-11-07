//go:build busfailpoints

package wiring

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/testfiles"
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

// pt1Fixed are the seeds the gate runs: a few seconds each. The first eight are the point rows'
// (TestPT1Points runs row i on seed i), seeds whose schedules end in none of the removal and the
// endings, so that a row's armed failpoint is the only fault of its run beside the schedule's own;
// the rest draw the removal alone, with the end, with the death of CMD, the two endings alone,
// and the recv whose output fails (the last four, which hand items out in a run with the inbox
// refused and before a removal; TestPT1Redelivery is the row that hands one out on a live inbox
// every time). Between them they draw every kind.
var pt1Fixed = []uint64{7, 15, 27, 38, 47, 55, 56, 71, 92, 95, 100, 108, 42, 109, 150, 35, 11, 24, 67, 57, 5, 40}

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

// pt1Run is one seed's run, with no failpoint armed.
func pt1Run(t *testing.T, seed uint64) {
	t.Helper()
	pt1RunAt(t, seed, "")
}

// TestPT1Points is a SIGKILL at each failpoint a single-team session can reach, one row each,
// named by the point without its "link-" (a row's name is in its temporary directory, and a
// socket's path may not pass 107 bytes): the link's first process arms the point, the row's
// provoker makes it fire (and waits for the kill), and the seed's schedule then runs against the
// restarted link before the same drain and checks as any run. A row whose point never fired
// fails, so none passes vacuously.
func TestPT1Points(t *testing.T) {
	for i, point := range slices.Sorted(maps.Keys(pt1Provokers)) {
		// Row i runs the i-th fixed seed, in the sorted order of the points, unless -pt1.seed names another.
		seed := pt1Fixed[i]
		if *pt1Only != 0 {
			seed = *pt1Only
		}
		t.Run(pt1RowName(point), func(t *testing.T) { pt1RunAt(t, seed, point) })
	}
}

// pt1RowName is a point's row name.
func pt1RowName(point failpoint.Name) string { return strings.TrimPrefix(string(point), "link-") }

// pt1RunAt is seed's run with point armed in the link, "" for none.
func pt1RunAt(t *testing.T, seed uint64, point failpoint.Name) {
	t.Helper()
	ops := pt1Schedule(seed)
	t.Logf("PT1 seed %d, failpoint %q: %v", seed, point, ops)
	defer func() {
		if t.Failed() {
			t.Logf("PT1 seed %d failed; replay it with: %s", seed, pt1Replay(seed, point))
		}
	}()
	r := newPT1Runner(t, seed, point)
	if point != "" {
		pt1Provokers[point](r)
		r.s.seatedIn()
		if !r.s.linkKilled() {
			t.Fatalf("the failpoint %s never fired: %s", point, r.s.describe())
		}
	}
	for _, op := range ops {
		r.do(op)
		if !op.kind.terminal() {
			r.s.oneLink()
		}
	}
	r.judge(ops)
}

// newPT1Runner is a run's world with a --claude session seated on the desired membership, and
// point armed in its first link, "" for none.
func newPT1Runner(t *testing.T, seed uint64, point failpoint.Name) *pt1Runner {
	t.Helper()
	w := newWorld(t, fakeServer(t))
	if point != "" {
		w.arm = arm("link", point)
	}
	return &pt1Runner{t: t, seed: seed, w: w, s: w.claudeSeated(w.desiredFlags(state.InboundAccept)...), recv: map[string]int{}, marked: map[string]int{}, marks: map[string]pt1Mark{}}
}

// judge drains the run and fails the test with every check ops broke.
func (r *pt1Runner) judge(ops []pt1Op) {
	r.t.Helper()
	r.drain()
	seen := r.seen()
	r.t.Logf("PT1 seed %d saw: %d items posted, %d copies by recv, %d in the transcript, %d sends, %d link crashes; removed %v (%d posted after), want %q, notices %v, %d handed out and returned",
		r.seed, len(seen.posted), pt1Total(seen.recv), len(seen.transcript), len(seen.sends), seen.crashes, seen.removed, len(seen.afterRemoval), seen.want, seen.notices, len(seen.marked))
	if bad := pt1Check(seen); len(bad) > 0 {
		r.t.Fatalf("PT1 seed %d broke %d checks:\n%s\nschedule: %v\n%s", r.seed, len(bad), strings.Join(bad, "\n"), ops, r.s.describe())
	}
}

// pt1Replay is the command that runs seed again, at point when there is one.
func pt1Replay(seed uint64, point failpoint.Name) string {
	run := "'TestPT1Seeds$'"
	if point != "" {
		run = "'TestPT1Points/" + pt1RowName(point) + "$'"
	}
	return fmt.Sprintf("go test -race -tags busfailpoints -run %s ./internal/teamchat/member/wiring/ -pt1.seed=%d", run, seed)
}

// pt1Provokers make each reachable failpoint fire, and wait for the kill. The link's points of a
// delivery fire at the first delivery (the first say, with the inbox live); a hand-out's fire at
// the first recv that takes an item, which the inbox's refusal leaves to recv alone; a send's
// fire at the first send; the first batch's fires as the link starts.
var pt1Provokers = map[failpoint.Name]func(*pt1Runner){
	failpoint.LinkAfterJudgeBeforePersist: func(r *pt1Runner) { r.s.awaitLinkKilled(false) },
	failpoint.LinkAfterAuth:               (*pt1Runner).provokeDelivery,
	failpoint.LinkAfterAwaiting:           (*pt1Runner).provokeDelivery,
	failpoint.LinkAfterUserLine:           (*pt1Runner).provokeDelivery,
	failpoint.LinkAfterHandOut:            (*pt1Runner).provokeHandOut,
	failpoint.LinkWhileHanded:             (*pt1Runner).provokeHandOut,
	failpoint.LinkAfterChargeBeforeSend:   (*pt1Runner).provokeSend,
	failpoint.LinkAfterSendBeforeRecord:   (*pt1Runner).provokeSend,
}

// pt1Unreached is each failpoint no row of TestPT1Points makes fire, and why.
var pt1Unreached = map[failpoint.Name]string{
	failpoint.LinkAfterMissing:        "a delivery is missing after 180 s of quiet or an hour, which no real-process run reaches without a clock seam the link does not have: the engine's fake-clock tests own it",
	failpoint.LinkBetweenStoreWrites:  "it needs two stores written by one hand-out, so two teams in one session: the engine's tests own it",
	failpoint.LinkInRemove:            "a removal: its row is TestFailpointInRemove (P4, P5)",
	failpoint.LinkAfterRemoveAnswered: "a removal: its row is TestFailpointAfterRemoveAnswered (P4, P5)",
	failpoint.KeeperBeforeStoreDelete: "a removal that times out: its row is TestFailpointKeeperBeforeStoreDelete (P4's pinned exception)",
	failpoint.LinkMidEndSends:         "a session's end: its row is TestFailpointMidEndSends (P5)",
	failpoint.KeeperBeforeEnd:         "a session's end: its row is TestFailpointKeeperBeforeEnd (P5)",
	failpoint.KeeperLinkWriting:       "a keeper kill at its link's first state: its row is TestFailpointKeeperLinkWriting (P1, P6)",
}

// provokeDelivery posts a human's item and waits for the link to be killed delivering it.
func (r *pt1Runner) provokeDelivery() {
	r.posted = append(r.posted, r.w.say(pt1Text(r.seed, pt1Op{pt1Say, -1})))
	r.s.awaitLinkKilled(false)
}

// provokeHandOut refuses the inbox, so that no delivery takes the item, posts one and receives
// until the link is killed handing it out.
func (r *pt1Runner) provokeHandOut() {
	ccAsk[bool](r.s, "refuse")
	r.posted = append(r.posted, r.w.say(pt1Text(r.seed, pt1Op{pt1Say, -1})))
	counts := r.s.awaitLinkKilled(true)
	for _, id := range slices.Sorted(maps.Keys(counts)) {
		r.recv[id] += counts[id]
	}
}

// provokeSend makes one send and waits for the link to be killed sending it.
func (r *pt1Runner) provokeSend() {
	r.send(pt1Op{pt1Send, -1})
	r.s.awaitLinkKilled(false)
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
	// removed is whether the seat was removed, atRemoval how many item copies the transcript held
	// then, limbo the ids an awaiting entry held (the store is deleted with the seat), and
	// afterRemoval the ids posted to main after it (pt1_end_test.go).
	removed      bool
	atRemoval    int
	limbo        []string
	afterRemoval []string
	storeBack    bool
	// ended is whether the run ended (the keeper's SIGTERM, CMD's SIGKILL); links are the pids of
	// the links the keeper started, read before the end because the runtime directory goes with it;
	// want is the one notice the ending or the removal posts.
	ended bool
	links []int
	want  string
	// marked is each id a recv whose output failed handed out, and the link got back unacknowledged
	// and marked repeat, with how many copies the transcript held then (pt1_redeliver_test.go).
	marked map[string]int
	// seq is the harness's own sequence: one number per store read of a mark and per transcript
	// copy the first time a read holds it (firstSeen, by place); marks is each mark's record.
	seq       int
	firstSeen []int
	marks     map[string]pt1Mark
}

// pt1PeerShort is the peer's short name, which a send names it by (P69).
const pt1PeerShort = "myrepo.3"

func (r *pt1Runner) do(op pt1Op) {
	r.t.Helper()
	switch op.kind {
	case pt1Say:
		r.post(r.w.say(pt1Text(r.seed, op)))
	case pt1Peer:
		r.post(r.w.peerText(pt1Text(r.seed, op)))
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
	case pt1Remove:
		r.remove()
	case pt1End, pt1Die:
		r.end(op.kind)
	case pt1RecvFail:
		r.recvFail()
	case pt1Restart:
		check(r.t, r.w.hs.fake.Restart())
	case pt1KillLink:
		r.killLink()
	}
}

// post records the id of an item a human or a peer posted: owed to the seat's agent, or, once the
// seat is removed, one P4 holds that nothing delivers.
func (r *pt1Runner) post(id string) {
	if r.removed {
		r.afterRemoval = append(r.afterRemoval, id)
		return
	}
	r.posted = append(r.posted, id)
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

// copies is the transcript's item copies, in the order the fake wrote them. It reads the file the
// fake keeps, not the fake's answer, because the fake is gone after an ending; only the lines
// whole so far are read, up to the last newline. Each copy a read holds for the first time takes
// the next sequence number (the transcript only grows, so a copy keeps its place).
func (r *pt1Runner) copies() []pt1Copy {
	r.t.Helper()
	data, err := testfiles.Try(filepath.Dir(r.s.transcript), filepath.Base(r.s.transcript))
	check(r.t, err)
	text := string(data)
	text = text[:strings.LastIndexByte(text, '\n')+1]
	copies, err := pt1Copies(text)
	check(r.t, err)
	for len(r.firstSeen) < len(copies) {
		r.seq++
		r.firstSeen = append(r.firstSeen, r.seq)
	}
	return copies
}

// drain is the end of a run that goes on: the faults that would hold it for ever ended and what
// must settle waited for, as rest does; for a run whose seat was removed, the leave; and for a
// run that ended, nothing: its ending has waited for its own notice.
func (r *pt1Runner) drain() {
	r.t.Helper()
	switch {
	case r.ended:
	case r.removed:
		r.leaveDone()
	default:
		r.rest()
	}
}

// rest is the run at rest: the faults that would hold it for ever ended, and everything that
// must settle settled: every accepted item shown by recv or the transcript, nothing owed or
// queued, every answered send in main. Each wait is on its condition, with a recv meanwhile that
// is itself a channel under test.
func (r *pt1Runner) rest() {
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

// awaitingIDs are the ids the team's store holds in an awaiting entry.
func (r *pt1Runner) awaitingIDs() []string {
	r.t.Helper()
	var ids []string
	for _, a := range r.w.store().Awaiting {
		ids = append(ids, a.IDs...)
	}
	return ids
}

// allShown receives once, and is whether every accepted item has a copy somewhere, or is in an
// awaiting entry (see pt1Seen.awaiting).
func (r *pt1Runner) allShown() bool {
	r.take(r.s.recvAny())
	seen := map[string]bool{}
	for _, c := range r.copies() {
		seen[c.id] = true
	}
	limbo := r.awaitingIDs()
	for _, id := range r.posted {
		if r.recv[id] == 0 && !seen[id] && !slices.Contains(limbo, id) {
			return false
		}
	}
	return true
}

// settled is whether the link owes nothing but what an awaiting entry holds, and queues nothing,
// for the seat's team.
func (r *pt1Runner) settled() bool {
	owed := r.s.exec("inbox", "--team", teamName)
	queued := r.s.exec("status", "--team", teamName)
	limbo := r.awaitingIDs()
	for _, id := range pt1IDs(owed.Stdout) {
		if !slices.Contains(limbo, id) {
			return false
		}
	}
	return owed.ExitCode == 0 && queued.ExitCode == 0 && !strings.Contains(string(queued.Stdout), `"kind":"queued"`)
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
	links := r.links
	if !r.ended {
		links = r.s.links()
	}
	alive := 0
	for _, pid := range links {
		if !gone(pid) {
			alive++
		}
	}
	inMain := map[string]int{}
	for _, s := range r.sends {
		inMain[s.text] = len(r.w.textsFrom(me, s.text))
	}
	awaiting := r.limbo
	if !r.removed {
		awaiting = r.awaitingIDs()
	}
	// Read before the literal: the read adds to firstSeen, which the literal takes.
	transcript := r.copies()
	return pt1Seen{
		posted: r.posted, recv: r.recv, transcript: transcript, awaiting: awaiting, crashes: max(len(links)-1, 0), linksAlive: alive,
		notices: notices, want: r.want, sends: r.sends, inMain: inMain,
		removed: r.removed, atRemoval: r.atRemoval, afterRemoval: r.afterRemoval, storeBack: r.storeBack, marked: r.marked,
		marks: r.marks, firstSeen: r.firstSeen,
	}
}
