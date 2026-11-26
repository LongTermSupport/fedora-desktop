// Package conversation stands in for an internal/conversation whose StartPlan asks
// recordedElsewhere and assigns what it finds to the blank identifier, keeping only its error.
package conversation

import (
	"errors"
	"fix/internal/registry"
)

var errNotRecorded = errors.New("no conversation is recorded")

// Start is how a recorded session is started.
type Start struct {
	Fresh bool
	Args  []string
}

// recordedElsewhere is the conversations the checkout records under other session names.
func recordedElsewhere(rec registry.Record) ([]string, error) {
	switch rec.Name {
	case "renamed":
		return []string{"old"}, nil
	case "":
		return nil, errors.New("no name")
	}
	return nil, nil
}

// StartPlan keeps recordedElsewhere's error and drops what it found.
func StartPlan(rec registry.Record, err error) (Start, error) {
	_, scanErr := recordedElsewhere(rec)
	if scanErr != nil {
		return Start{}, scanErr
	}
	if rec.FreshIfNoConversation && errors.Is(err, errNotRecorded) {
		args, argsErr := registry.FreshArgs(rec.Compose, rec.Prefix, rec.Args) // want `conversation.StartPlan starts a record fresh without asking recordedElsewhere`
		return Start{Fresh: true, Args: args}, argsErr
	}
	return Start{}, err
}
