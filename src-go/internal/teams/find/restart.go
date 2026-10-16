package find

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccyenv"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// maxRestartArgs bounds what restart-args reads: a launcher's argv is a few kilobytes.
const maxRestartArgs = 1 << 20

// RestartArgs is `ccy-teams internal restart-args <session>` (Plan 00203 H5, H22): the launcher
// pipes the arguments of a supervisor-requested relaunch through it, NUL separated, and gets them
// back with the --teams of <session>'s record, which a hot add or remove changed while the
// session ran. The launcher names the session (its one rule for which session it IS; "" for an
// unnamed launch), so the TMUX it inherited plays no part. Unnamed, or with no record, they
// pass unchanged.
func (f *Find) RestartArgs(_ context.Context, args []string, in io.Reader, out io.Writer) term.ExitCode {
	if len(args) != 1 {
		f.say("ccy-teams: 'internal restart-args' takes the session (\"\" for none); it reads the arguments NUL separated on stdin. See ccy-teams --help.")
		return term.ExitUsage
	}
	data, err := io.ReadAll(io.LimitReader(in, maxRestartArgs+1))
	if err != nil || len(data) > maxRestartArgs || (len(data) > 0 && data[len(data)-1] != 0) {
		f.say("ccy-teams: internal restart-args: the arguments on stdin are not a NUL-terminated list.")
		return term.ExitFailure
	}
	words := []string{}
	if len(data) > 0 {
		words = strings.Split(string(data[:len(data)-1]), "\x00")
	}
	teams, found, code := f.recordTeams(args[0])
	if code != term.ExitOK {
		return code
	}
	if found {
		words = ReplaceTeams(words, teams)
	}
	var b bytes.Buffer
	for _, w := range words {
		b.WriteString(w)
		b.WriteByte(0)
	}
	f.Host.Print(out, b.String())
	return term.ExitOK
}

// recordTeams is the --teams of the session's record; found is false for no session or no
// record.
func (f *Find) recordTeams(session string) (string, bool, term.ExitCode) {
	if session == "" {
		return "", false, term.ExitOK
	}
	store, err := f.Host.Registry()
	if err != nil {
		f.Host.ReportErr(err)
		return "", false, term.ExitFailure
	}
	rec, err := store.Get(session)
	if errors.Is(err, registry.ErrNoRecord) {
		return "", false, term.ExitOK
	}
	if err != nil {
		f.Host.ReportErr(err)
		return "", false, term.ExitFailure
	}
	return discover.TeamsList(rec.Args), true, term.ExitOK
}

// ReplaceTeams is args with every launcher --teams (before a bare "--") removed and, unless teams
// is empty, one `--teams <teams>` where the first was, or in front. A flag that takes a value
// consumes the next word, as the launcher's parser does.
func ReplaceTeams(args []string, teams string) []string {
	out := make([]string, 0, len(args)+2)
	at := -1
	for i := 0; i < len(args); i++ {
		word := args[i]
		switch {
		case word == "--":
			out = append(out, args[i:]...)
			return insertTeams(out, at, teams)
		case word == ccyenv.FlagTeams:
			at = firstAt(at, len(out))
			i++
			continue
		case strings.HasPrefix(word, ccyenv.FlagTeams+"="):
			at = firstAt(at, len(out))
			continue
		}
		out = append(out, word)
		class := registry.FlagClass(word)
		if (class == registry.ClassKeepValue || class == registry.ClassDropValue) && i+1 < len(args) {
			i++
			out = append(out, args[i])
		}
	}
	return insertTeams(out, at, teams)
}

func firstAt(at, here int) int {
	if at < 0 {
		return here
	}
	return at
}

func insertTeams(out []string, at int, teams string) []string {
	if teams == "" {
		return out
	}
	return slices.Insert(out, max(at, 0), ccyenv.FlagTeams, teams)
}
