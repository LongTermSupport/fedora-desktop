package state

import (
	"encoding/json"
	"testing"
)

const (
	evA = "$aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	evB = "$bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// Two events can share a millisecond: a floor raised by one covers it and not the other.
func TestAFloorRaisedByOneEventLeavesItsMillisecondsOthers(t *testing.T) {
	f := Floor{}.Raised(100, evA)
	if !f.Covers(100, evA) || !f.Covers(99, evB) {
		t.Errorf("%+v: the taken event or an earlier one is not covered", f)
	}
	if f.Covers(100, evB) || f.Covers(101, evA) {
		t.Errorf("%+v: an untaken event of the same millisecond, or a later one, is covered", f)
	}
	g := f.Raised(100, evB)
	if !g.Covers(100, evB) || !g.Covers(100, evA) {
		t.Errorf("%+v: the second event of the millisecond is not covered", g)
	}
	if h := g.Raised(99, "$c"); !h.Equal(g) {
		t.Errorf("an older event moved the floor: %+v", h)
	}
	if h := g.Raised(101, "$c"); h.MS != 101 || h.Covers(101, evA) || !h.Covers(100, evB) {
		t.Errorf("a newer event: %+v", h)
	}
}

// A floor with no IDs (one written before floor_ids) covers every event of its millisecond.
func TestAFloorWithoutIDsCoversItsWholeMillisecond(t *testing.T) {
	f := Floor{MS: 100}
	if !f.Covers(100, evA) || f.Covers(101, evA) {
		t.Errorf("%+v", f)
	}
	if g := f.Raised(100, evB); !g.Equal(f) {
		t.Errorf("an event of the millisecond narrowed a whole one: %+v", g)
	}
}

func TestBeforeCoversOnlyEarlierMilliseconds(t *testing.T) {
	f := Before(100)
	if f.Covers(100, evA) || !f.Covers(99, evA) {
		t.Errorf("%+v", f)
	}
}

func TestMaxAndBelow(t *testing.T) {
	a, b := Floor{}.Raised(100, evA), Floor{}.Raised(100, evB)
	m := a.Max(b)
	if !m.Covers(100, evA) || !m.Covers(100, evB) || !m.Equal(b.Max(a)) {
		t.Errorf("max of one millisecond: %+v", m)
	}
	if !a.Below(m) || m.Below(a) || a.Below(a) {
		t.Error("below is not strict")
	}
	later := Floor{}.Raised(101, evA)
	if !m.Max(later).Equal(later) || !later.Max(m).Equal(later) || !m.Below(later) {
		t.Error("a later millisecond is not the max")
	}
	whole := Floor{MS: 100}
	if !a.Max(whole).Equal(whole) || !a.Below(whole) {
		t.Error("a whole millisecond is not above part of it")
	}
}

// The store keeps a floor's IDs beside floor_ms, and a store without floor_ids reads as whole
// milliseconds.
func TestTheStoreKeepsAFloorsIDs(t *testing.T) {
	s := Sync{FloorMS: map[string]int64{}}
	s.SetFloor("main", Floor{}.Raised(100, evA))
	s.SetFloor("questions", Floor{MS: 7})
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back Sync
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if f, ok := back.Floor("main"); !ok || !f.Covers(100, evA) || f.Covers(100, evB) {
		t.Errorf("main %+v %v from %s", f, ok, raw)
	}
	if f, ok := back.Floor("questions"); !ok || !f.Covers(7, evB) {
		t.Errorf("questions %+v %v from %s", f, ok, raw)
	}
	if _, ok := back.Floor("side"); ok {
		t.Error("a room with no floor has one")
	}
	back.SetFloor("main", Floor{MS: 200})
	if _, held := back.FloorIDs["main"]; held {
		t.Errorf("a whole millisecond kept the old IDs: %v", back.FloorIDs)
	}
}
