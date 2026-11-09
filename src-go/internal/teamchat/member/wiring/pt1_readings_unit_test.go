//go:build busfailpoints

package wiring

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
)

// P2's two readings (issue 156): when an item a failed recv handed back has a copy after its mark
// with no repeat line, the finding names which reading the run's record supports. (b), the
// harness: the copy's delivery was a missed entry before the recv ran, so it began before the
// hand-out, its first copy rightly carries no repeat line, and it was found late, after the mark.
// (a), production: the copy's delivery began after the hand-out and should carry the line, or it
// was awaiting before the recv, so the recv handed out an item a delivery held (P11's question).
// The verdict, what fails, is the same as before; only what a failure prints is added.

// returned is clean with $a handed out by a failed recv and returned, marked with no copy in the
// transcript, and delivered once afterwards by delivery nonce with no repeat line: the shape of the
// seed 57 finding. The mark's record is filled by each case.
func returned(nonce string) pt1Seen {
	o := clean()
	delete(o.recv, "$a")
	o.marked = map[string]int{"$a": 0}
	o.transcript = []pt1Copy{{id: "$b", nonce: "0b0b0b0b"}, {id: "$a", nonce: nonce}}
	o.firstSeen = []int{1, 6}
	return o
}

// snap is a store snapshot at seq with awaiting entries of nonces holding the item.
func snap(seq int, item string, nonces ...string) pt1Snap {
	return pt1Snap{seq: seq, read: true, item: item, awaiting: append([]string{}, nonces...), missed: []string{}}
}

// missedSnap is a store snapshot at seq with the item pending, repeat (as declareMissing leaves
// it), and missed entries of nonces naming it, each declared at atMS on the boot clock.
func missedSnap(seq int, atMS int64, nonces ...string) pt1Snap {
	at := map[string]int64{}
	for _, n := range nonces {
		at[n] = atMS
	}
	return pt1Snap{seq: seq, read: true, item: "pending repeat", awaiting: []string{}, missed: append([]string{}, nonces...), missedAt: at}
}

// pt1DueMS is a declaration time a missed entry of a run started at 0 can be due at.
const pt1DueMS = pt1QuietMS + 20_000

// pt1ReadingCase is a forced timeline and the reading the classifier must name for $a's copy 1.
type pt1ReadingCase struct {
	name   string
	seen   func() pt1Seen
	want   pt1Reading
	reason string
}

