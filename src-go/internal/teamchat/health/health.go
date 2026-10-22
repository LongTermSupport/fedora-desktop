// Package health is the root health tick, `teamchat-admin health --all` (Plan 00209
// SPEC-n2e.md HT1-HT6, DESIGN-v2-C-host.md 3.11.1-3.11.2): for every stamped team, at most four
// at once, the unit state and the loopback probe with the bounded restart, the team's view from
// the child, the backups and the free space, then under the team lock taken without waiting the
// LXC disconnected notices and the status record, written only if no other writer wrote since.
// systemd, the child, the backups and the guest probe are seams; this package holds the rules.
package health

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// MaxParallel bounds the teams ticked at once (n2e-D5).
const MaxParallel = 4

// The results of one team's tick.
const (
	ResultWritten = "written"
	ResultDropped = "dropped"
	ResultFailed  = "failed"
)

// Clock is the tick's time: now, and the waits between probes.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// Team is a stamped team as the tick needs it: its display name, its origin for a first record,
// and when it was applied.
type Team struct {
	ID        paths.TeamID
	Name      string
	Origin    string
	AppliedMS int64
}

// Teams is the stamps: every stamped team, and whether one is still stamped.
type Teams interface {
	Stamped(ctx context.Context) ([]Team, error)
	Stamp(ctx context.Context, team paths.TeamID) (bool, error)
}

// View is the child's read of a team (section 4 of the spec): its members (online unset), its
// humans, and the problems it found.
type View struct {
	Members  []status.Member
	Humans   []status.Human
	Problems []status.Problem
}

// Viewer starts the child's read-only view of a team. A failure wraps ErrViewFiles when the
// team's own files cannot be read, ErrViewChild when the child failed or gave no answer, and is
// otherwise the homeserver's (a *matrix.Error or the context's).
type Viewer interface {
	View(ctx context.Context, team paths.TeamID) (View, error)
}

// The Viewer's own failure classes, whose texts a record carries.
var (
	ErrViewFiles = errors.New("the team's files could not be read")
	ErrViewChild = errors.New("the view's child did not answer")
)

// viewProblem is HT3's problem for a view that failed, its class and the fix for that class.
func viewProblem(team paths.TeamID, err error) status.Problem {
	what, health := "the team's rooms could not be read: ", "teamchat-admin health "+team.String()
	switch {
	case errors.Is(err, ErrViewFiles):
		return status.Problem{What: what + ErrViewFiles.Error(), Fix: health + ", then apply the team again (" + paths.PlayFile + ")"}
	case errors.Is(err, ErrViewChild):
		return status.Problem{What: what + ErrViewChild.Error(), Fix: health}
	}
	return status.Problem{What: what + matrix.Class(err), Fix: health + ", then systemctl status " + paths.ServerUnit(team)}
}

// BackupState is the newest usable backup (nil for none) and the last run's result.
type BackupState struct {
	Newest  *status.Newest
	LastRun string
}

// Backups answers a team's backups (C 3.12.1).
type Backups interface {
	State(ctx context.Context, team paths.TeamID) (BackupState, error)
}

// Space is the free and the total bytes of /var/lib's filesystem.
type Space interface {
	Space() (free, total int64, err error)
}

// Unlocker frees a team lock.
type Unlocker interface{ Unlock() error }

// Locks takes a team's lock without waiting; a busy lock is teamlock.ErrBusy.
type Locks interface {
	TryLock(team paths.TeamID) (Unlocker, error)
}

// Config is what a tick is made from; every part is required.
type Config struct {
	Clock     Clock
	Teams     Teams
	Units     Units
	Prober    Prober
	Viewer    Viewer
	Backups   Backups
	Space     Space
	Guests    Guests
	Announcer Announcer
	Locks     Locks
	// StatusPath and LXCOnlinePath name a team's two files (paths.StatusFile and
	// paths.LXCOnlineFile on the host).
	StatusPath    func(paths.TeamID) string
	LXCOnlinePath func(paths.TeamID) string
	// GID is the owner group the status record is given.
	GID int
}

// Tick is the health tick.
type Tick struct{ cfg Config }

