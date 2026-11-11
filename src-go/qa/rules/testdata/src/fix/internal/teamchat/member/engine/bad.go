// Package engine stands for teamchat's engine: the sends that pick their room.
package engine

import (
	"errors"

	"fix/internal/teamchat/proto"
)

var errForbidden = errors.New("forbidden")

type entry struct{ Room string }

// postRoom is the originating default: every room that is not the reports room went to main.
func postRoom(e entry, main, reports string) string {
	if e.Room != proto.PurposeReports { // want `project_room_default`
		return main
	}
	return reports
}

// recheck tested for a report and decoded every other send as main's.
func recheck(e entry) string {
	if e.Room == proto.PurposeReports { // want `project_room_default`
		return "report"
	}
	return "main"
}

// withElse is the same default spelt with an else.
func withElse(e entry) string {
	var keys string
	if ready := e.Room != ""; ready && (e.Room == proto.PurposeQuestions) { // want `project_room_default`
		keys = "question"
	} else {
		keys = "main"
	}
	return keys
}

// sendFailed tested a reports 403 before main's, which every other room then took.
func sendFailed(e entry, err error) string {
	switch {
	case errors.Is(err, errForbidden) && e.Room == proto.PurposeReports: // want `project_room_default`
		return "drop the report"
	case errors.Is(err, errForbidden):
		return "stop main"
	}
	return ""
}

// notAside is a negated side room in a plain expression: still every other room.
func notAside(e entry) bool {
	return e.Room != proto.PurposeQuestions // want `project_room_default`
}