func pt1ReadingCases() []pt1ReadingCase {
	return []pt1ReadingCase{
		{"the copy's delivery was declared missing, once due, before the recv ran", func() pt1Seen {
			o := returned("d1d1d1d1")
			o.marks = map[string]pt1Mark{"$a": {copies: 0, before: missedSnap(2, pt1DueMS, "d1d1d1d1"), at: missedSnap(4, pt1DueMS, "d1d1d1d1")}}
			return o
		}, pt1Harness, "began before the hand-out"},
		{"the copy's delivery was declared missing before it could be due", func() pt1Seen {
			o := returned("d1d1d1d1")
			o.startMS = 5_000_000
			o.marks = map[string]pt1Mark{"$a": {copies: 0, before: missedSnap(2, o.startMS+pt1QuietMS-1, "d1d1d1d1"), at: missedSnap(4, o.startMS+pt1QuietMS-1, "d1d1d1d1")}}
			return o
		}, pt1Production, "declared missing before due"},
		{"the copy's delivery was declared missing just as it could be due", func() pt1Seen {
			o := returned("d1d1d1d1")
			o.startMS = 5_000_000
			o.marks = map[string]pt1Mark{"$a": {copies: 0, before: missedSnap(2, o.startMS+pt1QuietMS, "d1d1d1d1"), at: missedSnap(4, o.startMS+pt1QuietMS, "d1d1d1d1")}}
			return o
		}, pt1Harness, "found late"},
		{"the copy's delivery was both missed and awaiting before the recv ran", func() pt1Seen {
			o := returned("d1d1d1d1")
			both := missedSnap(2, pt1DueMS, "d1d1d1d1")
			both.awaiting = []string{"d1d1d1d1"}
			o.marks = map[string]pt1Mark{"$a": {copies: 0, before: both, at: snap(4, "pending repeat")}}
			return o
		}, pt1Ambiguous, "both missed and awaiting"},
		{"the copy's delivery was awaiting before the recv ran", func() pt1Seen {
			o := returned("d1d1d1d1")
			o.marks = map[string]pt1Mark{"$a": {copies: 0, before: snap(2, "pending, handed to d1d1d1d1", "d1d1d1d1"), at: snap(4, "pending repeat", "d1d1d1d1")}}
			return o
		}, pt1Production, "P11's question"},
		{"a redelivery begun after the return, in flight at the mark", func() pt1Seen {
			o := returned("d2d2d2d2")
			o.marks = map[string]pt1Mark{"$a": {copies: 0, before: snap(2, "pending"), at: snap(4, "handed to d2d2d2d2", "d2d2d2d2")}}
			return o
		}, pt1Production, "began after the recv"},
		{"a delivery begun after the mark", func() pt1Seen {
			o := returned("d3d3d3d3")
			o.marks = map[string]pt1Mark{"$a": {copies: 0, before: snap(2, "pending"), at: snap(4, "pending repeat")}}
			return o
		}, pt1Production, "began after the recv"},
		{"no record of the mark", func() pt1Seen { return returned("d1d1d1d1") }, pt1Ambiguous, "no record"},
		{"the store unread before the recv", func() pt1Seen {
			o := returned("d1d1d1d1")
			o.marks = map[string]pt1Mark{"$a": {copies: 0, before: pt1Snap{seq: 2, err: "no store"}, at: snap(4, "pending repeat")}}
			return o
		}, pt1Ambiguous, "not read"},
		{"a copy with no delivery nonce", func() pt1Seen {
			o := returned("")
			o.marks = map[string]pt1Mark{"$a": {copies: 0, before: snap(2, "pending"), at: snap(4, "pending repeat")}}
			return o
		}, pt1Ambiguous, "no delivery nonce"},
		{"a link crash in the run", func() pt1Seen {
			o := returned("d3d3d3d3")
			o.crashes = 1
			o.marks = map[string]pt1Mark{"$a": {copies: 0, before: snap(2, "pending"), at: snap(4, "pending repeat")}}
			return o
		}, pt1Ambiguous, "crash"},
		{"a copy first seen before the mark", func() pt1Seen {
			o := returned("d3d3d3d3")
			o.firstSeen = []int{1, 3}
			o.marks = map[string]pt1Mark{"$a": {copies: 0, before: snap(2, "pending"), at: snap(4, "pending repeat")}}
			return o
		}, pt1Ambiguous, "first seen at seq 3"},
	}
}

// readingCase is the forced timeline of that name.
func readingCase(t *testing.T, name string) pt1ReadingCase {
	t.Helper()
	cases := pt1ReadingCases()
	i := slices.IndexFunc(cases, func(c pt1ReadingCase) bool { return c.name == name })
	if i < 0 {
		t.Fatalf("no forced timeline %q", name)
	}
	return cases[i]
}

// The classifier names each forced timeline's reading, and "neither" where the record cannot
// tell.
func TestPT1P2ClassifierNamesTheReading(t *testing.T) {
	for _, c := range pt1ReadingCases() {
		got, why := pt1Classify(c.seen(), "$a", 0)
		if got != c.want || !strings.Contains(why, c.reason) {
			t.Errorf("%s: %q (%s), want %q with %q", c.name, got, why, c.want, c.reason)
		}
	}
}

// pt1OldP2 is P2's finding for a returned item's copy as it was printed before the readings: the
// first line of a finding now, byte for byte.
const pt1OldP2 = "P2: $a was returned to pending marked repeat and its copy 1 in the transcript carries no repeat line"

