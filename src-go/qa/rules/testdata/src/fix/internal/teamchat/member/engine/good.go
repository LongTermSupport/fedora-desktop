package engine

import (
	"errors"

	"fix/internal/teamchat/proto"
)

// postRoomNamed decides every room by name, and refuses one it does not know.
func postRoomNamed(e entry, main, reports string) (string, error) {
	switch e.Room {
	case proto.PurposeMain:
		return main, nil
	case proto.PurposeReports:
		return reports, nil
	default:
		return "", errors.New("no room")
	}
}

// onlyMain tests for main itself: what it does is main's alone.
func onlyMain(e entry, last *string, id string) {
	if e.Room == proto.PurposeMain {
		*last = id
	}
	if e.Room != proto.PurposeMain {
		return
	}
}

// addition is a side room's own addition, which takes nothing from any other room.
func addition(e entry, window *int) {
	if e.Room == proto.PurposeReports {
		*window++
	}
}

// lastCase is a side room as the last case: no later case is any other room's.
func lastCase(e entry, questions string) string {
	switch {
	case e.Room == proto.PurposeMain:
		return "main"
	case e.Room == proto.PurposeQuestions && questions != "":
		return questions
	}
	return ""
}

// notARoom compares a constant that is not a room purpose.
func notARoom(role string) bool {
	if role == proto.RoleWorker {
		return true
	}
	return role != proto.RoleWorker
}
