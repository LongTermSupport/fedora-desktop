package conversation

import (
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
)

// Start is how a recorded session is started: resuming the conversation ID, or Fresh (ID
// empty) with Why saying what was found in place of a conversation; Args is the launcher's.
type Start struct {
	ID    string
	Fresh bool
	Why   string
	Args  []string
}

// ArgsError is StartPlan's error when the start was decided and the arguments to start it with
// could not be worked out (a record's fault, not a missing conversation).
type ArgsError struct{ Err error }

func (e *ArgsError) Error() string { return e.Err.Error() }

func (e *ArgsError) Unwrap() error { return e.Err }

// StartPlan is the one decision how a recorded session is started, which the boot restore, its
// check, declare and a team restart all take (rule project_start_decision). Its own recorded
// conversation is resumed (Plan 00203 C6, Decision 10: by its recorded id, never --continue).
// A record provisioning marked fresh-if-no-conversation is started fresh when there is no
// conversation to resume: the hook recorded none (Plan 00135 Phase 13), or the one it recorded
// has no transcript, as a session that never took a turn has none (fedora-desktop#270). Any
// other failure of the lookup is For's error, whatever the mark.
func StartPlan(rec registry.Record, claudeHome string) (Start, error) {
	id, err := read(rec, claudeHome)
	switch {
	case err == nil:
		args, argsErr := registry.ResumeArgs(rec.Compose, rec.Prefix, rec.Args, id)
		if argsErr != nil {
			return Start{}, &ArgsError{Err: argsErr}
		}
		return Start{ID: id, Args: args}, nil
	case rec.FreshIfNoConversation && (errors.Is(err, errNotRecorded) || errors.Is(err, errNoTranscript)):
		args, argsErr := registry.FreshArgs(rec.Compose, rec.Prefix, rec.Args)
		if argsErr != nil {
			return Start{}, &ArgsError{Err: argsErr}
		}
		return Start{Fresh: true, Why: err.Error(), Args: args}, nil
	}
	return Start{}, byHand(rec, err)
}
