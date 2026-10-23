package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/faketranscript"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/inbox"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/proof"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// pworld is a delivery world whose session records into a fake transcript.
type pworld struct {
	*dworld

	tr *faketranscript.Transcript
}

func newProofWorld(t *testing.T) *pworld {
	t.Helper()
	dw := newDeliveryWorld(t, state.DeliverySocket)
	tr, err := faketranscript.New(t.TempDir(), "session-1", dw.clk)
	check(t, err)
	dw.transcript = tr.Path()
	return &pworld{dworld: dw, tr: tr}
}

// deliver persists items for team a and delivers them with the session idle or busy; it is
// the delivery's text once its user line is written in full.
func (pw *pworld) deliver(a paths.TeamID, idle bool, items ...team.Item) string {
	pw.t.Helper()
	pw.persist(a, items...)
	pw.facts(idle)
	ev := pw.next()
	nonce := nonceOf(pw.t, ev.text)
	pw.until(func() bool { return writtenOf(pw.e.teams[a].store, nonce) == state.WrittenYes })
	return ev.text
}

func writtenOf(s *state.Store, nonce string) string {
	for _, a := range s.Awaiting {
		if a.Nonce == nonce {
			return a.Written
		}
	}
	return ""
}

// settled waits until team a's store has no awaiting entry left.
func (pw *pworld) settled(a paths.TeamID) {
	pw.t.Helper()
	pw.until(func() bool { return len(pw.e.teams[a].store.Awaiting) == 0 })
}

// grown waits until the proof has seen the transcript grow at the clock's boot time now.
func (pw *pworld) grown(path string) {
	pw.t.Helper()
	pw.until(func() bool {
		m := pw.e.proof.marks[path]
		return m != nil && m.grew && m.grewAt == pw.clk.Boot()
	})
}

// recvIDs is one recv's items, acknowledged: the ids it showed.
func (pw *pworld) recvIDs() []string {
	pw.t.Helper()
	d, out := pw.serve(pw.t.Context(), wire.OpRecv, wire.TeamsArgs{})
	if d.Exit != 0 {
		pw.t.Fatalf("recv = %+v", d)
	}
	if d.Ack != "" {
		check(pw.t, pw.e.Ack(pw.t.Context(), d.Ack))
	}
	var ids []string
	for _, r := range out.recs {
		switch r := r.(type) {
		case records.Human:
			ids = append(ids, r.ID)
		case records.Peer:
			ids = append(ids, r.ID)
		}
	}
	return ids
}

// quietFor moves the clock by d with only the timers armed (status.json's and the proof's).
func (pw *pworld) quietFor(d time.Duration) {
	pw.t.Helper()
	pw.armed(0)
	pw.clk.Advance(d)
}

// change rewrites team a's store with f, its content kept for the items given.
func (pw *pworld) change(a paths.TeamID, f func(s *state.Store), items ...team.Item) {
	pw.t.Helper()
	check(pw.t, pw.e.change(pw.t.Context(), a, func(h *held, s *state.Store) (bool, error) {
		f(s)
		for _, it := range items {
			h.content[it.Pending.ID] = it.Verdict
		}
		return true, nil
	}))
}

func ledgerIDs(s state.Store) []string {
	ids := make([]string, 0, len(s.Ledger))
	for _, l := range s.Ledger {
		ids = append(ids, l.ID)
	}
	return ids
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	check(t, err)
	return fi.Size()
}

// DV4, DV5: a delivery recorded idle (a `user` entry) or busy (a `queued_command`) is proven:
// its entry leaves awaiting, its items leave pending, its HUMAN items get their receipts, and
// the ledger keeps them.
func TestAProvenDeliveryLeavesAwaiting(t *testing.T) {
	for name, mode := range map[string]faketranscript.Mode{"idle": faketranscript.Idle, "busy": faketranscript.Busy} {
		t.Run(name, func(t *testing.T) {
			pw := newProofWorld(t)
			a := tid(t, "alpha")
			pw.live(a)
			text := pw.deliver(a, mode == faketranscript.Idle, humanItem(a, 1, startWall, "hello"), peerItem(a, 2, startWall+1, "peer"))
			pw.tr.Noise(2)
			pw.tr.Record(text, mode, 0)
			pw.settled(a)
			s := pw.store(a)
			if len(s.Pending) != 0 || len(s.Missed) != 0 {
				t.Fatalf("after the proof: %+v", s)
			}
			if !slices.Equal(ledgerIDs(s), []string{evID(1), evID(2)}) {
				t.Fatalf("the ledger: %+v", s.Ledger)
			}
			if len(s.Receipts) != 1 || s.Receipts[0] != (state.Receipt{ID: evID(1), Room: proto.PurposeMain}) {
				t.Fatalf("the receipts: %+v", s.Receipts)
			}
			check(t, pw.tr.Err())
		})
	}
}

