package engine

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/proof"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
)

// B 2.7.4's clocks, on the boot clock (n3e-D9): an entry is missing once the transcript has
// been quiet quietAfter since the later of its stop_ms and the transcript's last growth, or
// missingCap after its write; a missed entry is watched lateFindFor for a late find. A scan
// reads scanChunk bytes a call and at most scanRound bytes a path in one round, so a long
// transcript never holds the state goroutine for long.
const (
	quietAfter  = 180 * time.Second
	missingCap  = time.Hour
	lateFindFor = 24 * time.Hour
	scanChunk   = 1 << 20
	scanRound   = 8 << 20
)

// proofState is the transcript proof's memory (B 2.7.4, DV4), the state goroutine's alone but
// for kick and rounds: where each transcript was last scanned to and when it last grew, and the
// one timer armed for the next entry falling due.
type proofState struct {
	marks map[string]*pathMark
	stop  func() bool
	due   time.Duration
	gen   uint64
	// reread is a whole read of every transcript in progress before a deadline is judged.
	reread bool
	// kick wakes the proof loop for a round; rounds counts the rounds made.
	kick   chan struct{}
	rounds atomic.Int64
}

// pathMark is one transcript as the proof last read it: the mark the next scan starts from,
// the furthest end seen in its inode, when it last grew, and the last refusal logged for it.
type pathMark struct {
	mark   proof.Mark
	high   int64
	grewAt time.Duration
	grew   bool
	failed string
}

// watchFunc is proof.Watch, a field so the tests may replace it.
type watchFunc func(path string) (proof.Watcher, error)

// startProof starts the proof loop in loop, before the state goroutine takes its first op.
func (e *Engine) startProof(ctx context.Context, loop *sync.WaitGroup) {
	e.proof.kick = make(chan struct{}, 1)
	watch := e.watch
	if watch == nil {
		watch = proof.Watch
	}
	loop.Go(func() { e.proofLoop(ctx, watch) })
}

// proofLoop is B 2.7.4's watcher beside the state goroutine, in socket mode until ctx ends: it
// watches the session's transcript (following it to a new path) and runs a proof round on the
// state goroutine at each change, each kick and once after each new watch. The scan is a bounded
// local read made in the round; the network is never touched.
func (e *Engine) proofLoop(ctx context.Context, watch watchFunc) {
	var (
		w       proof.Watcher
		watched string
	)
	defer func() { e.closeWatch(w) }()
	for ctx.Err() == nil {
		var path string
		if err := e.do(ctx, func() {
			e.proofRound()
			path = e.sess.transcript
		}); err != nil {
			return
		}
		e.proof.rounds.Add(1)
		if path != watched {
			e.closeWatch(w)
			w, watched = e.watchTranscript(watch, path), path
			continue
		}
		var changes <-chan struct{}
		if w != nil {
			changes = w.Changes()
		}
		select {
		case _, open := <-changes:
			if !open {
				e.closeWatch(w)
				w = nil
			}
		case <-e.proof.kick:
		case <-ctx.Done():
		}
	}
}

// watchTranscript is a watch on path, nil for none or one that failed (logged): the rounds at
// each kick and each deadline still prove, only later.
func (e *Engine) watchTranscript(watch watchFunc, path string) proof.Watcher {
	if path == "" {
		return nil
	}
	w, err := watch(path)
	if err != nil {
		e.logf("proof: the transcript is not watched, it is scanned at each change of the stores and at each deadline: %v", err)
		return nil
	}
	return w
}

func (e *Engine) closeWatch(w proof.Watcher) {
	if w == nil {
		return
	}
	if err := w.Close(); err != nil {
		e.logf("proof: the transcript watch ended: %v", err)
	}
}

// pokeProof wakes the proof loop for a round; pokes meanwhile are one.
func (e *Engine) pokeProof() {
	select {
	case e.proof.kick <- struct{}{}:
	default:
	}
}

// proofChanged is publish's hunk: the proof's timer follows the stores at once, and the proof
// loop looks again.
func (e *Engine) proofChanged() {
	e.armProof()
	e.pokeProof()
}

// begun is whether h's store is this run's: an earlier run's store is neither delivered from nor
// proven until BeginRun has taken it (15b).
func (e *Engine) begun(h *held) bool {
	return h.store != nil && h.store.Run.ID == e.cfg.Run
}

