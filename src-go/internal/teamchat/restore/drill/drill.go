package drill

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
)

// ErrFailed is a drill that ran to its end with a check failed: no proof record is written.
var ErrFailed = errors.New("restore drill: a check failed")

// errForced is the drill's own failure, which AfterSwap answers in the first restore so that
// RS11's revert is seen.
var errForced = errors.New("the restore drill's forced failure")

// Credential is a member's credential: its handle and its secret (a token or a password). The
// secret is never printed.
type Credential struct{ Handle, Secret string }

// Seed is what the drill put in the scratch team before backup N: the events it posted, the
// credential of a member left alone, of a member reissued after N, and of a human whose password
// is reset after N.
type Seed struct {
	Events                []string
	Kept, Reissued, Human Credential
}

// Hooks are the restore engine's drill hooks (restore.Deps.OnStaged and AfterSwap).
type Hooks struct {
	OnStaged, AfterSwap func(ctx context.Context) error
}

// Rig is every effect of the drill on the machine, each a seam the tests fake: what it does to
// the scratch team, and what it asks of it.
type Rig interface {
	Machine
	Probe
}

// Machine is what the drill does to the machine and its scratch team.
type Machine interface {
	// Lock is the drill's one lock, held from before the start check to after the clean-up.
	Lock(ctx context.Context) (release func() error, err error)
	// Apply declares, approves and applies the scratch team on port.
	Apply(ctx context.Context, team paths.TeamID, port int) error
	// Seed adds two token members and a human and posts messages.
	Seed(ctx context.Context, team paths.TeamID) (Seed, error)
	// Backup takes backup N.
	Backup(ctx context.Context, team paths.TeamID) (int64, error)
	// Restore is the restore engine's fresh restore of backup n with h.
	Restore(ctx context.Context, team paths.TeamID, n int64, h Hooks) error
	// Discard is restore --discard.
	Discard(ctx context.Context, team paths.TeamID) error
	// Change reissues s.Reissued and resets s.Human's password.
	Change(ctx context.Context, team paths.TeamID, s Seed) error
	// Cleaner is the clean-up of a marked scratch team.
	Cleaner() Cleaner
}

// Probe is what the drill asks of the scratch team.
type Probe interface {
	// Floors is each seat's teamchat.floor room account data.
	Floors(ctx context.Context, team paths.TeamID) (map[string]string, error)
	// Owners is the owner of each file of the team's state directory.
	Owners(ctx context.Context, team paths.TeamID) (map[string]uint32, error)
	// LiveEpoch is the restore_epoch of the roster event the host reads from the live server on
	// 127.0.0.1:<port>.
	LiveEpoch(ctx context.Context, team paths.TeamID) (string, error)
	// MemberReach is whether anything answers on the team's port from a member's address.
	MemberReach(ctx context.Context, team paths.TeamID) (bool, error)
	// JoinedEpoch is the restore_epoch a unit joined to the staging unit's namespace reads.
	JoinedEpoch(ctx context.Context, team paths.TeamID) (string, error)
	// Staged is whether the team's restore record is at staged, its staged copy kept.
	Staged(ctx context.Context, team paths.TeamID) (bool, error)
	// MissingEvents is those of events the live server does not hold.
	MissingEvents(ctx context.Context, team paths.TeamID, events []string) ([]string, error)
	// Accepts is whether the live server accepts c (whoami for a token, a login for a password).
	Accepts(ctx context.Context, team paths.TeamID, c Credential) (bool, error)
}

// Clock is the time the proof records.
type Clock interface{ Now() time.Time }

// Config is one drill: the machine's root and the uid its records are owned by, the random
// source of the scratch id, the clock, the key the proof is evidence about, where it runs and the
// declared drill port.
type Config struct {
	Root  string
	UID   uint32
	Rand  io.Reader
	Clock Clock
	Key   restore.Key
	Where restore.Where
	Port  int
}

// Outcome is what a drill found: its team, the verdict of each clause that passed or was
// skipped, what failed in each clause that failed, and whether the proof record was written.
type Outcome struct {
	Team    paths.TeamID
	Checks  map[restore.Clause]restore.Verdict
	Failed  map[restore.Clause][]string
	Written bool
}