// A user line cut part-way (written unknown) that the transcript shows is proven: its ids go
// into the ledger at the proof.
func TestAnUnknownEntryProvenGoesIntoTheLedger(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	pw.live(a)
	pw.persist(a, humanItem(a, 1, startWall, "hello"))
	pw.dial.then(dialStep{write: fmt.Errorf("%w: 10 of 400 bytes", inbox.ErrPartial)})
	pw.facts(false)
	ev := pw.next()
	pw.until(func() bool { return writtenOf(pw.e.teams[a].store, nonceOf(t, ev.text)) == state.WrittenUnknown })
	pw.tr.Record(ev.text, faketranscript.Busy, 0)
	pw.settled(a)
	if s := pw.store(a); !slices.Equal(ledgerIDs(s), []string{evID(1)}) || len(s.Pending) != 0 {
		t.Fatalf("after proving an unknown entry: %+v", s)
	}
}

// P8, busy then recorded: a session in one tool call for 10 minutes writes nothing, then
// records the queued delivery: proven, never missing, and recv never shows its items.
func TestBusyThenRecorded(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	pw.live(a)
	text := pw.deliver(a, false, humanItem(a, 1, startWall, "hello"))
	pw.quietFor(10 * time.Minute)
	if s := pw.store(a); len(s.Awaiting) != 1 || len(s.Missed) != 0 {
		t.Fatalf("a busy session 10 minutes on: %+v", s)
	}
	pw.tr.Record(text, faketranscript.Busy, 0)
	pw.settled(a)
	if s := pw.store(a); len(s.Missed) != 0 || len(s.Pending) != 0 {
		t.Fatalf("after the record: %+v", s)
	}
	if ids := pw.recvIDs(); len(ids) != 0 {
		t.Fatalf("recv showed %v", ids)
	}
	if n := pw.hitCount(failpoint.LinkAfterMissing); n != 0 {
		t.Fatalf("%d deliveries declared missing", n)
	}
}

// checkMissing is team a's store once its one delivery of evID(1) and evID(2) was declared
// missing: the entry in missed, the items pending again, repeat and held, none in the ledger.
func checkMissing(t *testing.T, s state.Store, nonce string) {
	t.Helper()
	if len(s.Awaiting) != 0 || len(s.Missed) != 1 || s.Missed[0].Nonce != nonce || !slices.Equal(s.Missed[0].IDs, []string{evID(1), evID(2)}) {
		t.Fatalf("the entries after missing: %+v, %+v", s.Awaiting, s.Missed)
	}
	if len(s.Ledger) != 0 || len(s.Pending) != 2 {
		t.Fatalf("the ledger and pending after missing: %+v, %+v", s.Ledger, s.Pending)
	}
	for _, p := range s.Pending {
		if p.Handed != nil || !p.Repeat || !p.Held {
			t.Fatalf("an item after missing: %+v", p)
		}
	}
}

// DV4's quiet clock: an entry is missing once the transcript has been quiet 180 s since the
// first Stop after the write; growth restarts the count.
func TestMissingAfter180sQuietSinceStop(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	pw.live(a)
	text := pw.deliver(a, false, humanItem(a, 1, startWall, "one"), humanItem(a, 2, startWall+1, "two"))
	nonce := nonceOf(t, text)
	pw.quietFor(5 * time.Minute)
	check(t, pw.e.SetSession(t.Context(), SessionFacts{Idle: true}))
	stop := pw.clk.Boot().Milliseconds()
	if s := pw.store(a); s.Awaiting[0].StopMS == nil || *s.Awaiting[0].StopMS != stop {
		t.Fatalf("the Stop after the write: %+v", s.Awaiting[0])
	}
	pw.quietFor(100 * time.Second)
	pw.tr.Noise(1)
	pw.grown(pw.tr.Path())
	pw.quietFor(quietAfter - time.Millisecond)
	if s := pw.store(a); len(s.Awaiting) != 1 {
		t.Fatalf("missing before 180 s quiet since the growth: %+v", s.Missed)
	}
	pw.quietFor(time.Millisecond)
	checkMissing(t, pw.store(a), nonce)
	if n := pw.hitCount(failpoint.LinkAfterMissing); n != 1 {
		t.Fatalf("%d hits of the missing failpoint", n)
	}
	if st := pw.status(); st.Teams[0].Missing != 1 || st.Teams[0].Pending != 2 {
		t.Fatalf("status.json after missing: %+v", st.Teams[0])
	}
	if !strings.Contains(pw.log.String(), "missing") {
		t.Fatalf("no log line: %s", pw.log.String())
	}
}

