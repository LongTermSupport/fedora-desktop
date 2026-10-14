package find

import (
	"context"
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// RecordSeats is `ccy-teams record-seats <seats>`: the seats a launch took from the Quick
// Launch go into the record of the ccy session it runs in (found from the TMUX the launcher
// inherited), as an explicit --teams would be, so a restore or restart replays them from the
// record and never from the saved launch choices. Outside a ccy tmux session, or for a session
// with no record, there is nothing to restore: not an error. A session that cannot be named, or
// a record that cannot be read or written, is.
func (f *Find) RecordSeats(ctx context.Context, args []string) term.ExitCode {
	if len(args) != 1 {
		f.say("ccy-teams: 'record-seats' takes the seats. See ccy-teams --help.")
		return term.ExitUsage
	}
	session, err := f.Host.Tmux.CurrentSession(ctx, f.Host.Getenv("TMUX"))
	if err != nil {
		f.Host.ReportErr(err)
		return term.ExitFailure
	}
	if session == "" {
		return term.ExitOK
	}
	store, err := f.Host.Registry()
	if err != nil {
		f.Host.ReportErr(err)
		return term.ExitFailure
	}
	err = store.SetTeams(session, args[0])
	if err != nil && !errors.Is(err, registry.ErrNoRecord) {
		f.Host.ReportErr(err)
		return term.ExitFailure
	}
	return term.ExitOK
}