// skipped are the clauses no drill attempts yet: the pool seat while Gate B holds, the held
// session until Task 4.4.
var skipped = []restore.Clause{restore.ClausePoolSeat, restore.ClauseHeldSession}

type run struct {
	rig  Rig
	team paths.TeamID
	out  Outcome
}

func (r *run) failf(c restore.Clause, format string, args ...any) {
	r.out.Failed[c] = append(r.out.Failed[c], fmt.Sprintf(format, args...))
}

// Run is the drill (issue #324): under its lock, the start check, a marked scratch team applied
// and seeded, backup N, the changes after it, a restore forced to fail and discarded, a restore
// that finishes, each check, and the clean-up. The proof record is written only when every
// clause attempted passed and the clean-up succeeded; a failed check is ErrFailed, a failed
// operation its own error.
func Run(ctx context.Context, cfg Config, rig Rig) (Outcome, error) {
	release, err := rig.Lock(ctx)
	if err != nil {
		return Outcome{}, fmt.Errorf("the restore drill's lock: %w", err)
	}
	out, err := locked(ctx, cfg, rig)
	return out, errors.Join(err, release())
}

func locked(ctx context.Context, cfg Config, rig Rig) (Outcome, error) {
	if err := CheckStart(cfg.Root, cfg.UID); err != nil {
		return Outcome{}, err
	}
	team, err := NewTeam(cfg.Rand)
	if err != nil {
		return Outcome{}, err
	}
	if err = WriteMarker(cfg.Root, team, Marker{Digest: cfg.Key.Software, StartedMS: cfg.Clock.Now().UnixMilli()}); err != nil {
		return Outcome{}, err
	}
	r := &run{rig: rig, team: team, out: Outcome{Team: team, Failed: map[restore.Clause][]string{}}}
	err = r.flow(ctx, cfg.Port)
	if _, cerr := Clean(ctx, cfg.Root, cfg.UID, rig.Cleaner()); cerr != nil {
		err = errors.Join(err, fmt.Errorf("%w; teamchat-admin restore-drill --clean removes it", cerr))
	}
	if err != nil {
		return r.out, err
	}
	if len(r.out.Failed) > 0 {
		return r.out, fmt.Errorf("%w: %s", ErrFailed, r.failures())
	}
	r.out.Checks = map[restore.Clause]restore.Verdict{}
	for _, c := range restore.Clauses {
		r.out.Checks[c] = restore.VerdictPass
	}
	for _, c := range skipped {
		r.out.Checks[c] = restore.VerdictSkipped
	}
	p := restore.Proof{Key: cfg.Key, Checks: r.out.Checks, PassedMS: cfg.Clock.Now().UnixMilli(), Where: cfg.Where}
	if err = restore.WriteProof(cfg.Root, p); err != nil {
		return r.out, fmt.Errorf("the restore proof record: %w", err)
	}
	r.out.Written = true
	return r.out, nil
}

func (r *run) failures() string {
	var parts []string
	for _, c := range restore.Clauses {
		if f, failed := r.out.Failed[c]; failed {
			parts = append(parts, string(c)+" ("+strings.Join(f, "; ")+")")
		}
	}
	return strings.Join(parts, ", ")
}

// before is the team as backup N left it, and the live epoch and owners before any restore.
type before struct {
	seed   Seed
	n      int64
	floors map[string]string
	epoch  string
	owners map[string]uint32
}

func (r *run) flow(ctx context.Context, port int) error {
	rig, t := r.rig, r.team
	if err := rig.Apply(ctx, t, port); err != nil {
		return err
	}
	var b before
	var err error
	if b.seed, err = rig.Seed(ctx, t); err != nil {
		return err
	}
	if b.n, err = rig.Backup(ctx, t); err != nil {
		return err
	}
	if b.floors, err = rig.Floors(ctx, t); err != nil {
		return err
	}
	if b.epoch, err = rig.LiveEpoch(ctx, t); err != nil {
		return err
	}
	if b.owners, err = rig.Owners(ctx, t); err != nil {
		return err
	}
	if err = rig.Change(ctx, t, b.seed); err != nil {
		return err
	}
	if err = r.forced(ctx, b); err != nil {
		return err
	}
	if err = rig.Restore(ctx, t, b.n, Hooks{}); err != nil {
		return err
	}
	return r.restored(ctx, b)
}