// A P2 failure prints its reading and the record it was read from, after the finding's own line.
func TestPT1P2FailurePrintsTheReadingAndTheRecord(t *testing.T) {
	o := readingCase(t, "the copy's delivery was declared missing, once due, before the recv ran").seen()
	got := pt1Check(o)
	if len(got) != 1 {
		t.Fatalf("findings %v, want one", got)
	}
	for _, want := range []string{
		pt1OldP2 + "\n",
		string(pt1Harness),
		"run started at 0 ms on the boot clock",
		"before the recv (seq 2): store read, $a pending repeat, awaiting [], missed [d1d1d1d1 at 200000 ms]",
		"at the mark (seq 4): 0 copies in the transcript, store read, $a pending repeat, awaiting [], missed [d1d1d1d1 at 200000 ms]",
		"copy 1: delivery d1d1d1d1, no repeat line, first seen at seq 6",
	} {
		if !strings.Contains(got[0], want) {
			t.Errorf("the finding\n%s\ndoes not hold %q", got[0], want)
		}
	}
	o = readingCase(t, "the copy's delivery was declared missing before it could be due").seen()
	if got := pt1Check(o); len(got) != 1 || !strings.Contains(got[0], string(pt1Production)) || !strings.Contains(got[0], "run started at 5000000 ms on the boot clock") || !strings.Contains(got[0], "missed [d1d1d1d1 at 5179999 ms]") {
		t.Errorf("the early missed entry's finding: %v", got)
	}
	o = readingCase(t, "the copy's delivery was awaiting before the recv ran").seen()
	if got := pt1Check(o); len(got) != 1 || !strings.Contains(got[0], string(pt1Production)) || !strings.Contains(got[0], "before the recv (seq 2): store read, $a pending, handed to d1d1d1d1, awaiting [d1d1d1d1], missed []") {
		t.Errorf("the awaiting entry's finding: %v", got)
	}
	o = readingCase(t, "a delivery begun after the mark").seen()
	if got := pt1Check(o); len(got) != 1 || !strings.Contains(got[0], string(pt1Production)) || !strings.Contains(got[0], "at the mark (seq 4): 0 copies in the transcript, store read, $a pending repeat, awaiting [], missed []") {
		t.Errorf("the production reading's finding: %v", got)
	}
}

// verdict is each finding's first line: what fails, without the reading added under it.
func verdict(findings []string) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, strings.SplitN(f, "\n", 2)[0])
	}
	return out
}

// P2's verdict does not depend on the record: every forced timeline fails with the old finding,
// byte for byte, and the same observation with no record at all fails with the same lines; a
// timeline whose copy carries the repeat line passes whatever its record.
func TestPT1P2VerdictIsTheRecordsNot(t *testing.T) {
	for _, c := range pt1ReadingCases() {
		o := c.seen()
		bare := o
		bare.marks, bare.firstSeen = nil, nil
		got, want := verdict(pt1Check(o)), verdict(pt1Check(bare))
		if !slices.Equal(got, []string{pt1OldP2}) || !slices.Equal(got, want) {
			t.Errorf("%s: verdict %q, with no record %q, want [%q]", c.name, got, want, pt1OldP2)
		}
		o.transcript[1].repeat = true
		if got := pt1Check(o); len(got) != 0 {
			t.Errorf("%s, the copy marked repeat: findings %v", c.name, got)
		}
	}
}

// storeMark is $a's mark record built from real stores through pt1SnapOf: before, the store just
// before the recv ran, and at, the store after the recv's return was marked, for a copy of $a
// carried by delivery nonce.
func storeMark(nonce string, before, at state.Store) pt1Seen {
	o := returned(nonce)
	o.marks = map[string]pt1Mark{"$a": {copies: 0, before: pt1SnapOf(2, before, nil, "$a"), at: pt1SnapOf(4, at, nil, "$a")}}
	return o
}

// returnedStore is a store after a failed recv returned $a: pending, repeat, handed to nobody,
// with the deliveries in flight that each case names.
func returnedStore() state.Store {
	return state.Store{Pending: []state.Pending{{ID: "$a", Repeat: true}}}
}

