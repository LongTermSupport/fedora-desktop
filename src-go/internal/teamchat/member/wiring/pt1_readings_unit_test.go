//go:build busfailpoints

package wiring

import (
	"slices"
	"strings"
	"testing"
)

// P2's two readings (issue 156): when an item a failed recv handed back has a copy after its mark
// with no repeat line, the finding names which reading the run's record supports. (b), the
// harness: the copy's delivery was in flight in the store before the recv ran, so it began before
// the hand-out, its first copy rightly carries no repeat line, and the mark was sampled before it
// landed. (a), production: no delivery of the item was in flight before the recv, so the copy's
// delivery began after the hand-out and should carry the line. The verdict, what fails, is the
// same as before; only what a failure prints is added.

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

// snap is a store snapshot at seq holding nonces in flight for the item.
func snap(seq int, item string, nonces ...string) pt1Snap {
	return pt1Snap{seq: seq, read: true, item: item, inFlight: nonces}
}

// pt1ReadingCase is a forced timeline and the reading the classifier must name for $a's copy 1.
type pt1ReadingCase struct {
	name   string
	seen   func() pt1Seen
	want   pt1Reading
	reason string
}

func pt1ReadingCases() []pt1ReadingCase {
	return []pt1ReadingCase{
		{"the copy's delivery was in flight before the recv ran", func() pt1Seen {
			o := returned("d1d1d1d1")
			o.marks = map[string]pt1Mark{"$a": {copies: 0, before: snap(2, "handed to d1d1d1d1", "d1d1d1d1"), at: snap(4, "handed to d1d1d1d1", "d1d1d1d1")}}
			return o
		}, pt1Harness, "began before the hand-out"},
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
	o := pt1ReadingCases()[0].seen()
	got := pt1Check(o)
	if len(got) != 1 {
		t.Fatalf("findings %v, want one", got)
	}
	for _, want := range []string{
		pt1OldP2 + "\n",
		string(pt1Harness),
		"before the recv (seq 2): store read, $a handed to d1d1d1d1, in flight [d1d1d1d1]",
		"at the mark (seq 4): 0 copies in the transcript, store read, $a handed to d1d1d1d1, in flight [d1d1d1d1]",
		"copy 1: delivery d1d1d1d1, no repeat line, first seen at seq 6",
	} {
		if !strings.Contains(got[0], want) {
			t.Errorf("the finding\n%s\ndoes not hold %q", got[0], want)
		}
	}
	o = pt1ReadingCases()[2].seen()
	if got := pt1Check(o); len(got) != 1 || !strings.Contains(got[0], string(pt1Production)) || !strings.Contains(got[0], "at the mark (seq 4): 0 copies in the transcript, store read, $a pending repeat, in flight []") {
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