// forced is the first restore: H-C5's and H-C15.reach's probes with the copy staged, then the
// forced failure, which must be reverted with the live team unchanged and the staged copy kept,
// then discarded.
func (r *run) forced(ctx context.Context, b before) error {
	rig, t := r.rig, r.team
	hooks := Hooks{
		OnStaged:  func(ctx context.Context) error { return r.atStaged(ctx, b) },
		AfterSwap: func(context.Context) error { return errForced },
	}
	err := rig.Restore(ctx, t, b.n, hooks)
	switch {
	case err == nil:
		r.failf(restore.ClauseHC5, "the forced failure after the swap did not fail the restore")
		return nil
	case !errors.Is(err, errForced):
		return err
	}
	staged, err := rig.Staged(ctx, t)
	if err != nil {
		return err
	}
	live, err := rig.LiveEpoch(ctx, t)
	if err != nil {
		return err
	}
	if live != b.epoch {
		r.failf(restore.ClauseHC5, "after the revert the live server answered %s, not %s", live, b.epoch)
	}
	if !staged {
		r.failf(restore.ClauseHC5, "after the revert the copy was not kept staged")
		return nil
	}
	return rig.Discard(ctx, t)
}

// atStaged is the probe with the copy staged: no file changed owner, the host reaches the live
// server only, nothing answers a member's address, and the joined unit reaches the staged copy.
func (r *run) atStaged(ctx context.Context, b before) error {
	rig, t := r.rig, r.team
	owners, err := rig.Owners(ctx, t)
	if err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(owners)) {
		if was, ok := b.owners[name]; !ok || was != owners[name] {
			r.failf(restore.ClauseHC5, "%s has a changed owner (%d) with the copy staged", name, owners[name])
		}
	}
	live, err := rig.LiveEpoch(ctx, t)
	if err != nil {
		return err
	}
	if live != b.epoch {
		r.failf(restore.ClauseHC5, "with the copy staged the live server answered %s, not %s", live, b.epoch)
	}
	reached, err := rig.MemberReach(ctx, t)
	if err != nil {
		return err
	}
	if reached {
		r.failf(restore.ClauseHC5, "a member address reached the team's port with the copy staged")
	}
	joined, err := rig.JoinedEpoch(ctx, t)
	if err != nil {
		return err
	}
	if joined == "" || joined == b.epoch {
		r.failf(restore.ClauseReach, "the joined unit read epoch %q, not the staged copy's", joined)
	}
	return nil
}

// restored is the checks after the restore that finished: backup N's history and its kept
// credential (H-C5), the revocations, the floors read before any reissue, and the epoch.
func (r *run) restored(ctx context.Context, b before) error {
	rig, t := r.rig, r.team
	missing, err := rig.MissingEvents(ctx, t, b.seed.Events)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		r.failf(restore.ClauseHC5, "the restored team lacks events %s", strings.Join(missing, ", "))
	}
	checks := []struct {
		c      Credential
		accept bool
		clause restore.Clause
		what   string
	}{
		{b.seed.Kept, true, restore.ClauseHC5, "a credential from backup N is refused"},
		{b.seed.Reissued, false, restore.ClauseRevocations, "the credential reissued after backup N is accepted"},
		{b.seed.Human, false, restore.ClauseRevocations, "the password reset after backup N is accepted"},
	}
	for _, k := range checks {
		ok, aerr := rig.Accepts(ctx, t, k.c)
		if aerr != nil {
			return aerr
		}
		if ok != k.accept {
			r.failf(k.clause, "%s: %s", k.what, k.c.Handle)
		}
	}
	floors, err := rig.Floors(ctx, t)
	if err != nil {
		return err
	}
	for _, h := range slices.Sorted(maps.Keys(b.floors)) {
		if floors[h] != b.floors[h] {
			r.failf(restore.ClauseFloors, "the floor of %s is %q, not the backup's %q", h, floors[h], b.floors[h])
		}
	}
	epoch, err := rig.LiveEpoch(ctx, t)
	if err != nil {
		return err
	}
	if epoch == b.epoch {
		r.failf(restore.ClauseEpoch, "the roster event's restore_epoch did not change (%s)", epoch)
	}
	return nil
}
