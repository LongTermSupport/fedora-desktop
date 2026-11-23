package state

import "slices"

// Floor is a room's floor (B 2.3.2): every event before MS is taken, and of the events at MS
// those IDs names. Two events can share an origin_server_ts, so a millisecond alone cannot say
// which of them were taken. IDs nil is every event at MS: a floor kept before floor_ids, or a
// marker with none. The IDs are kept sorted, so two floors compare by value.
type Floor struct {
	MS  int64
	IDs []string
}

// Before is the floor just below ms: every earlier event, none at ms.
func Before(ms int64) Floor {
	return Floor{MS: max(ms, 0), IDs: []string{}}
}

// Covers is whether the event id sent at ts is at or below the floor, so taken.
func (f Floor) Covers(ts int64, id string) bool {
	if ts != f.MS {
		return ts < f.MS
	}
	return f.IDs == nil || slices.Contains(f.IDs, id)
}

// Raised is the floor with the event id sent at ts taken too.
func (f Floor) Raised(ts int64, id string) Floor {
	return f.Max(Floor{MS: ts, IDs: []string{id}})
}

// Max is the higher of two floors; at one millisecond, the events either took.
func (f Floor) Max(o Floor) Floor {
	switch {
	case o.MS > f.MS:
		return o.canonical()
	case o.MS < f.MS:
		return f.canonical()
	case f.IDs == nil || o.IDs == nil:
		return Floor{MS: f.MS}
	}
	return Floor{MS: f.MS, IDs: append(slices.Clone(f.IDs), o.IDs...)}.canonical()
}

// Equal is whether two floors cover the same events.
func (f Floor) Equal(o Floor) bool {
	a, b := f.canonical(), o.canonical()
	return a.MS == b.MS && (a.IDs == nil) == (b.IDs == nil) && slices.Equal(a.IDs, b.IDs)
}

// Below is whether f covers strictly less than o: o is the higher of the two, and not f.
func (f Floor) Below(o Floor) bool {
	return o.Max(f).Equal(o) && !f.Equal(o)
}

// canonical is f with its IDs sorted and distinct. A floor naming more than the store allows
// (maxFloorIDs) is the floor just below its millisecond: it covers fewer events, never more, so
// the events of that millisecond are fetched again and dropped as already held, where a floor
// the store refuses would fail every write of the batch that raised it.
func (f Floor) canonical() Floor {
	if f.IDs == nil {
		return f
	}
	ids := slices.Clone(f.IDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > maxFloorIDs {
		return Before(f.MS)
	}
	return Floor{MS: f.MS, IDs: ids}
}

// Floor is the floor of room, and whether it has one.
func (s Sync) Floor(room string) (Floor, bool) {
	ms, ok := s.FloorMS[room]
	if !ok {
		return Floor{}, false
	}
	return Floor{MS: ms, IDs: s.FloorIDs[room]}, true
}

// SetFloor keeps f as room's floor: its millisecond in floor_ms and its IDs, when it names
// them, in floor_ids.
func (s *Sync) SetFloor(room string, f Floor) {
	f = f.canonical()
	if s.FloorMS == nil {
		s.FloorMS = map[string]int64{}
	}
	s.FloorMS[room] = f.MS
	if f.IDs == nil {
		delete(s.FloorIDs, room)
		return
	}
	if s.FloorIDs == nil {
		s.FloorIDs = map[string][]string{}
	}
	s.FloorIDs[room] = f.IDs
}
