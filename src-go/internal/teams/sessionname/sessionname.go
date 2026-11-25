// Package sessionname is `ccy-teams internal session-name <launcher> <project> <seats> <checkout>`: the
// name of the tmux session a ccy or cc launch starts, which is also what the tab shows
// (files/etc/tmux.conf titles the terminal with #S).
//
//   - N1. A launch in seats is named for them: `<launcher>-<team>-<project>-<seat>`, so
//     `ccy --teams 4@team-a` in the fedora-desktop checkout starts
//     `ccy-team-a-fedora-desktop-4`. Several seats are joined with '_' in list order
//     (`ccy-alpha-proj-1_beta-proj-3`). Team and seat names hold only letters, digits and '-',
//     so nothing is mapped; tmux would not keep a '.' or ':' in a session name.
//   - N2. The launcher (ccy for a container seat, cc for a host one) comes first, not last as
//     in the owner's example (`team-a · fedora-desktop.4 · ccy`): the `ccy-` and `cc-` prefix
//     is how the re-attach offer, ccy-sessions and its declare tell a container session from a
//     host one. No '·' or space: the name is also the session's key in the session registry,
//     the seat store (paths.ParseSessionName) and the agent-bus membership.
//   - N3. The list is read as `ccy-teams internal seat-check` reads it (whitespace removed,
//     membership.ParseSeats), so cc, which passes the list as typed, and ccy, which passes the
//     check's canonical list, name the same seats alike. A launch in no seat, or in a list the
//     check would refuse or that names a team alone (cc's chooser runs inside the session), keeps
//     `<launcher>-<project>`.
//   - N4. A seat-named session name longer than MaxLen keeps `<launcher>-<project>` and says so on stderr:
//     the launcher appends `-<n>` for a second session of the same name, and the seat store
//     takes 128 bytes.
//   - N5. The name printed is the first free one on ccy's tmux server: the name, then
//     `<name>-2`, `<name>-3` ..., matched exactly against every session there. A server that
//     cannot be listed is exit 1 (no server yet is an empty list).
//   - N6. A launcher other than ccy or cc, a project that is not a session-name word, or a
//     checkout that is not an absolute path is exit 64; stdout is one line, the name.
//   - N7. A launch takes over a stopped record of its own (R61): when the session registry holds
//     exactly one record of the same checkout and launcher whose seats are the launch's (both
//     read as N3 reads them, in any order; no seats matches no seats), whose file is its name and
//     whose session is not running, that record's name is printed, so the launcher's record
//     replaces it instead of standing beside it (a declared `ccy-sessions declare --name`, or one
//     an earlier session left), and stderr says so. A record marked fresh-if-no-conversation
//     adds ` yes` to the line, for the launcher to keep the mark. Two such records are neither
//     taken, and stderr names them; an unreadable record is named on stderr and never taken; a
//     registry that cannot be read is exit 1. The re-attach offer handles running sessions.
package sessionname

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// MaxLen bounds a seat-named session name, leaving room for the launcher's `-<n>`.
const MaxLen = 120

