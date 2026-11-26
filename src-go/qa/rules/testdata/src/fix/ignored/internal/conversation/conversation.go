// Package conversation stands in for an internal/conversation whose StartPlan asks
// recordedElsewhere and drops its answer, so the fresh start is as unguarded as it was before
// fedora-desktop#431.
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
func recordedElsewhere(rec registry.Record) []string {
	if rec.Name == "renamed" {
		return []string{"old"}
	}
	return nil
}

// StartPlan calls recordedElsewhere as a statement, so nothing it finds stops the fresh start.
func StartPlan(rec registry.Record, err error) (Start, error) {
	recordedElsewhere(rec)
	if rec.FreshIfNoConversation && errors.Is(err, errNotRecorded) {
		args, argsErr := registry.FreshArgs(rec.Compose, rec.Prefix, rec.Args) // want `conversation.StartPlan starts a record fresh without asking recordedElsewhere`
		return Start{Fresh: true, Args: args}, argsErr
	}
	return Start{}, err
}
