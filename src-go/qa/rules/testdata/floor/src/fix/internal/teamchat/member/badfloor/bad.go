// Package badfloor holds the floor held as a millisecond alone, as teamsync had it.
package badfloor

import "fix/internal/teamchat/member/state"

type event struct {
	OriginTs int64
	ID       string
}

type bounds struct{ floor int64 }

type judged struct{ floor int64 }

// qtake is the originating shape: a post at the floor's millisecond is taken as already seen.
func qtake(evs []event, floor int64) []event {
	var out []event
	for _, e := range evs {
		if e.OriginTs <= floor { // want `a floor compared as a millisecond alone`
			continue
		}
		out = append(out, e)
	}
	return out
}

// raise is the floor set to the newest millisecond, and compared floor against floor.
func raise(evs []event, jr judged, floor int64) int64 {
	for _, e := range evs {
		jr.floor = max(jr.floor, e.OriginTs) // want `a floor set by max as a millisecond alone`
	}
	if jr.floor > floor { // want `a floor compared as a millisecond alone`
		return jr.floor
	}
	return floor
}

// cut is the resume's cut on a struct's floor field, the operand on the left.
func cut(evs []event, bd bounds, joinTS int64) []event {
	var out []event
	for _, e := range evs {
		if bd.floor < e.OriginTs { // want `a floor compared as a millisecond alone`
			out = append(out, e)
		}
	}
	_ = min(joinTS, bd.floor) // want `a floor set by min as a millisecond alone`
	return out
}

// storeFloor is a floor read from a map, and a call that returns one.
func storeFloor(floors map[string]int64, e event) bool {
	return e.OriginTs <= floors["questions"] || e.OriginTs >= mainFloor() // want `a floor compared as a millisecond alone` `a floor compared as a millisecond alone`
}

func mainFloor() int64 { return 0 }

// unwrapped is a Floor's MS compared outside Floor's methods: the pair taken apart.
func unwrapped(f state.Floor, e event) bool {
	return (e.OriginTs) <= (f.MS) // want `a floor compared as a millisecond alone`
}

// fine are the forms that keep the pair: Covers, Max, and a floor only tested for equality.
func fine(f state.Floor, e event, floor int64) bool {
	g := f.Max(state.Floor{MS: e.OriginTs, IDs: []string{e.ID}})
	return g.Covers(e.OriginTs, e.ID) || floor == e.OriginTs || floor > 0
}
