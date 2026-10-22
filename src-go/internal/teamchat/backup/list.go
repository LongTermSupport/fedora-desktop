package backup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// States is health.Backups over d (HB1): the newest backup with both halves and the last run.
func States(d Deps) health.Backups { return states{d} }

type states struct{ d Deps }

// State is HB1: Newest is the highest number with both its meta file and its tar, with the
// meta's time; LastRun is the record's result, or none. Anything unreadable is an error.
func (s states) State(_ context.Context, team paths.TeamID) (health.BackupState, error) {
	metas, tars, err := halves(s.d, team)
	if err != nil {
		return health.BackupState{}, err
	}
	out := health.BackupState{LastRun: status.RunNone}
	for _, n := range slices.Backward(tars) {
		if sec, ok := metas[n]; ok {
			if sec == 0 {
				return health.BackupState{}, fmt.Errorf("the meta file of backup %d of team %s has no time", n, team)
			}
			out.Newest = &status.Newest{ID: n, TimeMS: sec * 1000}
			break
		}
	}
	run, found, err := LastRun(s.d, team)
	if err != nil {
		return health.BackupState{}, err
	}
	if found {
		out.LastRun = run.Result
	}
	return out, nil
}

// halves are Tuwunel's numbers with their times, and the numbers of root's tars, ascending.
func halves(d Deps, team paths.TeamID) (map[int64]int64, []int64, error) {
	metas, err := scanMeta(d, team)
	if err != nil {
		return nil, nil, err
	}
	dir := d.Root + paths.BackupDir(team)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return metas, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("cannot list %s: %w", dir, err)
	}
	var tars []int64
	for _, e := range entries {
		if !tarRe.MatchString(e.Name()) {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSuffix(e.Name(), ".tar"), 10, 64)
		if err != nil {
			return nil, nil, err
		}
		tars = append(tars, n)
	}
	slices.Sort(tars)
	return metas, tars, nil
}

// Entry is one backup number either half names: its time (0 when neither half gives one), and
// whether it is usable, both halves present and the tar valid by BK3.
type Entry struct {
	ID, TimeMS int64
	Usable     bool
}

// Listing is a team's backups newest first, and its last run (nil for none): BV3.
type Listing struct {
	Backups []Entry
	LastRun *Run
}

// List is BV3's listing; it takes no lock and writes nothing. An entry's time is the meta's,
// or for a tar whose meta is gone the time its manifest records.
func List(d Deps, team paths.TeamID) (Listing, error) {
	metas, tars, err := halves(d, team)
	if err != nil {
		return Listing{}, err
	}
	ids := slices.Clone(tars)
	for n := range metas {
		if !slices.Contains(ids, n) {
			ids = append(ids, n)
		}
	}
	slices.Sort(ids)
	var l Listing
	for _, n := range slices.Backward(ids) {
		e := Entry{ID: n, TimeMS: metas[n] * 1000}
		if slices.Contains(tars, n) {
			c, terr := ReadTar(d, team, n)
			_, hasMeta := metas[n]
			e.Usable = terr == nil && hasMeta
			if terr == nil && e.TimeMS == 0 {
				e.TimeMS = c.Manifest.TimeMS
			}
		}
		l.Backups = append(l.Backups, e)
	}
	run, found, err := LastRun(d, team)
	if err != nil {
		return Listing{}, err
	}
	if found {
		l.LastRun = &run
	}
	return l, nil
}

// Value is BV3's answer for team.
func (l Listing) Value(team paths.TeamID) proto.Value {
	items := make([]proto.Value, 0, len(l.Backups))
	for _, e := range l.Backups {
		at := proto.Null()
		if e.TimeMS > 0 {
			at = proto.Int(e.TimeMS)
		}
		items = append(items, proto.Obj(proto.M("id", proto.Int(e.ID)), proto.M("time_ms", at), proto.M("usable", proto.Bool(e.Usable))))
	}
	last := proto.Null()
	if l.LastRun != nil {
		last = l.LastRun.Value()
	}
	return proto.Obj(
		proto.M("v", proto.Int(1)), proto.M("team", proto.Str(team.String())),
		proto.M("backups", proto.Arr(items...)), proto.M("last_run", last),
	)
}
