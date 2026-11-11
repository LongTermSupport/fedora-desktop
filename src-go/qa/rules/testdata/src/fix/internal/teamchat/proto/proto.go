// Package proto stands for teamchat's proto: the room purposes (proto.go) and the one decision
// of a coordinator clause with the places it is written (claim.go).
package proto

// The room purposes.
const (
	PurposeMain      = "main"
	PurposeReports   = "reports"
	PurposeQuestions = "questions"
)

// RoleWorker is a constant that is not a room purpose.
const RoleWorker = "worker"
