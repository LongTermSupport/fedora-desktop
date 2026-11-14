// Package badstart decides a recorded session's start outside conversation.StartPlan: the
// restore's planner as it was (fedora-desktop#270), which started a record marked
// fresh-if-no-conversation fresh on one form of "no conversation" and refused the other, and a
// team restart that resumed by the recorded conversation only.
package badstart

import (
	"errors"

	"fix/internal/conversation"
	"fix/internal/registry"
)

// planRestore is the defect's shape.
func planRestore(rec registry.Record) ([]string, error) {
	id, err := conversation.For(rec)
	switch {
	case err == nil:
		return registry.ResumeArgs(rec.Compose, rec.Prefix, rec.Args, id) // want `a recorded session's start is decided in conversation.StartPlan alone`
	case rec.FreshIfNoConversation && errors.Is(err, conversation.ErrNotRecorded): // want `a recorded session's start is decided in conversation.StartPlan alone`
		return registry.FreshArgs(rec.Compose, rec.Prefix, rec.Args) // want `a recorded session's start is decided in conversation.StartPlan alone`
	}
	return nil, err
}

// prepareStart resumes by the recorded conversation only.
func prepareStart(rec registry.Record) ([]string, error) {
	id, err := conversation.For(rec)
	if err != nil {
		return nil, err
	}
	return registry.ResumeArgs(rec.Compose, rec.Prefix, rec.Args, id) // want `a recorded session's start is decided in conversation.StartPlan alone`
}

// held keeps a builder as a value and calls it later.
func held(rec registry.Record) ([]string, error) {
	build := registry.FreshArgs // want `a recorded session's start is decided in conversation.StartPlan alone`
	return build(rec.Compose, rec.Prefix, rec.Args)
}

// compared keys a decision on a lookup's error held in a variable.
var notRecorded = conversation.ErrNotRecorded // want `a recorded session's start is decided in conversation.StartPlan alone`

func compared(err error) bool { return err == notRecorded }
