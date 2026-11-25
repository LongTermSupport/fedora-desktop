package conversation

import (
	"errors"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
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
//
// A seated ccy record with no conversation of its own (its end removed the record, or it never
// wrote one) is resumed by its seats' record instead (issue 417, register R4: a clean /exit of a
// teamchat session removes its own state), so it is never started fresh while its seats name a
// conversation; when they name none either, the seat lookup's reason joins the session's.
func StartPlan(rec registry.Record, where Where) (Start, error) {
	id, err := read(rec, where)
	why := ""
	if missing := errors.Is(err, errNotRecorded) || errors.Is(err, errNoTranscript); missing {
		if seated, seatErr := bySeats(rec, where); seatErr == nil {
			id, err, why = seated, nil, "its own record has none; resumed by its seats' record"
		} else if !errors.Is(seatErr, errNotSeated) {
			err = fmt.Errorf("%w; by its seats: %w", err, seatErr)
		}
	}
	switch {
	case err == nil:
		args, argsErr := registry.ResumeArgs(rec.Compose, rec.Prefix, rec.Args, id)
		if argsErr != nil {
			return Start{}, &ArgsError{Err: argsErr}
		}
		return Start{ID: id, Why: why, Args: args}, nil
	case rec.FreshIfNoConversation && (errors.Is(err, errNotRecorded) || errors.Is(err, errNoTranscript)):
		args, argsErr := registry.FreshArgs(rec.Compose, rec.Prefix, rec.Args)
		if argsErr != nil {
			return Start{}, &ArgsError{Err: argsErr}
		}
		return Start{Fresh: true, Why: err.Error(), Args: args}, nil
	}
	return Start{}, byHand(rec, err)
}

// errNotSeated is bySeats' answer for a record that is not a seated ccy launch: it has no seats to
// ask.
var errNotSeated = errors.New("not a seated ccy launch")

// bySeats is the conversation the seats of a seated ccy record resume (ForSeats, in the record's
// checkout).
func bySeats(rec registry.Record, where Where) (string, error) {
	list := discover.RecordTeams(rec)
	if rec.Prefix != membership.KindCcy || list == "" || !discover.LaunchedWithBus(rec) {
		return "", errNotSeated
	}
	got, err := ForSeats(rec.Dir, list, where)
	if err != nil {
		return "", err
	}
	return got.ID, nil
}