// An entry written while the session is idle has its quiet clock from the write.
func TestMissingAfter180sQuietFromAnIdleWrite(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	pw.live(a)
	text := pw.deliver(a, true, humanItem(a, 1, startWall, "one"), humanItem(a, 2, startWall+1, "two"))
	pw.quietFor(quietAfter)
	checkMissing(t, pw.store(a), nonceOf(t, text))
}

// DV4's cap: 1 h after the write the entry is missing whatever the session did.
func TestMissingAfterAnHour(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	pw.live(a)
	text := pw.deliver(a, false, humanItem(a, 1, startWall, "one"), humanItem(a, 2, startWall+1, "two"))
	for range 3 {
		pw.quietFor(20*time.Minute - time.Second)
		pw.tr.Noise(1)
		pw.grown(pw.tr.Path())
	}
	if s := pw.store(a); len(s.Awaiting) != 1 {
		t.Fatalf("missing before the hour: %+v", s)
	}
	pw.quietFor(3 * time.Second)
	checkMissing(t, pw.store(a), nonceOf(t, text))
	if ids := pw.recvIDs(); !slices.Equal(ids, []string{evID(1), evID(2)}) {
		t.Fatalf("recv after missing showed %v", ids)
	}
	if ids := pw.recvIDs(); len(ids) != 0 {
		t.Fatalf("a second recv showed %v", ids)
	}
}

// P9, found after missing: items still pending leave as proven (with receipts), items recv
// already showed are counted seen twice and logged, and nothing is shown a third time.
func TestALateFind(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	pw.live(a)
	two := humanItem(a, 2, startWall+1, "two")
	text := pw.deliver(a, true, humanItem(a, 1, startWall, "one"), two)
	pw.quietFor(quietAfter)
	checkMissing(t, pw.store(a), nonceOf(t, text))
	// recv shows the older item alone: the other is out of pending meanwhile, then put back.
	var kept state.Pending
	pw.change(a, func(s *state.Store) { kept, s.Pending = s.Pending[1], s.Pending[:1] })
	if ids := pw.recvIDs(); !slices.Equal(ids, []string{evID(1)}) {
		t.Fatalf("recv showed %v", ids)
	}
	pw.change(a, func(s *state.Store) { s.Pending = append(s.Pending, kept) }, two)
	pw.tr.Record(text, faketranscript.Busy, 0)
	pw.until(func() bool { return len(pw.e.teams[a].store.Missed) == 0 })
	s := pw.store(a)
	if len(s.Pending) != 0 || !slices.Contains(ledgerIDs(s), evID(2)) {
		t.Fatalf("after the late find: %+v", s)
	}
	if !slices.Contains(s.Receipts, state.Receipt{ID: evID(2), Room: proto.PurposeMain}) || slices.Contains(s.Receipts, state.Receipt{ID: evID(1), Room: proto.PurposeMain}) {
		t.Fatalf("the receipts: %+v", s.Receipts)
	}
	if ids := pw.recvIDs(); len(ids) != 0 {
		t.Fatalf("recv after the late find showed %v", ids)
	}
	d, out := pw.serve(t.Context(), wire.OpStatus, wire.TeamsArgs{})
	if d.Exit != 0 {
		t.Fatalf("status = %+v", d)
	}
	tr, ok := out.recs[0].(records.Team)
	if !ok || tr.Counts.SeenTwice != 1 {
		t.Fatalf("status's team record: %+v", out.recs[0])
	}
	if !strings.Contains(pw.log.String(), "seen twice") || !strings.Contains(pw.log.String(), evID(1)) {
		t.Fatalf("no seen-twice line: %s", pw.log.String())
	}
}

