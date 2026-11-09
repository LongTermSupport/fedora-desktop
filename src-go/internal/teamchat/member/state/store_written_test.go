package state

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The class these tests hold: a size or a cap computed from another encoding than the one the
// store's writer writes. encoding/json writes `<`, `>` and `&` as six bytes each and U+2028 and
// U+2029 as six, where A's compact form (proto.Value.Size) writes one and three, so a cap taken
// in the compact form lets an entry through that is up to six times as large in the file.

// worstPieces are the characters a free text field is filled with: each one the writer escapes,
// the ones A's compact form escapes too, multibyte UTF-8, and a plain letter.
var worstPieces = []string{"<", ">", "&", `"`, `\`, "\x01", "\x1f", "é", "€", "😀", lineSeparator, paragraphSeparator, "x"}

// U+2028 and U+2029, which encoding/json writes as six-byte escapes.
var (
	lineSeparator      = string(rune(0x2028))
	paragraphSeparator = string(rune(0x2029))
)

// fillFunc makes every one of count entries of its list in s the list's largest form, with n
// copies of piece in the entry's free text.
type fillFunc func(t *testing.T, s *Store, count int, piece string, n int)

// freeText is each capped list of the store (listCaps, by name) and how its free text is
// filled; nil for a list whose every string is held to a grammar with no character the writer
// escapes (event ids, user ids, verbs, references, nonces, digests). A list added to listCaps
// without a row here fails TestEveryListOfTheRegistryHasARow.
func freeText() map[string]fillFunc {
	return map[string]fillFunc{
		"pending":   nil,
		"ledger":    nil,
		"receipts":  nil,
		"pings":     nil,
		"missed":    nil,
		"gate keys": nil,
		// A transcript's path is the session's: any UTF-8 a path may hold.
		"awaiting": func(_ *testing.T, s *Store, count int, piece string, n int) {
			s.Awaiting = []Awaiting{}
			for i := range count {
				s.Awaiting = append(s.Awaiting, Awaiting{
					Nonce: fmt.Sprintf("%08x", i), IDs: []string{eventID(1), eventID(2)},
					Transcript: TranscriptMark{Path: "/" + strings.Repeat(piece, n) + ".jsonl", Offset: 1 << 40},
					AtMS:       farMS, Written: WrittenUnknown, StopMS: new(int64(farMS)),
				})
			}
		},
		// A send's content is the composed event: a body of any text.
		"sending": func(t *testing.T, s *Store, count int, piece string, n int) {
			t.Helper()
			s.Outbox.Sending = []Sending{}
			for range count {
				x := largestSending(t)
				x.Content = json.RawMessage(`{"body":` + string(proto.Str(strings.Repeat(piece, n)).Compact()) + `}`)
				s.Outbox.Sending = append(s.Outbox.Sending, x)
			}
		},
	}
}

// TestSendingFitsIsTheStoreRule holds that Fits, which the engine asks before a charge, is the
// store's own rule: the largest entry the store writes fits, and one character over, Fits
// refuses it exactly when the store's refusal is the entry's size (not A's content cap).
func TestSendingFitsIsTheStoreRule(t *testing.T) {
	fill := freeText()["sending"]
	for _, piece := range worstPieces {
		n := largestAccepted(t, fill, piece)
		s := sampleStore(t)
		fill(t, &s, 1, piece, n)
		if err := s.Outbox.Sending[0].Fits(); err != nil {
			t.Errorf("%d × %q, which the store writes: Fits = %v", n, piece, err)
		}
		fill(t, &s, 1, piece, n+1)
		_, written := s.encode()
		bySize := written != nil && strings.Contains(written.Error(), "an entry of sending is over")
		if fits := s.Outbox.Sending[0].Fits(); errors.Is(fits, ErrTooLarge) != bySize {
			t.Errorf("%d × %q: Fits = %v, the store's write = %v", n+1, piece, fits, written)
		}
	}
	x := largestSending(t)
	x.Content = json.RawMessage(`{"body":`)
	if err := x.Fits(); err == nil || errors.Is(err, ErrTooLarge) {
		t.Fatalf("a content that is not JSON: Fits = %v, want an encoding error", err)
	}
}

func TestEveryListOfTheRegistryHasARow(t *testing.T) {
	rows := freeText()
	for _, l := range listCaps {
		if _, ok := rows[l.name]; !ok {
			t.Errorf("the store's list %q has no row in freeText: say how its free text is filled, or nil for none", l.name)
		}
	}
	if len(rows) != len(listCaps) {
		t.Errorf("freeText has %d rows for %d lists", len(rows), len(listCaps))
	}
}

// writtenEntry is the bytes one entry takes in the file the writer writes: the store with that
// one entry less the store with none, both encoded by the real encoder.
func writtenEntry(t *testing.T, fill fillFunc, piece string, n int) (int, error) {
	t.Helper()
	one, none := sampleStore(t), sampleStore(t)
	fill(t, &one, 1, piece, n)
	fill(t, &none, 0, piece, n)
	withOne, err := one.encode()
	if err != nil {
		return 0, err
	}
	empty, err := none.encode()
	check(t, err)
	return len(withOne) - len(empty), nil
}

// largestAccepted is the most copies of piece the free text of one entry takes and is still
// written; the store's own rules decide, whichever of them refuses first.
func largestAccepted(t *testing.T, fill fillFunc, piece string) int {
	t.Helper()
	accepted := func(n int) bool {
		s := sampleStore(t)
		fill(t, &s, 1, piece, n)
		_, err := s.encode()
		return err == nil
	}
	if !accepted(0) {
		t.Fatalf("an entry with no free text is refused")
	}
	hi := 1
	for accepted(hi) {
		hi *= 2
		if hi > 1<<20 {
			t.Fatalf("an entry of %d × %q is still accepted", hi, piece)
		}
	}
	lo := hi / 2
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if accepted(mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}

// TestEveryListIsCappedAsWritten holds n3d-D1's promise for every capped list and every worst
// character: the largest entry the store accepts is within its entry cap in the written file, a
// full list of them is written under the file cap, and one character more is refused by a rule
// of the entry, never by the file cap.
func TestEveryListIsCappedAsWritten(t *testing.T) {
	rows := freeText()
	for _, l := range listCaps {
		fill := rows[l.name]
		if fill == nil {
			continue
		}
		for _, piece := range worstPieces {
			t.Run(fmt.Sprintf("%s/%q", l.name, piece), func(t *testing.T) {
				t.Parallel()
				listAsWritten(t, l, fill, piece)
			})
		}
	}
}

// fullStorePieces are the pieces a whole store is also written with: the two ways the writer
// writes a character longer than A's form does (six bytes for one, six for three). The other
// pieces are written as A's form writes them, so their entry check above is the whole question,
// and a full store's encode is what makes this test slow under -race.
var fullStorePieces = []string{"<", lineSeparator}

func listAsWritten(t *testing.T, l listCap, fill fillFunc, piece string) {
	t.Helper()
	n := largestAccepted(t, fill, piece)
	written, err := writtenEntry(t, fill, piece, n)
	check(t, err)
	if written > l.size {
		t.Errorf("%s: an entry of %d × %q is accepted and written as %d bytes, over its cap of %d", l.name, n, piece, written, l.size)
	}
	if !slices.Contains(fullStorePieces, piece) {
		return
	}
	full := fullStore(t)
	fill(t, &full, l.count, piece, n)
	data, err := full.encode()
	if err != nil {
		t.Errorf("%s: a full store with %d entries of %d × %q is refused: %v", l.name, l.count, n, piece, err)
	} else if len(data) > MaxStore {
		t.Errorf("%s: a full store is %d bytes, over the file cap", l.name, len(data))
	}
	fill(t, &full, l.count, piece, n+1)
	if _, err := full.encode(); err == nil || strings.Contains(err.Error(), "larger than its cap") {
		t.Errorf("%s: a full store with %d entries of %d × %q, one over the largest accepted, = %v; want a rule of the entry", l.name, l.count, n+1, piece, err)
	}
}

// TestEveryListAtItsWorstFitsTheFileCap fills every list with free text at once, each entry the
// largest accepted of `<`, and the position at its longest written form: the file cap holds it.
func TestEveryListAtItsWorstFitsTheFileCap(t *testing.T) {
	s := fullStore(t)
	for _, l := range listCaps {
		if fill := freeText()[l.name]; fill != nil {
			fill(t, &s, l.count, "<", largestAccepted(t, fill, "<"))
		}
	}
	s.Sync.NextBatch = strings.Repeat("<", maxNextBatch)
	s.Sync.Fresh = false
	data, err := s.encode()
	check(t, err)
	if len(data) > MaxStore {
		t.Fatalf("a store with every list at its worst is %d bytes, over the file cap of %d", len(data), MaxStore)
	}
}

// fixedWritten is the most bytes each string, map, raw value or list of the store outside its
// capped lists takes in the written file, quotes included, by its grammar: a value that may hold
// a character the writer escapes counts six bytes for each. A field the walk of the Store type
// finds without a row fails TestEveryFixedFieldHasARow, so a key added later is counted here.
func fixedWritten() map[string]int {
	return map[string]int{
		"team":    2 + 22, // ^[a-z][a-z0-9-]{0,21}$
		"seat":    2 + 12,
		"user_id": 2 + maxUserID,
		// syncTokenRe is printable ASCII, `<`, `>` and `&` included.
		"sync.next_batch":  2 + 6*maxNextBatch,
		"sync.floor_ms":    2 + len(storeRooms)*(len(`"questions":,`)+16),
		"outbox.last_sent": 2 + 44,
		// The questions room's keys (SPEC-n3f QX6). Each list is its frame (the key and the
		// brackets) and its entries; the key `questions` is bounded whole by its codec, which
		// counts the escaping, so it is one leaf; an event id has no character to escape.
		"outbox.reports":            sizeListFrame + capReports*sizeReportTime,
		"outbox.questions_unechoed": 2 + 44,
		"remind_late":               sizeListFrame + capRemindLate*sizeRemindLate,
		"questions":                 proto.MaxQuestionsStateBytes,
		"run.id":                    2 + 16,
		"run.ended.notice":          2 + len(NoticeDisconnected),
		"run.ended.reason":          2 + len(ReasonLaunchFailed),
		"run.ended.state":           2 + len(StateAttempted),
		"run.ended.txn":             2 + 34,
	}
}

var (
	rawMessageType    = reflect.TypeFor[json.RawMessage]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
	jsonMarshalerType = reflect.TypeFor[json.Marshaler]()
)

// storeLeaves is every JSON path of a string, map, raw value or list in the Store type below
// prefix that is not one of listCaps, which the list rules size; a struct with a row of its own
// in rows (a key whose codec bounds it whole) is one leaf.
func storeLeaves(typ reflect.Type, prefix string, rows map[string]int, out *[]string) {
	for f := range typ.Fields() {
		// The store's tags are bare names: no option such as omitempty changes what is written.
		name := f.Tag.Get("json")
		if name == "" {
			continue
		}
		path := strings.TrimPrefix(prefix+"."+name, ".")
		if slices.ContainsFunc(listCaps, func(l listCap) bool { return strings.Join(l.path, ".") == path }) {
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		_, own := rows[path]
		leaf := own || ft == rawMessageType || reflect.PointerTo(ft).Implements(textMarshalerType) || reflect.PointerTo(ft).Implements(jsonMarshalerType)
		switch {
		case leaf, ft.Kind() == reflect.String, ft.Kind() == reflect.Map, ft.Kind() == reflect.Slice:
			*out = append(*out, path)
		case ft.Kind() == reflect.Struct:
			storeLeaves(ft, path, rows, out)
		}
	}
}

func TestEveryFixedFieldHasARow(t *testing.T) {
	var leaves []string
	rows := fixedWritten()
	storeLeaves(reflect.TypeFor[Store](), "", rows, &leaves)
	for _, path := range leaves {
		if _, ok := rows[path]; !ok {
			t.Errorf("the store's %s has no row in fixedWritten: give the most bytes it is written as", path)
		}
	}
	if len(rows) != len(leaves) {
		t.Errorf("fixedWritten has %d rows for %d fields: %v", len(rows), len(leaves), leaves)
	}
}

// writtenFixed is the sum of fixedWritten: what the file cap's arithmetic counts for the store
// outside its lists, besides the keys, numbers and punctuation.
func writtenFixed() int {
	sum := 0
	for _, n := range fixedWritten() {
		sum += n
	}
	return sum
}
