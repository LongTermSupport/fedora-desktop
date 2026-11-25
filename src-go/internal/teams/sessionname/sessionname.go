// Package sessionname is `ccy-teams internal session-name <launcher> <project> <seats>`: the
// name of the tmux session a ccy or cc launch starts, which is also what the tab shows
// (files/etc/tmux.conf titles the terminal with #S).
//
//   - N1. A launch in seats is named for them: `<launcher>-<team>-<seat>`, every character of
//     the seat that tmux or the seat store would not keep in a session name ('.' and ':' tmux
//     replaces; '+' the store refuses) made '-'. `ccy --teams fedora-desktop.4@team-a` starts
//     `ccy-team-a-fedora-desktop-4`. Several seats are joined with '_' in list order.
//   - N2. The launcher (ccy for a container seat, cc for a host one) comes first, not last as
//     in the owner's example (`team-a · fedora-desktop.4 · ccy`): the `ccy-` and `cc-` prefix
//     is how the re-attach offer, ccy-sessions and its declare tell a container session from a
//     host one. No '·' or space: the name is also the session's key in the session registry,
//     the seat store (paths.ParseSessionName) and the agent-bus membership.
//   - N3. A launch in no seat, or in a list with an entry that names no seat (cc is given a
//     team alone before its chooser runs inside the session), keeps `<launcher>-<project>`.
//   - N4. A seat name longer than MaxLen keeps `<launcher>-<project>` and says so on stderr:
//     the launcher appends `-<n>` for a second session of the same name, and the seat store
//     takes 128 bytes.
//   - N5. A launcher other than ccy or cc, or a project that is not a session-name word, is
//     exit 64; the name is the only line on stdout.
package sessionname

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// MaxLen bounds a seat-named session name, leaving room for the launcher's `-<n>`.
const MaxLen = 120

var (
	projectWord = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*$`)
	unkept      = regexp.MustCompile(`[^A-Za-z0-9_-]`)
	errUsage    = errors.New("internal session-name <ccy|cc> <project> <seats>")
)

// Name is the session name for a launch of launcher in project and seats (a canonical
// `<seat>@<team>,...` list, or empty), and a note for stderr when the seats were not used.
func Name(launcher, project, seats string) (string, string, error) {
	if launcher != "ccy" && launcher != "cc" {
		return "", "", fmt.Errorf("%w: the launcher is %q, not ccy or cc", errUsage, launcher)
	}
	if !projectWord.MatchString(project) {
		return "", "", fmt.Errorf("%w: the project %q is not letters, digits, '_' and '-'", errUsage, project)
	}
	plain := launcher + "-" + project
	parts, ok := seatParts(seats)
	if !ok {
		return plain, "", nil
	}
	named := launcher + "-" + strings.Join(parts, "_")
	if len(named) > MaxLen {
		return plain, fmt.Sprintf("the session is named %s: %s is longer than %d characters.", plain, named, MaxLen), nil
	}
	return named, "", nil
}

// seatParts is `<team>-<seat>` for every entry of the list, or false when an entry names no seat.
func seatParts(seats string) ([]string, bool) {
	if seats == "" {
		return nil, false
	}
	var parts []string
	for entry := range strings.SplitSeq(seats, ",") {
		seat, team, found := strings.Cut(entry, "@")
		if !found || seat == "" || team == "" {
			return nil, false
		}
		parts = append(parts, unkept.ReplaceAllString(team, "-")+"-"+unkept.ReplaceAllString(seat, "-"))
	}
	return parts, true
}

// Run is the command: it prints the name, or refuses its arguments.
func Run(args []string, stdout, stderr io.Writer) term.ExitCode {
	if len(args) != 3 {
		return refuse(stderr, errUsage)
	}
	name, note, err := Name(args[0], args[1], args[2])
	if err != nil {
		return refuse(stderr, err)
	}
	if note != "" {
		if _, err := fmt.Fprintln(stderr, "ccy-teams: "+note); err != nil {
			return term.ExitFailure
		}
	}
	if _, err := fmt.Fprintln(stdout, name); err != nil {
		return term.ExitFailure
	}
	return term.ExitOK
}

func refuse(stderr io.Writer, err error) term.ExitCode {
	if _, werr := fmt.Fprintln(stderr, "ccy-teams: "+err.Error()+". See ccy-teams --help."); werr != nil {
		return term.ExitFailure
	}
	return term.ExitUsage
}
