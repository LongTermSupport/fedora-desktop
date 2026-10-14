package find

import (
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// RecordSeats is `ccy-teams record-seats <session> <seats>`: the seats a launch took from the
// Quick Launch go into the session's own registry record, as an explicit --teams would be, so a
// restore or restart replays them from the record and never from the saved launch choices. A
// session with no record, or no name (ccy did not start it under tmux), has nothing to
// restore: not an error.
func (f *Find) RecordSeats(args []string) term.ExitCode {
	if len(args) != 2 {
		f.say("ccy-teams: 'record-seats' takes a session and its seats. See ccy-teams --help.")
		return term.ExitUsage
	}
	if args[0] == "" {
		return term.ExitOK
	}
	store, err := f.Host.Registry()
	if err != nil {
		f.Host.ReportErr(err)
		return term.ExitFailure
	}
	err = store.SetTeams(args[0], args[1])
	if err != nil && !errors.Is(err, registry.ErrNoRecord) {
		f.Host.ReportErr(err)
		return term.ExitFailure
	}
	return term.ExitOK
}
