package proto

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

const (
	testAdmin = "@admin:server.test"
	testMain  = "!mainmainmainmainmainmainmainmainmainmainmai"
)

func roomID(c byte) string { return "!" + strings.Repeat(string(c), 43) }

func adminEvent(kind string, content any) pyjson.Object {
	return pyjson.Object{"type": kind, "state_key": "", "sender": testAdmin, "content": content}
}

func TestParseRoomsIndex(t *testing.T) {
	event := adminEvent(TypeRooms, pyjson.Object{"v": intNum(1), PurposeReports: roomID('r')})
	rooms, err := ParseRoomsIndex(event, testAdmin, testMain)
	if err != nil || rooms[PurposeReports] != roomID('r') || len(rooms) != 1 {
		t.Fatal(rooms, err)
	}
	event = adminEvent(TypeRooms, pyjson.Object{"v": intNum(1), PurposeReports: testMain})
	if _, err := ParseRoomsIndex(event, testAdmin, testMain); untrustedReason(err) != "the rooms index room IDs" {
		t.Fatal(err)
	}
	if _, err := ParseRoomsIndex(event, "@x:server.test", testMain); untrustedReason(err) != "the agent_bus.rooms event" {
		t.Fatal(err)
	}
}

func TestCheckRoomLink(t *testing.T) {
	link := pyjson.Object{"v": intNum(1), "team": "team", "purpose": PurposeQuestions, "main": testMain}
	if err := CheckRoomLink(adminEvent(TypeRoomLink, link), testAdmin, "team", PurposeQuestions, testMain); err != nil {
		t.Fatal(err)
	}
	err := CheckRoomLink(adminEvent(TypeRoomLink, link), testAdmin, "team", PurposeReports, testMain)
	if untrustedReason(err) != "the room's back-link" {
		t.Fatal(err)
	}
}

func TestSidePowerLevels(t *testing.T) {
	humans := []string{"@alice:server.test"}
	if _, err := SidePowerLevels("other", humans, true); err == nil {
		t.Fatal("an unknown purpose was given levels")
	}
	closed, err := SidePowerLevels(PurposeQuestions, humans, false)
	if err != nil || pyjson.Canonical(closed["events_default"]) != "i10" {
		t.Fatal(closed, err)
	}
	if err := CheckSidePowerLevels(closed, PurposeQuestions, humans, true); untrustedReason(err) != "power levels" {
		t.Fatal(err)
	}
	if err := CheckSidePowerLevels(closed, "other", humans, true); err == nil || untrustedReason(err) != "" {
		t.Fatal(err)
	}
}

func TestJoinRules(t *testing.T) {
	if err := CheckJoinRules(ExpectedJoinRules(PurposeReports, testMain), PurposeReports, testMain); err != nil {
		t.Fatal(err)
	}
	if err := CheckJoinRules(ExpectedJoinRules(PurposeQuestions, testMain), PurposeReports, testMain); untrustedReason(err) != "join rules" {
		t.Fatal(err)
	}
}
