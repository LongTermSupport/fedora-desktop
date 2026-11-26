package sessions

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatcheckouts"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatchoice"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/sessionname"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// The seat pass (Plan 00265 R89, design section 3): after the session records, a restore of
// every record walks the checkouts in seat-checkouts and starts each teamchat pool seat whose
// conversation is recorded here, that no running session holds and no session record covers
// (a record of the same checkout whose --teams names the seat). Each is given a record of its
// own, as `ccy-sessions declare` would write it, and started from it as every record is, so the
// next boot restores it as a session. The seat is named in that record's --teams, never only
// its team: the records this restore just started do not hold their seats yet, so a launch by
// team could take one of theirs. What is shown to a person is the seat in words.
//
// Not started, and named with the way on: a seat whose conversation's transcript is gone, and a
// host (cc) seat, which this pass does not start. A seat that never ran is not a session to
// restore. A pool of a checkout seat-checkouts does not list is said, since its path cannot be
// known from its key. None of these is a failure of the restore: none was a session the reboot
// took down.

// seatKinds are the pool seats the pass looks at, by kind.
var seatKinds = []string{seatchoice.KindPodman, seatchoice.KindHost}

// seatPass is the pass over every seat checkout. A seat the pass cannot judge is a failure of
// the restore: a seat it should have started may not have been. The live session set is read
// once, when a seat is to be started and the records' half did not read it.
func (r *restoreRun) seatPass(ctx context.Context, reading registry.Reading) {
	a := r.app
	if err := r.walkSeats(ctx, reading); err != nil {
		a.failErr(err)
		a.fail("the seats with no session record could not all be judged, so some may not have been started.")
		r.failures++
	}
}

func (r *restoreRun) walkSeats(ctx context.Context, reading registry.Reading) error {
	listed, err := seatcheckouts.Of(registry.New(r.regdir)).Read()
	if err != nil || len(listed) == 0 {
		return err
	}
	state, err := paths.StateHome(r.app.Getenv("HOME"), r.app.Getenv("XDG_STATE_HOME"))
	if err != nil {
		return fmt.Errorf("the state home: %w", err)
	}
	store, err := paths.SeatStore(state)
	if err != nil {
		return fmt.Errorf("the seat store: %w", err)
	}
	seen := map[string]bool{}
	for _, dir := range listed {
		resolved, evalErr := filepath.EvalSymlinks(dir)
		if errors.Is(evalErr, fs.ErrNotExist) {
			continue
		}
		if evalErr != nil {
			return fmt.Errorf("the seat checkout %s: %w", dir, evalErr)
		}
		key, keyErr := paths.CheckoutKeyOf(resolved)
		if keyErr != nil {
			return fmt.Errorf("the checkout key of %s: %w", resolved, keyErr)
		}
		seen[key.String()] = true
		if err := r.checkoutSeats(ctx, reading, store, key, dir); err != nil {
			return err
		}
	}
	return r.unlistedPools(store, seen)
}