// Reading (b) on a timeline a recv can reach: delivery d4 was declared missing (declareMissing:
// its awaiting entry moved to missed, $a pending again, repeat, held, handed to nobody), so a recv
// may hand $a out; the recv fails and $a is returned and marked; then d4's line is found late, a
// copy written before the hand-out that rightly carries no repeat line.
func TestPT1P2ReadsAMissedEntryFoundLateAsTheHarness(t *testing.T) {
	before := state.Store{
		Pending: []state.Pending{{ID: "$a", Repeat: true, Held: true}},
		Missed:  []state.Missed{{Nonce: "d4d4d4d4", IDs: []string{"$a"}, AtMS: pt1DueMS}},
	}
	at := returnedStore()
	at.Missed = slices.Clone(before.Missed)
	got, why := pt1Classify(storeMark("d4d4d4d4", before, at), "$a", 0)
	if got != pt1Harness || !strings.Contains(why, "declared missing") {
		t.Fatalf("a missed entry found late: %q (%s), want %q", got, why, pt1Harness)
	}
}

// A missed entry declared before it could be due is not reading (b): declareMissing needs 180 s
// of quiet after the delivery's write (engine/proof.go), and no delivery of a run is written
// before the run starts, so an entry declared less than 180 s after the start was declared early,
// by production.
func TestPT1P2ReadsAMissedEntryDeclaredBeforeDueAsProduction(t *testing.T) {
	before := state.Store{
		Pending: []state.Pending{{ID: "$a", Repeat: true, Held: true}},
		Missed:  []state.Missed{{Nonce: "d6d6d6d6", IDs: []string{"$a"}, AtMS: 1_000}},
	}
	at := returnedStore()
	at.Missed = slices.Clone(before.Missed)
	got, why := pt1Classify(storeMark("d6d6d6d6", before, at), "$a", 0)
	if got != pt1Production || !strings.Contains(why, "declared missing before due") {
		t.Fatalf("a missed entry declared 1 s into the run: %q (%s), want %q, declared missing before due", got, why, pt1Production)
	}
}

// A delivery both missed and awaiting before the recv is a store that contradicts itself
// (declareMissing moves the entry, it never copies it): neither reading.
func TestPT1P2ReadsADeliveryBothMissedAndAwaitingAsAmbiguous(t *testing.T) {
	nonce := "d7d7d7d7"
	before := state.Store{
		Pending:  []state.Pending{{ID: "$a", Handed: &nonce}},
		Awaiting: []state.Awaiting{{Nonce: nonce, IDs: []string{"$a"}, Written: state.WrittenYes}},
		Missed:   []state.Missed{{Nonce: nonce, IDs: []string{"$a"}, AtMS: pt1DueMS}},
	}
	got, why := pt1Classify(storeMark(nonce, before, returnedStore()), "$a", 0)
	if got != pt1Ambiguous || !strings.Contains(why, "both missed and awaiting") {
		t.Fatalf("a delivery both missed and awaiting: %q (%s), want %q", got, why, pt1Ambiguous)
	}
}

// An awaiting entry is not reading (b): a recv hands out only an item handed to nobody, so an
// item an awaiting delivery held reached the recv only if production released it wrongly (a
// hand-out exclusivity breach, or an unawait of a line that was written), which is P11's question.
func TestPT1P2ReadsAnAwaitingEntryBeforeTheRecvAsProduction(t *testing.T) {
	nonce := "d5d5d5d5"
	before := state.Store{
		Pending:  []state.Pending{{ID: "$a", Handed: &nonce}},
		Awaiting: []state.Awaiting{{Nonce: nonce, IDs: []string{"$a"}, Written: state.WrittenYes}},
	}
	at := returnedStore()
	at.Awaiting = slices.Clone(before.Awaiting)
	got, why := pt1Classify(storeMark(nonce, before, at), "$a", 0)
	if got != pt1Production || !strings.Contains(why, "P11") {
		t.Fatalf("an awaiting entry before the recv: %q (%s), want %q naming P11", got, why, pt1Production)
	}
}

// A copy's delivery nonce is read from its entry's marker line.
func TestPT1ReadsTheDeliveryNonceOfEachCopy(t *testing.T) {
	got, err := pt1Copies(pt1Line(envelope(pt1Copy{id: "$a"})) + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := []pt1Copy{{id: "$a", nonce: "0a1b2c3d"}}; !slices.Equal(got, want) {
		t.Fatalf("copies %v, want %v", got, want)
	}
}
