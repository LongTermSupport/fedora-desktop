package find

import (
	"context"
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// RecordSeats is `ccy-teams record-seats <session> <seats>`: the seats a launch took from the
// Quick Launch go into the record of <session>, as an explicit --teams would be, so a restore or
// restart replays them from the record and never from the saved launch choices. The launcher
// names the session (ccy_tmux_current_session, its one rule for which session it IS) and makes
// no call when it is none, so a ccy run from a shell inside another session's pane never writes
// that session's record; the TMUX it inherited plays no part here. A session with no record has
// nothing to restore: not an error. A record that cannot be read or written is.
func (f *Find) RecordSeats(_ context.Context, args []string) term.ExitCode {
	if len(args) != 2 || args[0] == "" {
		f.say("ccy-teams: 'record-seats' takes the session and the seats. See ccy-teams --help.")
		return term.ExitUsage
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