// New is a tick; a config that lacks a part is refused.
func New(cfg Config) (*Tick, error) {
	parts := []any{cfg.Clock, cfg.Teams, cfg.Units, cfg.Prober, cfg.Viewer, cfg.Backups, cfg.Space, cfg.Guests, cfg.Announcer, cfg.Locks}
	if slices.Contains(parts, nil) || cfg.StatusPath == nil || cfg.LXCOnlinePath == nil {
		return nil, errors.New("health: every part of the config is required")
	}
	return &Tick{cfg: cfg}, nil
}

// Outcome is one team's tick: written, dropped (and why) or failed (and the error).
type Outcome struct {
	Team   paths.TeamID
	Result string
	Why    string
	Err    error
}

// RunAll ticks every stamped team, at most MaxParallel at once; a team's failure is its own
// outcome and never stops another's (HT1, DC7). The outcomes are in the stamps' order.
func (t *Tick) RunAll(ctx context.Context) ([]Outcome, error) {
	teams, err := t.cfg.Teams.Stamped(ctx)
	if err != nil {
		return nil, fmt.Errorf("health: the stamped teams: %w", err)
	}
	out := make([]Outcome, len(teams))
	slots := make(chan struct{}, MaxParallel)
	var wg sync.WaitGroup
	for i, team := range teams {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			out[i] = t.Team(ctx, team)
		})
	}
	wg.Wait()
	return out, nil
}

// facts is what steps 1 to 4 found, before the lock.
type facts struct {
	prev   *status.Record
	seq    int64
	unit   UnitState
	probe  string
	events []status.Event
	view   View
	// viewFailed leaves step 6 alone: with no view there is no member to move on, and
	// lxc-online.json is kept as it was for the next tick.
	viewFailed bool
	guests     map[string]guestSeen
	problems   []status.Problem
	backup     status.Backup
	disk       status.Disk
	restarted  bool
}

// Team ticks one team (C 3.11.2).
func (t *Tick) Team(ctx context.Context, team Team) Outcome {
	f, err := t.gather(ctx, team)
	if errors.Is(err, errRemoved) {
		return Outcome{Team: team.ID, Result: ResultDropped, Why: errRemoved.Error()}
	}
	if err != nil {
		return Outcome{Team: team.ID, Result: ResultFailed, Err: err}
	}
	return t.write(ctx, team, f)
}

// gather is steps 1 to 4, without the team lock and changing nothing but a restart.
func (t *Tick) gather(ctx context.Context, team Team) (*facts, error) {
	f := &facts{}
	prev, found, err := status.Read(t.cfg.StatusPath(team.ID), team.ID)
	if err == nil && found {
		f.prev, f.seq = &prev, prev.Seq
	}
	if hsErr := t.homeserver(ctx, team, f); hsErr != nil {
		return nil, hsErr
	}
	view, err := t.cfg.Viewer.View(ctx, team.ID)
	if err != nil {
		f.viewFailed = true
		f.problems = append(f.problems, viewProblem(team.ID, err))
	} else {
		f.view = view
		f.problems = append(f.problems, view.Problems...)
	}
	f.guests = t.probeGuests(ctx, team.ID, f)
	if err := t.backups(ctx, team, f); err != nil {
		return nil, err
	}
	return f, t.disk(f)
}

// backups is step 4's backups: older than BackupAge, a failed last run, or none BackupAge after
// the team was applied, is a problem.
func (t *Tick) backups(ctx context.Context, team Team, f *facts) error {
	state, err := t.cfg.Backups.State(ctx, team.ID)
	if err != nil {
		return fmt.Errorf("health: the backups of %s: %w", team.ID, err)
	}
	f.backup = status.Backup{Newest: state.Newest, LastRun: state.LastRun}
	floor := t.cfg.Clock.Now().Add(-BackupAge).UnixMilli()
	fix := "systemctl status teamchat-backup.service"
	switch {
	case state.LastRun == status.RunFailed:
		f.problems = append(f.problems, status.Problem{What: "the backup's last run failed", Fix: fix})
	case state.Newest == nil && team.AppliedMS < floor:
		f.problems = append(f.problems, status.Problem{What: "the backup has not run since the team was applied", Fix: fix})
	case state.Newest != nil && state.Newest.TimeMS < floor:
		f.problems = append(f.problems, status.Problem{What: "the backup is older than 36 hours", Fix: fix})
	}
	return nil
}

