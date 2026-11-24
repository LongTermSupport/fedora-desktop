package sessions

import (
	"context"
	"path/filepath"
	"strings"

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
//
// The record's name= is not held to its file's name, so the file removed is the one the record
// was read from; two files answering to the name remove nothing.
func (a *App) forget(ctx context.Context, args []string) term.ExitCode {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		a.out(usage)
		return term.ExitOK
	}
	if len(args) == 2 && args[0] == endOfOptions {
		args = args[1:]
	}
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
	files := recordFiles(reading, name)
	switch {
	case len(files) == 0:
		return a.usageError((&noRecordError{names: []string{name}}).Error())
	case len(files) > 1:
		a.fail("more than one record answers to " + name + " (" + strings.Join(files, ", ") +
			"), so none is removed: remove the wrong one by hand.")
		return term.ExitFailure
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
	if err = store.RemoveFile(files[0]); err != nil {
		a.failErr(err)
		return term.ExitFailure
	}
	a.out(name + " FORGOTTEN\n")
	return term.ExitOK
}

// recordFiles is every registry file that answers to name, as the restore names records: a
// readable record by its name=, an unreadable one by its file's name.
func recordFiles(reading registry.Reading, name string) []string {
	var files []string
	for i, rec := range reading.Records {
		if rec.Name == name {
			files = append(files, reading.Files[i])
		}
	}
	for _, p := range reading.Problems {
		if filepath.Base(p.File) == name {
			files = append(files, p.File)
		}
	}
	return files
}
