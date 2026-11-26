// Package conversation stands in for internal/conversation as it was before fedora-desktop#431:
// its StartPlan starts a marked record fresh without asking whether the checkout records the
// session's conversation under another name.
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

// StartPlan starts a marked record with nothing recorded under its own name fresh, asking
// nothing else.
func StartPlan(rec registry.Record, err error) (Start, error) {
	if rec.FreshIfNoConversation && errors.Is(err, errNotRecorded) {
		args, argsErr := registry.FreshArgs(rec.Compose, rec.Prefix, rec.Args) // want `conversation.StartPlan starts a record fresh without asking recordedElsewhere`
		return Start{Fresh: true, Args: args}, argsErr
	}
	return Start{}, err
}