// provable is whether h's entries are this proof's to settle.
func (e *Engine) provable(h *held) bool {
	return !h.removed && !h.refused && e.begun(h)
}

// seek is a round's work: every nonce watched, and where each transcript's scan starts when
// the proof has no mark for it yet.
type seek struct {
	nonces []string
	from   map[string]int64
}

func (s *seek) start(path string, offset int64) {
	if at, ok := s.from[path]; !ok || offset < at {
		s.from[path] = offset
	}
}

// sought is every awaiting and missed nonce of the provable stores. An awaiting entry's own
// transcript is scanned from its offset; the session's transcript too (from the missed
// offsets, or from 0 when it holds no entry, as after a /clear).
func (e *Engine) sought() seek {
	s := seek{from: map[string]int64{}}
	var missed []int64
	for _, id := range e.order() {
		h := e.teams[id]
		if !e.provable(h) {
			continue
		}
		for _, a := range h.store.Awaiting {
			s.nonces = append(s.nonces, a.Nonce)
			s.start(a.Transcript.Path, a.Transcript.Offset)
		}
		for _, m := range h.store.Missed {
			s.nonces = append(s.nonces, m.Nonce)
			missed = append(missed, m.Offset)
		}
	}
	if t := e.sess.transcript; t != "" && len(s.nonces) > 0 {
		if _, ok := s.from[t]; !ok {
			s.from[t] = 0
			if len(missed) > 0 {
				s.from[t] = slices.Min(missed)
			}
		}
	}
	return s
}

// proofRound is one round on the state goroutine: every watched transcript scanned on from its
// mark, then each provable team's entries settled in one write: found awaiting entries proven,
// found missed ones a late find, due ones missing (only once every transcript was read to its
// end), expired missed ones dropped. The timer is armed for what falls due next.
func (e *Engine) proofRound() {
	sk := e.sought()
	if len(sk.nonces) == 0 {
		clear(e.proof.marks)
		e.armProof()
		return
	}
	if e.proof.marks == nil {
		e.proof.marks = map[string]*pathMark{}
	}
	for path := range e.proof.marks {
		if _, ok := sk.from[path]; !ok {
			delete(e.proof.marks, path)
		}
	}
	now := e.cfg.Clock.Boot()
	e.startReread(now)
	found, caught := map[string]bool{}, true
	for _, path := range slices.Sorted(maps.Keys(sk.from)) {
		m := e.proof.marks[path]
		if m == nil {
			// The offset sought is the furthest end known, also when the read starts at 0.
			start, high := sk.from[path], sk.from[path]
			if e.proof.reread {
				start = 0
			}
			m = &pathMark{mark: proof.Mark{Offset: start}, high: high}
			e.proof.marks[path] = m
		}
		caught = e.scanPath(path, m, sk.nonces, now, found) && caught
	}
	if !caught {
		e.pokeProof()
	}
	e.proof.reread = e.proof.reread && !caught
	for _, id := range e.order() {
		if h := e.teams[id]; e.provable(h) {
			e.settleProof(h, found, caught, now)
		}
	}
	e.armProof()
}

// scanPath reads path on from m for nonces, up to scanRound bytes, adding what it finds; it is
// whether the read reached the file's end. A missing file holds nothing yet; a refusal is
// logged once and holds nothing either, so its entries go missing on their clock.
func (e *Engine) scanPath(path string, m *pathMark, nonces []string, now time.Duration, found map[string]bool) bool {
	for read := int64(0); read < scanRound; {
		res, err := proof.Scan(path, m.mark, nonces, scanChunk)
		if err != nil {
			if why := err.Error(); !errors.Is(err, fs.ErrNotExist) && why != m.failed {
				m.failed = why
				e.logf("proof: transcript %s is not read: %v", path, err)
			}
			return true
		}
		m.failed = ""
		for _, n := range res.Found {
			found[n] = true
		}
		from := m.mark.Offset
		if res.Reset {
			from, m.high = 0, 0
		}
		if res.Reset || res.End.Offset > m.high {
			m.grew, m.grewAt, m.high = true, now, max(m.high, res.End.Offset)
		}
		m.mark = res.End
		if res.End.Offset <= from {
			return true
		}
		read += res.End.Offset - from
	}
	return false
}