// BackupAge is how old the newest backup may be (C 3.11.2 step 4: 1.5 days).
const BackupAge = 36 * time.Hour

// MinFree is the free space below which /var/lib is a problem, unless a tenth is larger.
const MinFree = 2 << 30

// disk is step 4's free space: below the larger of a tenth and MinFree is a problem.
func (t *Tick) disk(f *facts) error {
	free, total, err := t.cfg.Space.Space()
	if err != nil {
		return fmt.Errorf("health: the free space of /var/lib: %w", err)
	}
	if total <= 0 || free < 0 || free > total || total > math.MaxInt64/100 {
		return errors.New("health: the free space of /var/lib is not a fraction of its size")
	}
	f.disk = status.Disk{FreeBytes: free, FreePct: free * 100 / total}
	if free < max(total/10, MinFree) {
		f.problems = append(f.problems, status.Problem{What: "free space on /var/lib is low", Fix: "free space on the filesystem of /var/lib"})
	}
	return nil
}

// write is steps 5 and 6, under the team lock taken without waiting.
func (t *Tick) write(ctx context.Context, team Team, f *facts) Outcome {
	lock, err := t.cfg.Locks.TryLock(team.ID)
	if err != nil {
		return dropOrFail(team.ID, err)
	}
	out := t.locked(ctx, team, f)
	if err := lock.Unlock(); err != nil && out.Err == nil {
		out = Outcome{Team: team.ID, Result: ResultFailed, Err: err}
	}
	return out
}

func dropOrFail(team paths.TeamID, err error) Outcome {
	if isBusy(err) {
		return Outcome{Team: team, Result: ResultDropped, Why: "busy: " + err.Error()}
	}
	return Outcome{Team: team, Result: ResultFailed, Err: err}
}

func isBusy(err error) bool { return errors.Is(err, teamlock.ErrBusy) }

func (t *Tick) locked(ctx context.Context, team Team, f *facts) Outcome {
	stamped, err := t.cfg.Teams.Stamp(ctx, team.ID)
	if err != nil {
		return Outcome{Team: team.ID, Result: ResultFailed, Err: err}
	}
	if !stamped {
		return Outcome{Team: team.ID, Result: ResultDropped, Why: "the team was removed"}
	}
	path := t.cfg.StatusPath(team.ID)
	now, found, err := status.Read(path, team.ID)
	current := int64(0)
	if err == nil && found {
		current = now.Seq
	}
	if current != f.seq {
		return Outcome{Team: team.ID, Result: ResultDropped, Why: "another writer wrote the record"}
	}
	var members []status.Member
	var problems []status.Problem
	if !f.viewFailed {
		if members, problems, err = t.online(ctx, team.ID, f); err != nil {
			return Outcome{Team: team.ID, Result: ResultFailed, Err: err}
		}
	}
	rec := t.record(team, f, members, problems)
	if err := status.Write(path, rec, t.cfg.GID); err != nil {
		return Outcome{Team: team.ID, Result: ResultFailed, Err: err}
	}
	return Outcome{Team: team.ID, Result: ResultWritten}
}

// record is the status record this tick writes after f.prev.
func (t *Tick) record(team Team, f *facts, members []status.Member, problems []status.Problem) status.Record {
	rec := t.base(team, f.prev)
	rec.Homeserver = status.Homeserver{Unit: f.unit.Active, NRestarts: f.unit.NRestarts, Probe: f.probe}
	if !f.unit.ActiveSince.IsZero() {
		rec.Homeserver.SinceMS = f.unit.ActiveSince.UnixMilli()
	}
	rec.Members, rec.Humans = members, f.view.Humans
	rec.Backup, rec.Disk = f.backup, f.disk
	rec.Problems = slices.Concat(f.problems, problems)
	rec.Events = status.KeepEvents(slices.Concat(rec.Events, f.events), t.cfg.Clock.Now())
	return rec
}

// base is the next record after prev with the name and the origin set: a field another writer
// owns (the migration's, the restore's) is kept as it was.
func (t *Tick) base(team Team, prev *status.Record) status.Record {
	rec := status.Next(prev, team.ID, t.cfg.Clock.Now())
	rec.Name = team.Name
	if prev == nil {
		rec.Origin = team.Origin
	}
	return rec
}