// checkoutSeats is the pass over one checkout's pool.
func (r *restoreRun) checkoutSeats(ctx context.Context, reading registry.Reading, store paths.Store, key paths.CheckoutKey, dir string) error {
	teams, err := system.PoolTeams(store, key)
	if err != nil {
		return err
	}
	cmd := &system.Cmd{Getenv: r.app.Getenv, Status: r.app.TeamStatus}
	for _, team := range teams {
		id, idErr := paths.ParseTeamID(team)
		if idErr != nil {
			return fmt.Errorf("team %s: %w", team, idErr)
		}
		roles, rolesErr := cmd.SeatRoles(dir, team)
		if rolesErr != nil {
			return rolesErr
		}
		for _, kind := range seatKinds {
			seats, poolErr := system.PoolState(store, key, id, recordedSessions(reading), kind)
			if poolErr != nil {
				return poolErr
			}
			for _, s := range seats {
				s.Role = roles[s.Name]
				if err := r.seat(ctx, reading, dir, team, kind, s); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// seat judges one pool seat, and starts it or names why not.
func (r *restoreRun) seat(ctx context.Context, reading registry.Reading, dir, team, kind string, s seatchoice.Seat) error {
	ref := s.Name + "@" + team
	ran, err := conversation.SeatRan(dir, membership.Seat{Team: team, Seat: s.Name})
	if err != nil || !ran.Recorded || s.Held() || recordCovers(reading, dir, ref) {
		return err
	}
	words := seatchoice.InWords(s, team)
	if kind == seatchoice.KindHost {
		if s.Role == "" {
			words = "a host seat of " + team
		}
		r.notStartedSeat(words, dir, "the restore starts ccy seats only; run cc --teams "+team+" there")
		return nil
	}
	if ran.MS == 0 {
		r.notStartedSeat(words, dir, "its conversation's transcript is gone; run ccy --teams "+team+" there to choose")
		return nil
	}
	resuming := "its conversation"
	if ran.Prompt != "" {
		resuming = `"` + ran.Prompt + `"`
	}
	said := words + " in " + dir + ", resuming " + resuming + " (last ran " + seatchoice.Ago(r.app.Now().UnixMilli(), ran.MS) + ")"
	if r.dryRun {
		r.app.out("would start " + said + "\n")
		return nil
	}
	return r.startSeat(ctx, reading, dir, ref, said)
}

// startSeat writes the seat's own record and restores it as any record is.
func (r *restoreRun) startSeat(ctx context.Context, reading registry.Reading, dir, ref, said string) error {
	if r.live == nil {
		sessions, err := r.app.Tmux.List(ctx)
		if err != nil {
			return fmt.Errorf("the live session list, so no seat is started: %w", err)
		}
		r.live = map[string]bool{}
		for _, s := range sessions {
			r.live[s.Name] = true
		}
	}
	roles := func(checkout, team string) (map[string]string, error) {
		return (&system.Cmd{Getenv: r.app.Getenv, Status: r.app.TeamStatus}).SeatRoles(checkout, team)
	}
	labels, err := sessionname.RoleLabels(dir, ref, roles)
	if err != nil {
		return err
	}
	base, _, err := sessionname.NameBy(prefixCcy, projectName(dir), ref, labels)
	if err != nil {
		return err
	}
	taken := make([]tmuxccy.Session, 0, len(r.live)+len(reading.Records))
	for name := range r.live {
		taken = append(taken, tmuxccy.Session{Name: name})
	}
	for _, rec := range reading.Records {
		taken = append(taken, tmuxccy.Session{Name: rec.Name})
	}
	rec := registry.Record{
		Name: sessionname.Free(base, taken), Dir: dir, Launcher: launcher, Prefix: prefixCcy, Restore: true,
		Args: []string{"--teams", ref},
	}
	store := registry.New(r.regdir)
	if err := store.Write(rec); err != nil {
		return err
	}
	written, file, err := store.Find(rec.Name)
	if err != nil {
		return err
	}
	r.app.sayf("starting %s: it had no session record.\n", said)
	r.record(ctx, written, file)
	return nil
}

// notStartedSeat names a seat the pass does not start, with why and the way on.
func (r *restoreRun) notStartedSeat(words, dir, why string) {
	line := "not start " + words + " in " + dir + ": " + why
	if r.dryRun {
		r.app.out("would " + line + "\n")
		return
	}
	r.app.sayf("did %s.\n", line)
}

// unlistedPools names each pool of a checkout no seat-checkouts line led to.
func (r *restoreRun) unlistedPools(store paths.Store, seen map[string]bool) error {
	entries, err := os.ReadDir(store.Pool())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("the seat pool: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() || seen[e.Name()] {
			continue
		}
		key, keyErr := paths.ParseCheckoutKey(e.Name())
		if keyErr != nil {
			continue
		}
		teams, teamsErr := system.PoolTeams(store, key)
		if teamsErr != nil {
			return teamsErr
		}
		if len(teams) > 0 {
			r.app.sayf("seats of %s are kept for a checkout this machine's seat list does not name, so none of them is started; run ccy --teams <team> in that checkout to start one.\n",
				strings.Join(teams, ", "))
		}
	}
	return nil
}

// recordCovers is whether a session record of dir names ref in its --teams.
func recordCovers(reading registry.Reading, dir, ref string) bool {
	for _, rec := range reading.Records {
		if rec.Dir != dir {
			continue
		}
		seats, err := membership.ParseSeats(strings.Join(strings.Fields(discover.RecordTeams(rec)), ""))
		if err != nil {
			continue
		}
		if slices.ContainsFunc(seats, func(s membership.Seat) bool { return s.Seat+"@"+s.Team == ref }) {
			return true
		}
	}
	return false
}

// recordedSessions is the seat store's test of a holding's session against the registry read
// for this restore: a session whose record is kept holds its seat.
type recordedSessions registry.Reading

func (s recordedSessions) Restorable(n paths.SessionName) (bool, error) {
	return slices.ContainsFunc(s.Records, func(rec registry.Record) bool { return rec.Name == n.String() }), nil
}