// A late find of items a recv holds when the needle appears: seen twice, and the recv's
// return cannot show them again.
func TestALateFindOfItemsARecvHolds(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	pw.live(a)
	text := pw.deliver(a, true, humanItem(a, 1, startWall, "one"), humanItem(a, 2, startWall+1, "two"))
	pw.quietFor(quietAfter)
	d, out := pw.serve(t.Context(), wire.OpRecv, wire.TeamsArgs{})
	if d.Ack == "" || len(out.recs) != 2 {
		t.Fatalf("recv = %+v, %s", d, out.kinds())
	}
	pw.tr.Record(text, faketranscript.Busy, 0)
	pw.until(func() bool { return len(pw.e.teams[a].store.Missed) == 0 })
	check(t, pw.e.Return(t.Context(), d.Ack))
	if ids := pw.recvIDs(); len(ids) != 0 {
		t.Fatalf("recv after the late find showed %v", ids)
	}
	var seen int64
	pw.local(func() { seen = pw.e.teams[a].seenTwice })
	if seen != 2 {
		t.Fatalf("seen twice = %d", seen)
	}
}

// P10 at the engine: a link killed right after the missing write (the failpoint) and started
// again has every item pending, repeat and held, none in the ledger, and recv shows each once.
func TestMissingSurvivesARestart(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	pw.live(a)
	text := pw.deliver(a, true, humanItem(a, 1, startWall, "one"), humanItem(a, 2, startWall+1, "two"))
	got := make(chan state.Store, 1)
	pw.setOnHit(func(n failpoint.Name) {
		if n != failpoint.LinkAfterMissing {
			return
		}
		f, err := pw.root.Read(pw.layout.Seat(a).Store(), 1<<22)
		if err != nil {
			t.Errorf("read the store at the failpoint: %v", err)
			return
		}
		s, err := state.DecodeStore(f.Data)
		if err != nil {
			t.Errorf("decode the store at the failpoint: %v", err)
			return
		}
		got <- s
	})
	pw.quietFor(quietAfter)
	killed := <-got
	checkMissing(t, killed, nonceOf(t, text))
	again := newDeliveryWorld(t, state.DeliverySocket)
	f := again.add(a, encodeStore(t, killed))
	<-f.running
	f.notices.set(roster(a))
	f.setVerdict(evID(1), humanItem(a, 1, startWall, "one").Verdict)
	f.setVerdict(evID(2), humanItem(a, 2, startWall+1, "two").Verdict)
	checkMissing(t, again.store(a), nonceOf(t, text))
	apw := &pworld{dworld: again}
	if ids := apw.recvIDs(); !slices.Equal(ids, []string{evID(1), evID(2)}) {
		t.Fatalf("the restarted link's recv showed %v", ids)
	}
	if ids := apw.recvIDs(); len(ids) != 0 {
		t.Fatalf("a second recv showed %v", ids)
	}
}

// A late find is watched for 24 h; an older missed entry is dropped.
func TestAMissedEntryIsDroppedAfter24h(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	pw.live(a)
	pw.deliver(a, true, humanItem(a, 1, startWall, "one"), humanItem(a, 2, startWall+1, "two"))
	pw.quietFor(quietAfter)
	pw.quietFor(lateFindFor - time.Millisecond)
	if s := pw.store(a); len(s.Missed) != 1 {
		t.Fatalf("the missed entry before 24 h: %+v", s.Missed)
	}
	pw.quietFor(time.Millisecond)
	if s := pw.store(a); len(s.Missed) != 0 || len(s.Pending) != 2 {
		t.Fatalf("after 24 h: %+v", s)
	}
	var armed bool
	pw.local(func() { armed = pw.e.proof.stop != nil })
	if armed {
		t.Fatal("a proof timer is armed with nothing to watch")
	}
}

