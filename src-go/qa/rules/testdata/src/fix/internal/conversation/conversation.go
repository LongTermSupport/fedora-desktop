// Package conversation stands in for internal/conversation: the conversation a recorded session
// is in, and StartPlan, the one place a recorded session's start is decided.
package conversation

import (
	"errors"
	"fix/internal/registry"
)

// ErrNotRecorded is one of For's errors for "no conversation".
var ErrNotRecorded = errors.New("no conversation is recorded")

// errNoTranscript is the other.
var errNoTranscript = errors.New("no transcript")

// For is the recorded conversation id.
func For(rec registry.Record) (string, error) {
	if rec.Name == "" {
		return "", ErrNotRecorded
	}
	return rec.Name, nil
}

// Start is how a recorded session is started.
type Start struct {
	ID    string
	Fresh bool
	Args  []string
}

// recordedElsewhere is the conversations the checkout records under other session names.
func recordedElsewhere(rec registry.Record) []string {
	if rec.Name == "renamed" {
		return []string{"old"}
	}
	return nil
}

// StartPlan decides it, and is the one place that may; it asks recordedElsewhere before a
// fresh start.
func StartPlan(rec registry.Record) (Start, error) {
	id, err := For(rec)
	switch {
	case err == nil:
		args, argsErr := registry.ResumeArgs(rec.Compose, rec.Prefix, rec.Args, id)
		return Start{ID: id, Args: args}, argsErr
	case rec.FreshIfNoConversation && (errors.Is(err, ErrNotRecorded) || errors.Is(err, errNoTranscript)) && len(recordedElsewhere(rec)) == 0:
		args, argsErr := registry.FreshArgs(rec.Compose, rec.Prefix, rec.Args)
		return Start{Fresh: true, Args: args}, argsErr
	}
	return Start{}, err
}

// elsewhere in the package is not the planner either.
func elsewhere(rec registry.Record) []string {
	args, err := registry.FreshArgs(rec.Compose, rec.Prefix, rec.Args) // want `a recorded session's start is decided in conversation.StartPlan alone`
	if err != nil {
		return nil
	}
	return args
}