var (
	projectWord = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*$`)
	errUsage    = errors.New("internal session-name <ccy|cc> <project> <seats> <checkout>")
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
	parts, ok := seatParts(project, seats)
	if !ok {
		return plain, "", nil
	}
	named := launcher + "-" + strings.Join(parts, "_")
	if len(named) > MaxLen {
		return plain, fmt.Sprintf("the session is named %s: %s is longer than %d characters.", plain, named, MaxLen), nil
	}
	return named, "", nil
}

// seatParts is `<team>-<project>-<seat>` for every seat of the list, the owner's order (team,
// project, seat number) and the one place to change it. The list is read as the seat check
// reads it (whitespace removed, membership.ParseSeats), so cc's list as typed and ccy's
// canonical one name a session alike; false when it names no seat or the check would refuse it.
func seatParts(project, seats string) ([]string, bool) {
	parsed, err := membership.ParseSeats(strings.Join(strings.Fields(seats), ""))
	if err != nil || len(parsed) == 0 {
		return nil, false
	}
	parts := make([]string, 0, len(parsed))
	for _, s := range parsed {
		parts = append(parts, s.Team+"-"+project+"-"+s.Seat)
	}
	return parts, true
}

// Free is the first of base, base-2, base-3 ... that no session in taken has.
func Free(base string, taken []tmuxccy.Session) string {
	used := make(map[string]bool, len(taken))
	for _, s := range taken {
		used[s.Name] = true
	}
	candidate := base
	for n := 2; used[candidate]; n++ {
		candidate = base + "-" + strconv.Itoa(n)
	}
	return candidate
}

// Server lists the sessions on ccy's tmux server.
type Server interface {
	List(ctx context.Context) ([]tmuxccy.Session, error)
}

// canonicalSeats is a --teams list as N3 reads it, sorted, so two lists of the same seats are
// equal in any order; "" for no list, and false for one the seat check would refuse.
func canonicalSeats(list string) (string, bool) {
	joined := strings.Join(strings.Fields(list), "")
	if joined == "" {
		return "", true
	}
	parsed, err := membership.ParseSeats(joined)
	if err != nil || len(parsed) == 0 {
		return "", false
	}
	refs := make([]string, 0, len(parsed))
	for _, s := range parsed {
		refs = append(refs, s.Seat+"@"+s.Team)
	}
	slices.Sort(refs)
	return strings.Join(refs, ","), true
}

// Takeover is the stopped records of reading a launch of launcher in dir and seats would take
// over (N7): same checkout, launcher and seats, the record's file named for it, its session not
// among the running ones.
func Takeover(reading registry.Reading, launcher, dir, seats string, running []tmuxccy.Session) []registry.Record {
	want, ok := canonicalSeats(seats)
	if !ok {
		return nil
	}
	var found []registry.Record
	for i, rec := range reading.Records {
		held, readable := canonicalSeats(discover.RecordTeams(rec))
		switch {
		case rec.Prefix != launcher, rec.Dir != dir, !readable, held != want,
			filepath.Base(reading.Files[i]) != rec.Name,
			slices.ContainsFunc(running, func(s tmuxccy.Session) bool { return s.Name == rec.Name }):
			continue
		}
		found = append(found, rec)
	}
	return found
}

// Command is `ccy-teams internal session-name`.
type Command struct {
	Server Server
	// Records reads the session registry.
	Records func() (registry.Reading, error)
	Stdout  io.Writer
	Stderr  io.Writer
}

// Run prints the session name for the launch, or refuses its arguments (64), or fails (1) when
// the server cannot be listed or the registry read.
func (c Command) Run(ctx context.Context, args []string) term.ExitCode {
	if len(args) != 4 {
		return c.say(term.ExitUsage, errUsage.Error()+". See ccy-teams --help.")
	}
	base, note, err := Name(args[0], args[1], args[2])
	if err == nil && (!filepath.IsAbs(args[3]) || filepath.Clean(args[3]) != args[3]) {
		err = fmt.Errorf("%w: the checkout %q is not an absolute path", errUsage, args[3])
	}
	if err != nil {
		return c.say(term.ExitUsage, err.Error()+". See ccy-teams --help.")
	}
	if note != "" {
		if code := c.say(term.ExitOK, note); code != term.ExitOK {
			return code
		}
	}
	sessions, err := c.Server.List(ctx)
	if err != nil {
		return c.say(term.ExitFailure, "no session name could be chosen: "+err.Error())
	}
	reading, err := c.Records()
	if err != nil {
		return c.say(term.ExitFailure, "no session name could be chosen: "+err.Error())
	}
	line, code := c.choose(reading, args, Free(base, sessions), sessions)
	if code != term.ExitOK {
		return code
	}
	if _, err := fmt.Fprintln(c.Stdout, line); err != nil {
		return term.ExitFailure
	}
	return term.ExitOK
}

// choose is the stdout line: the one record taken over (N7) with its fresh mark, else name.
func (c Command) choose(reading registry.Reading, args []string, name string, sessions []tmuxccy.Session) (string, term.ExitCode) {
	for _, p := range reading.Problems {
		if code := c.say(term.ExitOK, "the session record "+p.File+" cannot be read, so it is not taken over: "+p.Err.Error()); code != term.ExitOK {
			return "", code
		}
	}
	found := Takeover(reading, args[0], args[3], args[2], sessions)
	switch len(found) {
	case 0:
		return name, term.ExitOK
	case 1:
	default:
		names := make([]string, 0, len(found))
		for _, rec := range found {
			names = append(names, rec.Name)
		}
		return name, c.say(term.ExitOK, fmt.Sprintf("the session records %s are all stopped sessions of this checkout in these seats, so none is taken over; the session is named %s.",
			strings.Join(names, ", "), name))
	}
	rec := found[0]
	if code := c.say(term.ExitOK, fmt.Sprintf("the session takes over the stopped record %s, which is this checkout's in these seats, so it replaces that record.", rec.Name)); code != term.ExitOK {
		return "", code
	}
	if rec.FreshIfNoConversation {
		return rec.Name + " yes", term.ExitOK
	}
	return rec.Name, term.ExitOK
}

// say writes one ccy-teams line to stderr and returns code, or a failure when it cannot.
func (c Command) say(code term.ExitCode, msg string) term.ExitCode {
	if _, err := fmt.Fprintln(c.Stderr, "ccy-teams: "+msg); err != nil {
		return term.ExitFailure
	}
	return code
}