// n3e-D11: status.json's missing is the missed entries since the team's last proof.
func TestAProofClearsTheMissingCount(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	pw.live(a)
	pw.deliver(a, true, humanItem(a, 1, startWall, "one"), humanItem(a, 2, startWall+1, "two"))
	pw.quietFor(quietAfter)
	if st := pw.status(); st.Teams[0].Missing != 1 {
		t.Fatalf("status.json after missing: %+v", st.Teams[0])
	}
	text := pw.deliver(a, false, humanItem(a, 3, startWall+2, "three"))
	pw.tr.Record(text, faketranscript.Idle, 0)
	pw.settled(a)
	if st := pw.status(); st.Teams[0].Missing != 0 || len(pw.store(a).Missed) != 1 {
		t.Fatalf("status.json after a later proof: %+v", st.Teams[0])
	}
}

// A /clear mid-proof: the session's transcript changes, and a delivery recorded in the new one
// is proven.
func TestAClearMidProof(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	pw.live(a)
	text := pw.deliver(a, false, humanItem(a, 1, startWall, "one"))
	next, err := pw.tr.Clear("session-2")
	check(t, err)
	check(t, pw.e.SetSession(t.Context(), SessionFacts{Transcript: next}))
	pw.tr.Record(text, faketranscript.Busy, 0)
	pw.settled(a)
	if s := pw.store(a); len(s.Pending) != 0 || len(s.Missed) != 0 {
		t.Fatalf("after the record in the new transcript: %+v", s)
	}
}

// A truncated or rotated transcript is scanned again from 0.
func TestATruncatedOrRotatedTranscript(t *testing.T) {
	for name, cut := range map[string]func(*faketranscript.Transcript) error{
		"truncated": (*faketranscript.Transcript).Truncate, "rotated": (*faketranscript.Transcript).Rotate,
	} {
		t.Run(name, func(t *testing.T) {
			pw := newProofWorld(t)
			a := tid(t, "alpha")
			pw.live(a)
			pw.tr.Noise(5)
			text := pw.deliver(a, false, humanItem(a, 1, startWall, "one"))
			check(t, cut(pw.tr))
			pw.until(func() bool {
				m := pw.e.proof.marks[pw.tr.Path()]
				return m != nil && m.mark.Offset == 0
			})
			pw.tr.Record(text, faketranscript.Busy, 0)
			pw.settled(a)
		})
	}
}

// deafWatcher is a watch that never tells a change.
type deafWatcher struct{ changes chan struct{} }

func (w deafWatcher) Changes() <-chan struct{} { return w.changes }
func (deafWatcher) Close() error               { return nil }

// A transcript cut and grown back past its mark between two scans looks unchanged to an
// offset: the deadline reads every transcript whole again before it declares anything missing,
// so the delivery is proven, never missing.
func TestACutUnseenIsFoundAtTheDeadline(t *testing.T) {
	dw := &dworld{dial: newFakeDialer()}
	dw.world = newWorldWith(t, state.DeliverySocket, func(c *Config) { c.Inbox = dw.dial }, func(e *Engine) {
		e.hit = dw.hit
		e.watch = func(string) (proof.Watcher, error) { return deafWatcher{make(chan struct{})}, nil }
	})
	tr, err := faketranscript.New(t.TempDir(), "session-1", dw.clk)
	check(t, err)
	dw.transcript = tr.Path()
	pw := &pworld{dworld: dw, tr: tr}
	a := tid(t, "alpha")
	pw.live(a)
	pw.tr.Noise(1)
	text := pw.deliver(a, false, humanItem(a, 1, startWall, "one"), humanItem(a, 2, startWall+1, "two"))
	size := fileSize(t, pw.tr.Path())
	pw.until(func() bool { m := pw.e.proof.marks[pw.tr.Path()]; return m != nil && m.mark.Offset == size })
	check(t, pw.tr.Truncate())
	pw.tr.Record(text, faketranscript.Busy, 0)
	check(t, pw.e.SetSession(t.Context(), SessionFacts{Idle: true}))
	pw.grown(pw.tr.Path())
	if s := pw.store(a); len(s.Awaiting) != 1 {
		t.Fatalf("the offset saw past the cut: %+v", s)
	}
	pw.quietFor(quietAfter)
	pw.settled(a)
	if s := pw.store(a); len(s.Missed) != 0 || len(s.Pending) != 0 {
		t.Fatalf("after the deadline's re-read: %+v", s)
	}
}

