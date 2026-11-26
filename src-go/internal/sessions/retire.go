package sessions

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// cmdRetire names the subcommand that retires a conversation record no session owns
// (fedora-desktop#431).
const cmdRetire = conversation.RetireVerb

// retireConversation retires the agent-bus conversation record the checkout keeps under a session
// name no record owns (conversation.Retire), so the restore no longer refuses a fresh start in
// that checkout for it: the way out the refusal names, once a person has said that conversation
// is no session's. A name a session record answers to belongs to that session (forget retires
// it), and a live session's to that session, so both are refused, as is any retire while the
// live set cannot be read; so is a name with nothing to retire. One line on stdout:
// "<name> RETIRED".
func (a *App) retireConversation(ctx context.Context, args []string) term.ExitCode {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		a.out(usage)
		return term.ExitOK
	}
	if len(args) == 3 && args[0] == endOfOptions {
		args = args[1:]
	}
	if len(args) != 2 {
		return a.usageError(cmdRetire + " takes a checkout and a session name.")
	}
	checkout, name := args[0], args[1]
	switch {
	case !filepath.IsAbs(checkout):
		return a.usageError("the checkout '" + checkout + "' is not an absolute path.")
	case !membership.ValidSession(name):
		return a.usageError("'" + name + "' is not a session name.")
	}
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
	if files := reading.FilesOf(name); len(files) > 0 {
		a.fail(name + " has a session record (" + strings.Join(files, ", ") + "), so its conversation is its own and is kept; " +
			"if that session is done with, ccy-sessions " + cmdForget + " " + name + " removes its record and retires its conversation.")
		return term.ExitFailure
	}
	sessions, err := a.Tmux.List(ctx)
	if err != nil {
		a.failErr(err)
		a.fail("the live session list could not be read, so " + name + "'s conversation record is kept: it may be a running session's.")
		return term.ExitFailure
	}
	for _, s := range sessions {
		if s.Name == name {
			a.fail(name + " is a live session, so its conversation record is its own and is kept.")
			return term.ExitFailure
		}
	}
	path, retired, err := conversation.Retire(filepath.Clean(checkout), name)
	if err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	if !retired {
		a.fail("there is no conversation record of " + name + " in " + checkout + " (" + path + " is not there); nothing was retired.")
		return term.ExitFailure
	}
	a.sayf("retired %s: it is now %s, beside it, which no restore reads.\n", path, conversation.RetiredFile)
	a.out(name + " RETIRED\n")
	return term.ExitOK
}
