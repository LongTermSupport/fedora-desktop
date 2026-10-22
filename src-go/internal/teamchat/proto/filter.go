package proto

import (
	"maps"
	"slices"
)

// timelineLimit is B 2.6 step 03's page size (A 6.10).
const timelineLimit = 100

var (
	// StateTypes are the trust types of A 6.5, which the state section keeps; with
	// `lazy_load_members` off this member's own `m.room.member` always comes.
	StateTypes = []string{CreateType, PowerLevelsType, JoinRulesType, RosterType, LinkType, MemberType}
	// TimelineTypes are what 7.6 judges (messages, and the admin's redactions) and the trust
	// types.
	TimelineTypes = append([]string{MessageType, RedactionType}, StateTypes...)
)

// SyncFilter is this member's sync filter (A 6.10): main for a worker or the probe, main and
// questions for the non-parked coordinator; presence of the team's humans and members only; no
// ephemeral or account data.
func SyncFilter(r Roster, a Anchors) Value { return syncFilter(r, a, timelineLimit) }

// SyncFilterReAdd is SyncFilter with timeline.limit 0: SY1 (c)'s first sync after a re-add,
// which takes the room's state and none of the history it missed (n3-D5).
func SyncFilterReAdd(r Roster, a Anchors) Value { return syncFilter(r, a, 0) }

func syncFilter(r Roster, a Anchors, limit int64) Value {
	rooms := []Value{Str(a.MainRoom)}
	if coordinator, ok := r.Coordinator(); ok && coordinator == a.Self {
		rooms = append(rooms, Str(r.Rooms.Questions))
	}
	none := Obj(M("types", Arr()))
	return Obj(
		M("account_data", none),
		M("event_format", Str("client")),
		M("presence", Obj(M("senders", strArray(sortedUnion(r.Humans, slices.Collect(maps.Keys(r.Members))))))),
		M("room", Obj(
			M("rooms", Arr(rooms...)),
			M("timeline", Obj(M("types", strArray(TimelineTypes)), M("limit", Int(limit)))),
			M("state", Obj(M("types", strArray(StateTypes)), M("lazy_load_members", Bool(false)))),
			M("ephemeral", none),
			M("account_data", none),
		)),
	)
}
