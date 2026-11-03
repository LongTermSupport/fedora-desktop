package proto

import (
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// The side rooms' purposes (§8.1).
const (
	PurposeReports   = "reports"
	PurposeQuestions = "questions"
)

const reportsAgentPower = 10

// ErrPurpose is a side room purpose that is neither reports nor questions: a caller error.
var ErrPurpose = errors.New("not a side room purpose")

// sideStateContent is SPEC P47's envelope: an admin state event of the type with the empty
// state key, whose content is an object with v the int 1.
func sideStateContent(event any, kind, admin string) (pyjson.Object, error) {
	raw, ok := adminStateContent(event, kind, admin)
	if !ok {
		return nil, distrust("the " + kind + " event")
	}
	content, isObj := raw.(pyjson.Object)
	if !isObj || !isIntOne(content["v"]) {
		return nil, distrust("the " + kind + " content")
	}
	return content, nil
}

// ParseRoomsIndex is SPEC P47: the agent_bus.rooms index of the main room, purpose to room ID.
func ParseRoomsIndex(event any, admin, mainRoom string) (map[string]string, error) {
	content, err := sideStateContent(event, TypeRooms, admin)
	if err != nil {
		return nil, err
	}
	rooms := map[string]string{}
	for key := range content {
		if key != "v" && key != PurposeReports && key != PurposeQuestions {
			return nil, distrust("the rooms index keys")
		}
	}
	seen := map[string]bool{}
	for _, purpose := range []string{PurposeReports, PurposeQuestions} {
		raw, given := content[purpose]
		if !given {
			continue
		}
		room, isStr := raw.(string)
		if !isStr || !IsRoomID(room) || room == mainRoom || seen[room] {
			return nil, distrust("the rooms index room IDs")
		}
		seen[room] = true
		rooms[purpose] = room
	}
	return rooms, nil
}

// CheckRoomLink is SPEC P48: a side room's agent_bus.room back-link names exactly this team,
// purpose and main room.
func CheckRoomLink(event any, admin, team, purpose, mainRoom string) error {
	content, err := sideStateContent(event, TypeRoomLink, admin)
	if err != nil {
		return err
	}
	if !hasExactKeys(content, "v", "team", "purpose", "main") ||
		content["team"] != team || content["purpose"] != purpose || content["main"] != mainRoom {
		return distrust("the room's back-link")
	}
	return nil
}

// SidePowerLevels is SPEC P49: the exact power levels of a side room; open is the questions
// room with a coordinator. Any other purpose is ErrPurpose.
func SidePowerLevels(purpose string, humans []string, open bool) (pyjson.Object, error) {
	switch {
	case purpose == PurposeReports:
		return levelSpec{
			users: usersAt(humans, 0), usersDefault: reportsAgentPower, eventsDefault: reportsAgentPower,
			notify: closedPower, reaction: closedPower, status: closedPower,
		}.content(), nil
	case purpose == PurposeQuestions && open:
		return levelSpec{users: usersAt(humans, humanPower), notify: humanPower, reaction: closedPower}.content(), nil
	case purpose == PurposeQuestions:
		return levelSpec{
			users: usersAt(humans, 0), eventsDefault: reportsAgentPower, notify: humanPower, reaction: closedPower,
		}.content(), nil
	}
	return nil, ErrPurpose
}

// CheckSidePowerLevels is SPEC P49: an UntrustedError unless the content is exactly
// SidePowerLevels, types included; ErrPurpose for an unknown purpose.
func CheckSidePowerLevels(content any, purpose string, humans []string, open bool) error {
	want, err := SidePowerLevels(purpose, humans, open)
	if err != nil {
		return err
	}
	return exactly(content, want, "power levels")
}

// ExpectedJoinRules is SPEC P50: the reports room is joined by the main room's members, any
// other room by invitation.
func ExpectedJoinRules(purpose, mainRoom string) pyjson.Object {
	if purpose == PurposeReports {
		return pyjson.Object{
			"join_rule": "restricted",
			"allow":     []any{pyjson.Object{"type": "m.room_membership", "room_id": mainRoom}},
		}
	}
	return pyjson.Object{"join_rule": "invite"}
}

// CheckJoinRules is SPEC P50: an UntrustedError unless the content is exactly ExpectedJoinRules.
func CheckJoinRules(content any, purpose, mainRoom string) error {
	return exactly(content, ExpectedJoinRules(purpose, mainRoom), "join rules")
}