// startReread starts every transcript's scan again from 0 when an entry falls due: a file cut
// and grown back past its mark between two scans looks unchanged to an offset, so nothing is
// declared missing before every transcript has been read whole once more. Growth is judged
// against the furthest end seen, so the re-read does not restart a quiet clock.
func (e *Engine) startReread(now time.Duration) {
	if e.proof.reread || !e.anyDue(now) {
		return
	}
	e.proof.reread = true
	for _, m := range e.proof.marks {
		m.mark.Offset = 0
	}
}

func (e *Engine) anyDue(now time.Duration) bool {
	for _, h := range e.teams {
		if e.provable(h) && slices.ContainsFunc(h.store.Awaiting, func(a state.Awaiting) bool { return now >= e.dueAt(a) }) {
			return true
		}
	}
	return false
}

// lastGrowth is when any watched transcript last grew, and whether one did.
func (e *Engine) lastGrowth() (time.Duration, bool) {
	var (
		at   time.Duration
		grew bool
	)
	for _, m := range e.proof.marks {
		if m.grew && (!grew || m.grewAt > at) {
			at, grew = m.grewAt, true
		}
	}
	return at, grew
}

// dueAt is when entry a is missing: missingCap after its write, or quietAfter after the later of
// its stop and the last growth, whichever comes first.
func (e *Engine) dueAt(a state.Awaiting) time.Duration {
	due := time.Duration(a.AtMS)*time.Millisecond + missingCap
	if a.StopMS == nil {
		return due
	}
	quiet := time.Duration(*a.StopMS) * time.Millisecond
	if grew, ok := e.lastGrowth(); ok && grew > quiet {
		quiet = grew
	}
	return min(due, quiet+quietAfter)
}

func expiresAt(m state.Missed) time.Duration {
	return time.Duration(m.AtMS)*time.Millisecond + lateFindFor
}

// settled is what one team's round settles.
type settled struct {
	proven, missing []state.Awaiting
	late            []state.Missed
	expired         int
}

func (e *Engine) settling(h *held, found map[string]bool, caught bool, now time.Duration) settled {
	var out settled
	for _, a := range h.store.Awaiting {
		switch {
		case found[a.Nonce]:
			out.proven = append(out.proven, a)
		case caught && now >= e.dueAt(a):
			out.missing = append(out.missing, a)
		}
	}
	for _, m := range h.store.Missed {
		switch {
		case found[m.Nonce]:
			out.late = append(out.late, m)
		case now >= expiresAt(m):
			out.expired++
		}
	}
	return out
}

// settleProof is one team's round, in one store write.
func (e *Engine) settleProof(h *held, found map[string]bool, caught bool, now time.Duration) {
	st := e.settling(h, found, caught, now)
	if len(st.proven)+len(st.missing)+len(st.late)+st.expired == 0 {
		return
	}
	var (
		twice []string
		left  []state.Pending
		floor int64
		moved bool
	)
	offset := e.missedOffset()
	err := e.changeLocked(h.id, func(_ *held, s *state.Store) (bool, error) {
		wall, at := e.cfg.Clock.WallMS(), now.Milliseconds()
		for _, a := range st.proven {
			left = append(left, e.prove(s, a, wall)...)
		}
		for _, a := range st.missing {
			declareMissing(s, a, offset(a), at)
		}
		for _, m := range st.late {
			gone, shown := e.findLate(s, m, wall)
			left, twice = append(left, gone...), append(twice, shown...)
		}
		s.Missed = slices.DeleteFunc(s.Missed, func(m state.Missed) bool { return now >= expiresAt(m) })
		floor, moved = raiseFloors(s, left)
		return true, nil
	})
	if err != nil {
		e.logf("team %s: the proof's write failed, the next round tries again: %v", h.id, err)
		return
	}
	e.provedNote(h, st, left, twice)
	if moved {
		e.floorRaised(h.id, floor)
	}
	if len(st.missing) > 0 {
		e.hit(pointAfterMissing)
	}
}

// missedOffset is the offset a missing entry's late find scans the session's transcript from:
// that transcript's mark, else the entry's own.
func (e *Engine) missedOffset() func(state.Awaiting) int64 {
	return func(a state.Awaiting) int64 {
		if m := e.proof.marks[e.sess.transcript]; m != nil {
			return m.mark.Offset
		}
		if m := e.proof.marks[a.Transcript.Path]; m != nil {
			return m.mark.Offset
		}
		return a.Transcript.Offset
	}
}

