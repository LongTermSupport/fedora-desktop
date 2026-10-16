// Package discover is the one answer to "which session holds or wants this seat" (Plan 00203
// SPEC-hot-control.md section 7). A seat's identity is (checkout, team, seat): seat 1 of a team
// exists once per checkout. Two sources name a seat's session: a session record's --teams (what
// a restore or restart replays) and a session's desired membership under
// $XDG_RUNTIME_DIR/ccy/membership/<session>/ (what a hot add wrote, live). The board, shutdown and
// `ccy-teams session add` all ask this package, so they cannot disagree.
package discover

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccyenv"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
)

// Key is a seat's identity.
type Key struct{ Dir, Team, Seat string }

// Ref is the seat as a --teams item, seat@team.
func (k Key) Ref() string { return k.Seat + "@" + k.Team }

// Index is every seat a record or a desired file names, and the session it names.
type Index struct {
	session map[Key]string
	restore map[Key]bool
	desired map[Key][]string
}

// New is an empty Index.
func New() *Index {
	return &Index{session: map[Key]string{}, restore: map[Key]bool{}, desired: map[Key][]string{}}
}

// IsLauncher is whether a record's prefix is one of the two launchers that hold seats.
func IsLauncher(prefix string) bool { return prefix == "ccy" || prefix == "cc" }

// TeamsList is the --teams value in a launcher's arguments, up to a bare "--"; the last wins.
func TeamsList(args []string) string {
	list := ""
	for i, arg := range args {
		switch {
		case arg == "--":
			return list
		case arg == ccyenv.FlagTeams:
			list = ""
			if i+1 < len(args) {
				list = args[i+1]
			}
		case strings.HasPrefix(arg, ccyenv.FlagTeams+"="):
			list = strings.TrimPrefix(arg, ccyenv.FlagTeams+"=")
		}
	}
	return list
}

// AddRecord notes the seats a ccy or cc record's launch names. A cc record names its seats only
// when cc was given them whole.
func (ix *Index) AddRecord(rec registry.Record) {
	if !IsLauncher(rec.Prefix) {
		return
	}
	for ref := range strings.SplitSeq(TeamsList(rec.Args), ",") {
		seat, team, ok := strings.Cut(ref, "@")
		if !ok || seat == "" || team == "" {
			continue
		}
		k := Key{Dir: rec.Dir, Team: team, Seat: seat}
		ix.session[k] = rec.Name
		ix.restore[k] = rec.Restore
	}
}

// AddDesired notes the seats a session's desired membership names. A seat a record already
// names keeps the record's session; DesiredBy still says this session wants it.
func (ix *Index) AddDesired(d membership.Desired) {
	for _, s := range d.Seats {
		k := Key{Dir: d.Checkout, Team: s.Team, Seat: s.Seat}
		if !slices.Contains(ix.desired[k], d.Session) {
			ix.desired[k] = append(ix.desired[k], d.Session)
		}
		if _, named := ix.session[k]; !named {
			ix.session[k] = d.Session
		}
	}
}

// SessionOf is the session that names the seat, or "".
func (ix *Index) SessionOf(k Key) string { return ix.session[k] }

// Restore is whether the seat's session is started again after a reboot; recorded is false when
// no record names the seat.
func (ix *Index) Restore(k Key) (bool, bool) {
	restores, recorded := ix.restore[k]
	return restores, recorded
}

// DesiredBy is every session whose desired membership names the seat.
func (ix *Index) DesiredBy(k Key) []string { return slices.Clone(ix.desired[k]) }

// SeatsOf is every seat that names session, by checkout, team and seat.
func (ix *Index) SeatsOf(session string) []Key {
	var keys []Key
	for k, s := range ix.session {
		if s == session {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, compare)
	return keys
}

// Keys is every seat named, by checkout, team and seat.
func (ix *Index) Keys() []Key {
	keys := make([]Key, 0, len(ix.session))
	for k := range ix.session {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, compare)
	return keys
}

func compare(a, b Key) int {
	return cmp.Or(strings.Compare(a.Dir, b.Dir), strings.Compare(a.Team, b.Team), strings.Compare(a.Seat, b.Seat))
}

// ReadDesired is every session's desired membership under runtimeDir, and a line for each one
// that could not be read. No membership directory is none.
func ReadDesired(runtimeDir string) ([]membership.Desired, []string, error) {
	if !filepath.IsAbs(runtimeDir) {
		return nil, nil, fmt.Errorf("XDG_RUNTIME_DIR %q is not an absolute path", runtimeDir)
	}
	top := filepath.Dir(membership.Dir(runtimeDir, "x"))
	entries, err := os.ReadDir(top)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("could not list the session memberships in %s: %w", top, err)
	}
	var found []membership.Desired
	var unreadable []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		d, err := membership.Read(filepath.Join(top, e.Name()))
		switch {
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			unreadable = append(unreadable, err.Error())
		case d.Session != e.Name():
			unreadable = append(unreadable, "the membership in "+filepath.Join(top, e.Name())+" names another session, "+d.Session)
		default:
			found = append(found, d)
		}
	}
	return found, unreadable, nil
}
