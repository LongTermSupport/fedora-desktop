// Package discover is the one answer to "which session holds or wants this seat" (Plan 00203
// SPEC-hot-control.md section 7). A seat's identity is (checkout, team, seat): seat 1 of a team
// exists once per checkout. Two sources name a seat's session: a session record's --teams (what
// a restore or restart replays) and a session's desired membership under
// $XDG_RUNTIME_DIR/ccy/membership/<session>/, or a cc session's under
// $XDG_RUNTIME_DIR/pingbus/membership/<session>/ (what a hot add wrote, live). The board, shutdown and
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
	session  map[Key]string
	restore  map[Key]bool
	desired  map[Key][]string
	recorded map[Key]string
}

// New is an empty Index.
func New() *Index {
	return &Index{session: map[Key]string{}, restore: map[Key]bool{}, desired: map[Key][]string{}, recorded: map[Key]string{}}
}

// IsLauncher is whether a record's prefix is one of the two launchers that hold seats.
func IsLauncher(prefix string) bool { return prefix == "ccy" || prefix == "cc" }

// RecordTeams is the --teams value of a record's launch, read from its launcher's own words
// (launcherArgs): ccy's up to a bare "--", every word of cc's.
func RecordTeams(rec registry.Record) string { return teamsIn(launcherArgs(rec)) }

// teamsIn is the last --teams value among the words, all of them the launcher's; the last wins.
func teamsIn(args []string) string {
	list := ""
	for i, arg := range args {
		switch {
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
	for ref := range strings.SplitSeq(RecordTeams(rec), ",") {
		seat, team, ok := strings.Cut(ref, "@")
		if !ok || seat == "" || team == "" {
			continue
		}
		k := Key{Dir: rec.Dir, Team: team, Seat: seat}
		ix.session[k] = rec.Name
		ix.restore[k] = rec.Restore
		ix.recorded[k] = rec.Name
	}
}

// AddDesired notes the seats a live session's desired membership names (the caller adds only
// live sessions'). The live session is the seat's session, over a record of another session that
// names it (Fable's M3 note N4); RecordedBy still names that record, and its restore is not this
// session's. The first live session to want a seat keeps it; DesiredBy lists them all.
func (ix *Index) AddDesired(d membership.Desired) {
	for _, s := range d.Seats {
		k := Key{Dir: d.Checkout, Team: s.Team, Seat: s.Seat}
		first := len(ix.desired[k]) == 0
		if !slices.Contains(ix.desired[k], d.Session) {
			ix.desired[k] = append(ix.desired[k], d.Session)
		}
		if !first || ix.session[k] == d.Session {
			continue
		}
		if ix.recorded[k] != "" && ix.recorded[k] != d.Session {
			delete(ix.restore, k)
		}
		ix.session[k] = d.Session
	}
}

// RecordedBy is the session whose record names the seat, or "".
func (ix *Index) RecordedBy(k Key) string { return ix.recorded[k] }

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

// ReadDesired is every session's desired membership under runtimeDir, ccy's then cc's (H31),
// and a line for each one that could not be read. No membership directory is none.
func ReadDesired(runtimeDir string) ([]membership.Desired, []string, error) {
	if !filepath.IsAbs(runtimeDir) {
		return nil, nil, fmt.Errorf("XDG_RUNTIME_DIR %q is not an absolute path", runtimeDir)
	}
	var found []membership.Desired
	var unreadable []string
	for _, kind := range []string{membership.KindCcy, membership.KindCC} {
		some, bad, err := readKind(runtimeDir, kind)
		if err != nil {
			return nil, nil, err
		}
		found, unreadable = append(found, some...), append(unreadable, bad...)
	}
	return found, unreadable, nil
}

// Current is the desired memberships still owned by the launch of their name (Fable's M6 review
// B1). tmux reuses a session name once it ends, and each launch rewrites the record of its
// name, so a membership counts only while that record carries the bus: a cc record with
// --teams or --bus, a ccy record without --no-bus. A plain cc or a ccy --no-bus under a reused
// name, or a session with no record, owns no membership left behind under it.
func Current(desired []membership.Desired, records []registry.Record) []membership.Desired {
	bus := make(map[string]bool, len(records))
	for _, rec := range records {
		bus[rec.Name] = LaunchedWithBus(rec)
	}
	return slices.DeleteFunc(slices.Clone(desired), func(d membership.Desired) bool { return !bus[d.Session] })
}

const (
	// flagBus is cc's launch on the bus with no team; flagNoBus is ccy's launch without it.
	flagBus   = "--bus"
	flagNoBus = "--no-bus"
)

// LaunchedWithBus is whether a record's launch carries the agent team bus, judged on its
// launcher's own words (launcherArgs).
func LaunchedWithBus(rec registry.Record) bool {
	args := launcherArgs(rec)
	if rec.Prefix != membership.KindCC {
		return !slices.Contains(args, flagNoBus)
	}
	return slices.ContainsFunc(args, func(a string) bool {
		return a == ccyenv.FlagTeams || a == flagBus || strings.HasPrefix(a, ccyenv.FlagTeams+"=")
	})
}

// launcherArgs is the words of a record that are its launcher's (Fable's M6 re-check R1): ccy's
// up to a bare "--", after which they are claude's; every word of cc's, which has no option
// terminator and reads each word as its own.
func launcherArgs(rec registry.Record) []string {
	if rec.Prefix == membership.KindCC {
		return rec.Args
	}
	if end := slices.Index(rec.Args, "--"); end >= 0 {
		return rec.Args[:end]
	}
	return rec.Args
}

// RunningCC is every cc session with a desired membership whose tmux session is live (on ccy's
// server, where cc starts its sessions), and a line for each cc membership that could not be
// read: the cc sessions hot control can change (H31, section 5.1).
func RunningCC(runtimeDir string, live func(string) bool) ([]membership.Desired, []string, error) {
	if !filepath.IsAbs(runtimeDir) {
		return nil, nil, fmt.Errorf("XDG_RUNTIME_DIR %q is not an absolute path", runtimeDir)
	}
	all, unreadable, err := readKind(runtimeDir, membership.KindCC)
	if err != nil {
		return nil, nil, err
	}
	return slices.DeleteFunc(all, func(d membership.Desired) bool { return !live(d.Session) }), unreadable, nil
}

// readKind reads the membership directories of one kind of session, each by its name.
func readKind(runtimeDir, kind string) ([]membership.Desired, []string, error) {
	top := filepath.Dir(membership.DirOf(runtimeDir, kind, "x"))
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
		case d.Kind != kind:
			unreadable = append(unreadable, "the membership in "+filepath.Join(top, e.Name())+" is a "+d.Kind+" session's, among "+kind+"'s")
		default:
			found = append(found, d)
		}
	}
	return found, unreadable, nil
}