// provedNote is the memory and log.link lines of a team's settled round.
func (e *Engine) provedNote(h *held, st settled, left []state.Pending, twice []string) {
	for _, p := range left {
		delete(h.content, p.ID)
	}
	if len(st.proven) > 0 {
		h.shownMissing = map[string]bool{}
		for _, m := range h.store.Missed {
			if !slices.ContainsFunc(st.missing, func(a state.Awaiting) bool { return a.Nonce == m.Nonce }) {
				h.shownMissing[m.Nonce] = true
			}
		}
		// The write published the status before the count cleared.
		e.publish()
	}
	for _, a := range st.missing {
		e.logf("team %s: delivery %s is missing: the transcript does not show it; its %d items are pending again, repeat and held from the socket", h.id, a.Nonce, len(a.IDs))
	}
	for _, m := range st.late {
		e.logf("team %s: delivery %s, declared missing, is found in the transcript", h.id, m.Nonce)
	}
	if len(twice) > 0 {
		h.seenTwice += int64(len(twice))
		e.logf("team %s: items %v were seen twice: recv showed them and the transcript shows them", h.id, twice)
	}
	if st.expired > 0 {
		e.logf("team %s: %d missing deliveries are no longer watched (%s)", h.id, st.expired, lateFindFor)
	}
}

// prove settles a found entry in s: its items out of pending (the ledger already holds a
// written-yes entry's; an unknown one's go in now), and a receipt for each HUMAN item.
func (e *Engine) prove(s *state.Store, a state.Awaiting, wall int64) []state.Pending {
	s.Awaiting = slices.DeleteFunc(s.Awaiting, func(x state.Awaiting) bool { return x.Nonce == a.Nonce })
	var ids []string
	for _, p := range s.Pending {
		if p.Handed != nil && *p.Handed == a.Nonce {
			ids = append(ids, p.ID)
		}
	}
	left := takePending(s, ids)
	for _, p := range left {
		if a.Written != state.WrittenYes {
			s.AddLedger(ledgerOf(p, wall))
		}
		e.receipt(s, p)
	}
	return left
}

func ledgerOf(p state.Pending, wall int64) state.Ledger {
	return state.Ledger{ID: p.ID, Room: p.Room, Item: p.Item, From: p.From, AtMS: wall}
}

// receipt queues p's read receipt when it is HUMAN (DV5); a full queue loses the receipt, never
// the proof.
func (e *Engine) receipt(s *state.Store, p state.Pending) {
	if p.Item != records.KindHuman {
		return
	}
	if err := s.AddReceipt(state.Receipt{ID: p.ID, Room: p.Room}); err != nil {
		e.logf("team %s: no read receipt for %s: %v", s.Team, p.ID, err)
	}
}

// declareMissing moves a due entry to missed in s: its items pending again, repeat and held from
// the socket, and out of the ledger (B 2.7.4).
func declareMissing(s *state.Store, a state.Awaiting, offset, at int64) {
	s.Awaiting = slices.DeleteFunc(s.Awaiting, func(x state.Awaiting) bool { return x.Nonce == a.Nonce })
	for i := range s.Pending {
		if h := s.Pending[i].Handed; h != nil && *h == a.Nonce {
			s.Pending[i].Handed, s.Pending[i].Repeat, s.Pending[i].Held = nil, true, true
		}
	}
	s.Ledger = slices.DeleteFunc(s.Ledger, func(l state.Ledger) bool { return slices.Contains(a.IDs, l.ID) })
	s.AddMissed(state.Missed{Nonce: a.Nonce, IDs: slices.Clone(a.IDs), Offset: offset, AtMS: at})
}

// findLate is a missed entry found (P9): its items still pending leave as proven; those recv has
// shown (in the ledger, or handed to a recv now) are seen twice, and a handed one leaves pending
// too, so a return cannot show it a third time. It is the items that left, and those seen twice.
func (e *Engine) findLate(s *state.Store, m state.Missed, wall int64) ([]state.Pending, []string) {
	s.Missed = slices.DeleteFunc(s.Missed, func(x state.Missed) bool { return x.Nonce == m.Nonce })
	awaiting := awaitingIDs(s)
	var ids, twice []string
	for _, id := range m.IDs {
		i := slices.IndexFunc(s.Pending, func(p state.Pending) bool { return p.ID == id })
		switch {
		case awaiting[id]:
		case i >= 0:
			ids = append(ids, id)
			if s.Pending[i].Handed != nil {
				twice = append(twice, id)
			}
		case slices.ContainsFunc(s.Ledger, func(l state.Ledger) bool { return l.ID == id }):
			twice = append(twice, id)
		}
	}
	left := takePending(s, ids)
	for _, p := range left {
		s.AddLedger(ledgerOf(p, wall))
		e.receipt(s, p)
	}
	return left, twice
}

