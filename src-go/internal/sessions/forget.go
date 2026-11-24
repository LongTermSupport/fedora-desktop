package sessions

import (
	"context"
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// cmdForget names the subcommand that removes one session's record (fedora-desktop#396).
const cmdForget = "forget"

// forget removes the named session's record from the registry, so the next restore does not try
// to start it: the way out for a stale record, which would otherwise fail every later restore.
// The name is a record's name, or an unreadable record's file name, as the restore names them;
// one with no record is a usage mistake. A live session's record belongs to that session (its
// trampoline removes it when the session ends), so forgetting it is refused, and so is any
// forget while the live set cannot be read. One line on stdout: "<name> FORGOTTEN".
func (a *App) forget(ctx context.Context, args []string) term.ExitCode {
	if len(args) != 1 {
		return a.usageError(cmdForget + " takes one session name.")
	}
	name := args[0]
	store, err := registry.Locate(a.Getenv)
	if err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	reading, err := store.ReadAll()
	if err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	if _, err = scoped(reading, []string{name}); err != nil {
		var missing *noRecordError
		if errors.As(err, &missing) {
			return a.usageError(missing.Error())
		}
		return a.failed(err)
	}
	sessions, err := a.Tmux.List(ctx)
	if err != nil {
		a.failErr(err)
		a.fail("the live session list could not be read, so " + name + "'s record is kept: it may be a running session's.")
		return term.ExitFailure
	}
	for _, s := range sessions {
		if s.Name == name {
			a.fail(name + " is a live session, so its record is its own and is kept; end the session instead (its record goes when it ends).")
			return term.ExitFailure
		}
	}
	if err = store.Remove(name); err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	a.out(name + " FORGOTTEN\n")
	return term.ExitOK
}