// n3e-D10: a restarted link scans each awaiting entry from its persisted offset, so a record
// made while no link ran is found, and a needle before the offset is not.
func TestARestartScansFromTheOffset(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	s := pw.newStore(a)
	s.Pending = []state.Pending{humanItem(a, 1, startWall, "").Pending, humanItem(a, 2, startWall+1, "").Pending}
	early, late := "0000000a", "0000000b"
	pw.tr.Record("teamchat delivery "+early+": an earlier copy", faketranscript.Idle, 0)
	size := fileSize(t, pw.tr.Path())
	pw.tr.Record("teamchat delivery "+late+": the delivery", faketranscript.Busy, 0)
	at := pw.clk.Boot().Milliseconds()
	s.Awaiting = []state.Awaiting{
		{Nonce: early, IDs: []string{evID(1)}, Transcript: state.TranscriptMark{Path: pw.tr.Path(), Offset: size}, AtMS: at, Written: state.WrittenYes},
		{Nonce: late, IDs: []string{evID(2)}, Transcript: state.TranscriptMark{Path: pw.tr.Path(), Offset: size}, AtMS: at, Written: state.WrittenYes},
	}
	s.Pending[0].Handed, s.Pending[1].Handed = &early, &late
	pw.add(a, encodeStore(t, s))
	pw.until(func() bool { return len(pw.e.teams[a].store.Awaiting) == 1 })
	got := pw.store(a)
	if got.Awaiting[0].Nonce != early || len(got.Pending) != 1 || got.Pending[0].ID != evID(1) {
		t.Fatalf("after the restart's scan: %+v", got)
	}
}

// earlierRunStore is a store of another run holding one awaiting entry (evID(1) and evID(2),
// one in the ledger), one held item and one missed entry.
func (pw *pworld) earlierRunStore(a paths.TeamID) state.Store {
	pw.t.Helper()
	s := pw.newStore(a)
	other, err := paths.ParseRunID("fedcba9876543210")
	check(pw.t, err)
	s.Run = state.StoreRun{ID: other, StartedMS: startWall - 1000}
	nonce := "0000000c"
	s.Pending = []state.Pending{
		humanItem(a, 1, startWall, "").Pending, humanItem(a, 2, startWall+1, "").Pending, humanItem(a, 3, startWall+2, "").Pending,
	}
	s.Pending[0].Handed, s.Pending[1].Handed = &nonce, &nonce
	s.Pending[2].Repeat, s.Pending[2].Held = true, true
	s.Awaiting = []state.Awaiting{{
		Nonce: nonce, IDs: []string{evID(1), evID(2)}, Transcript: state.TranscriptMark{Path: pw.tr.Path(), Offset: 0},
		AtMS: 5, Written: state.WrittenYes,
	}}
	s.Missed = []state.Missed{{Nonce: "0000000d", IDs: []string{evID(3)}, Offset: 4, AtMS: 3}}
	s.Ledger = []state.Ledger{{ID: evID(1), Room: proto.PurposeMain, Item: records.KindHuman, From: alice(a), AtMS: startWall}}
	return s
}

// checkEarlierRunReturned is the store once BeginRun took it: the awaiting items pending,
// repeat and not held, out of the ledger; no missed entry; the run this one.
func checkEarlierRunReturned(t *testing.T, s state.Store, run paths.RunID) {
	t.Helper()
	if s.Run.ID != run || len(s.Awaiting) != 0 || len(s.Missed) != 0 || len(s.Ledger) != 0 || len(s.Pending) != 3 {
		t.Fatalf("after BeginRun: %+v", s)
	}
	for _, p := range s.Pending[:2] {
		if p.Handed != nil || !p.Repeat || p.Held {
			t.Fatalf("an earlier run's awaiting item: %+v", p)
		}
	}
}