// setIdle is SetSession's hunk: the session's idleness, and at its start (the first Stop, or a
// SessionStart with no prompt since) every awaiting entry without a stop gets one, one write per
// store, which starts its quiet clock (B 2.7.4).
func (e *Engine) setIdle(idle bool) {
	was := e.sess.idle
	e.sess.idle = idle
	e.pokeProof()
	if !idle || was {
		return
	}
	at := e.cfg.Clock.Boot().Milliseconds()
	for _, id := range e.order() {
		h := e.teams[id]
		if !e.provable(h) || !slices.ContainsFunc(h.store.Awaiting, func(a state.Awaiting) bool { return a.StopMS == nil }) {
			continue
		}
		if err := e.changeLocked(id, func(_ *held, s *state.Store) (bool, error) {
			for i := range s.Awaiting {
				if s.Awaiting[i].StopMS == nil {
					stop := at
					s.Awaiting[i].StopMS = &stop
				}
			}
			return true, nil
		}); err != nil {
			e.logf("team %s: the Stop was not recorded on its deliveries, their quiet clock waits for the next: %v", id, err)
		}
	}
}

// returnEarlierRun is BeginRun's hunk, in its one write (15b, 15c): an earlier run's awaiting
// entries are another session's, so their items go back to pending, repeat but not held (this
// session never saw that socket), out of the ledger; its missed entries are dropped, their boot
// times being of a run whose clock this one cannot compare. Keyed by the run id, a crash before
// the write repeats it whole and one after finds the run its own.
func (e *Engine) returnEarlierRun(s *state.Store) {
	// The store becomes this run's: its items are deliverable from now.
	e.poke()
	if s.Run.ID == e.cfg.Run || len(s.Awaiting)+len(s.Missed) == 0 {
		return
	}
	n, dropped := len(s.Awaiting), len(s.Missed)
	for _, a := range s.Awaiting {
		for i := range s.Pending {
			if h := s.Pending[i].Handed; h != nil && *h == a.Nonce {
				s.Pending[i].Handed, s.Pending[i].Repeat = nil, true
			}
		}
		s.Ledger = slices.DeleteFunc(s.Ledger, func(l state.Ledger) bool { return slices.Contains(a.IDs, l.ID) })
	}
	s.Awaiting, s.Missed = []state.Awaiting{}, []state.Missed{}
	e.logf("team %s: %d deliveries of an earlier run are pending again (repeat), and %d of its missing deliveries are no longer watched", s.Team, n, dropped)
}

// missing is status.json's count for h (n3e-D11): its missed entries since its last proof.
func (h *held) missing() int64 {
	if h.store == nil {
		return 0
	}
	n := int64(0)
	for _, m := range h.store.Missed {
		if !h.shownMissing[m.Nonce] {
			n++
		}
	}
	return n
}

// armProof arms the one proof timer for the earliest entry falling due in a provable store,
// else disarms it; on the state goroutine, at every change, so the timer follows the stores.
func (e *Engine) armProof() {
	var (
		due time.Duration
		has bool
	)
	next := func(at time.Duration) {
		if !has || at < due {
			due, has = at, true
		}
	}
	for _, h := range e.teams {
		if !e.provable(h) {
			continue
		}
		for _, a := range h.store.Awaiting {
			next(e.dueAt(a))
		}
		for _, m := range h.store.Missed {
			next(expiresAt(m))
		}
	}
	p := &e.proof
	if !has {
		e.disarmProof()
		return
	}
	if p.stop != nil && p.due == due {
		return
	}
	e.disarmProof()
	p.gen++
	gen := p.gen
	p.due = due
	p.stop = e.cfg.Clock.AfterFunc(max(due-e.cfg.Clock.Boot(), 0), func() { e.onGoroutine(func() { e.proofDue(gen) }) })
}

func (e *Engine) disarmProof() {
	if p := &e.proof; p.stop != nil {
		p.stop()
		p.stop = nil
	}
}

// proofDue is the timer's fire: a round now, which scans before it declares anything missing.
func (e *Engine) proofDue(gen uint64) {
	p := &e.proof
	if gen != p.gen || p.stop == nil {
		return
	}
	p.stop = nil
	e.proofRound()
}