// 15b, 15c: a new run's BeginRun returns an earlier run's awaiting items to pending (repeat,
// not held: the new session never saw that socket), out of the ledger, and drops its missed
// entries, in BeginRun's one write; no delivery or proof uses that store before it. A crash
// before the write repeats it; one after it finds nothing to do.
func TestBeginRunReturnsAnEarlierRunsEntries(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	before := pw.earlierRunStore(a)
	f := pw.add(a, encodeStore(t, before))
	<-f.running
	f.notices.set(roster(a))
	for n := 1; n <= 3; n++ {
		f.setVerdict(evID(n), humanItem(a, n, startWall+int64(n-1), "again").Verdict)
	}
	pw.facts(true)
	pw.tr.Record("teamchat delivery 0000000c: the old copy", faketranscript.Busy, 0)
	pw.quietFor(2 * time.Hour)
	if s := pw.store(a); len(s.Awaiting) != 1 || len(s.Missed) != 1 || pw.dial.dialCount() != 0 {
		t.Fatalf("a store of an earlier run was used before BeginRun: %+v, %d dials", s, pw.dial.dialCount())
	}
	// A crash before the write: the next link's BeginRun does it all.
	crashed := newProofWorld(t)
	crashed.add(a, encodeStore(t, before))
	check(t, crashed.e.BeginRun(t.Context(), a))
	checkEarlierRunReturned(t, crashed.store(a), crashed.run)
	check(t, pw.e.BeginRun(t.Context(), a))
	after := pw.store(a)
	checkEarlierRunReturned(t, after, pw.run)
	if !strings.Contains(pw.log.String(), "earlier run") {
		t.Fatalf("no log line: %s", pw.log.String())
	}
	// A crash after the write: the next link finds the run its own and changes nothing.
	again := newProofWorld(t)
	again.add(a, encodeStore(t, after))
	check(t, again.e.BeginRun(t.Context(), a))
	if got := again.store(a); len(got.Pending) != 3 || got.Pending[0].Held || !got.Pending[2].Held || !got.Pending[0].Repeat {
		t.Fatalf("a second BeginRun changed the store: %+v", got)
	}
	// Once begun, the returned items go on the new socket, marked repeat.
	ev := pw.next()
	if !strings.Contains(ev.text, evID(1)) || !strings.Contains(ev.text, "repeat") {
		t.Fatalf("the delivery after BeginRun: %q", ev.text)
	}
}

// OF1: a team stopped beside a running one: the running team's delivery is proven while the
// stopped team's transcript is gone and its entry waits on its own clock.
func TestAStoppedTeamHoldsNoOthersProof(t *testing.T) {
	pw := newProofWorld(t)
	a, b := tid(t, "alpha"), tid(t, "bravo")
	pw.live(b)
	s := pw.newStore(a)
	nonce := "0000000e"
	s.Pending = []state.Pending{humanItem(a, 1, startWall, "").Pending}
	s.Pending[0].Handed = &nonce
	gone := filepath.Join(t.TempDir(), "gone", "old.jsonl")
	s.Awaiting = []state.Awaiting{{
		Nonce: nonce, IDs: []string{evID(1)}, Transcript: state.TranscriptMark{Path: gone, Offset: 0},
		AtMS: pw.clk.Boot().Milliseconds(), Written: state.WrittenYes,
	}}
	pw.add(a, encodeStore(t, s))
	pw.e.SetHealth(a, team.Health{Down: true, Stopped: "unauthorized"})
	text := pw.deliver(b, false, humanItem(b, 2, startWall, "bravo"))
	pw.tr.Record(text, faketranscript.Busy, 0)
	pw.settled(b)
	if got := pw.store(a); len(got.Awaiting) != 1 {
		t.Fatalf("the stopped team's entry: %+v", got)
	}
	pw.quietFor(missingCap)
	if got := pw.store(a); len(got.Missed) != 1 || !got.Pending[0].Held {
		t.Fatalf("the stopped team's entry after the hour: %+v", got)
	}
}

// A transcript the proof may not read (a symbolic link) is logged once, and its entries go
// missing on their clock.
func TestARefusedTranscriptIsLoggedOnce(t *testing.T) {
	pw := newProofWorld(t)
	a := tid(t, "alpha")
	pw.live(a)
	link := filepath.Join(filepath.Dir(pw.tr.Path()), "link.jsonl")
	check(t, os.Symlink(pw.tr.Path(), link))
	pw.transcript = link
	text := pw.deliver(a, true, humanItem(a, 1, startWall, "one"), humanItem(a, 2, startWall+1, "two"))
	pw.tr.Record(text, faketranscript.Busy, 0)
	pw.tr.Noise(3)
	pw.quietFor(quietAfter)
	checkMissing(t, pw.store(a), nonceOf(t, text))
	if n := strings.Count(pw.log.String(), proof.ErrRefused.Error()); n != 1 {
		t.Fatalf("%d refusal lines: %s", n, pw.log.String())
	}
}
