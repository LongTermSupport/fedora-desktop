package carry

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// The caps of the files the seal reads, as restore reads the same files.
const (
	rosterMax = 1 << 20
	fileMax   = 64 << 10
)

// Deps is every effect of the carry verbs (SPEC-carry section 13). Root prefixes every host
// path ("" on a host) and LockDir is the team locks' directory; CtlUID owns ctl/<team>/ and
// StateOwnerUID root's own files. Units are the unit states RV2 reads, Systemd stops and disables
// (SC4), Ready probes server_version (SC1) and Status takes a team off the host's books (SC6).
// Every file root writes or removes goes through FS, so each one is a kill point of SM5, as is
// every call of the other seams. Files are the export's sources and archive (SX1-SX7), and
// MaxArchive reads the archive cap's setting (CN5), whose failure is the verb's 78.
type Deps struct {
	Root, LockDir         string
	CtlUID, StateOwnerUID uint32
	Clock                 teamlock.Clock
	Software              paths.Digest
	Units                 restore.Units
	Systemd               host.Systemd
	Ready                 restore.Readiness
	Child                 Child
	FS                    restore.FS
	Status                host.Status
	Files                 Files
	MaxArchive            func() (int64, error)
}

// Seal is `team seal --team T` (SC1-SC7) under the team lock, waited for host.PlayWait (SL1),
// with SG4's guard: a restore, a drill or an import refuses it, an earlier seal does not. A
// team sealed to step sealed answers its record again and changes nothing; one sealed to step
// sealing (a seal killed after SC3) is finished from SC4 on. The answer is sealed.json's
// document with the lines this run changed. Nothing is cleaned up in a defer (SM5): the lock's
// release is the one deferred call, as a kill drops the flock.
func Seal(ctx context.Context, d Deps, team paths.TeamID) ([]byte, error) {
	lock, err := teamlock.Take(ctx, d.LockDir, team, "team.seal", host.PlayWait, d.Clock, RemoveGuard(d.Root))
	if err != nil {
		return nil, err
	}
	s := &sealing{d: d, team: team}
	var out []byte
	func() {
		defer func() { err = errors.Join(err, lock.Release()) }()
		out, err = s.run(ctx)
	}()
	return out, err
}

type sealing struct {
	d       Deps
	team    paths.TeamID
	changed []string
}

func (s *sealing) note(line string) { s.changed = append(s.changed, line) }

func (s *sealing) run(ctx context.Context) ([]byte, error) {
	rec, found, err := s.read()
	switch {
	case err != nil:
		return nil, err
	case found && rec.Step == StepSealed:
		return s.answer(rec), nil
	case !found:
		tail, verr := s.validate(ctx)
		if verr != nil {
			return nil, verr
		}
		rec = SealRecord{Team: s.team, Step: StepSealing, StartedMS: s.d.Clock.Now().UnixMilli(), Tail: tail}
		if err = s.record(rec); err != nil {
			return nil, err
		}
	}
	if err = s.stop(ctx); err != nil {
		return nil, err
	}
	facts, err := s.facts(rec.Tail)
	if err != nil {
		return nil, err
	}
	if err = s.offTheBooks(ctx); err != nil {
		return nil, err
	}
	sealed := SealRecord{Team: s.team, Step: StepSealed, Seal: facts}
	if err = s.record(sealed); err != nil {
		return nil, err
	}
	return s.answer(sealed), nil
}

// read is sealed.json at either step, by the trust rule as root's own file.
func (s *sealing) read() (SealRecord, bool, error) {
	raw, found, err := state.ReadOwnedFile(s.d.Root+paths.SealRecord(s.team), maxRecord, s.d.StateOwnerUID)
	if err != nil || !found {
		return SealRecord{}, false, err
	}
	rec, err := DecodeSealRecord(raw, s.team)
	return rec, err == nil, err
}

// record writes sealed.json at its step (SC3, SC7): 0600, fsynced, through FS.
func (s *sealing) record(r SealRecord) error {
	if err := s.d.FS.WriteFile(paths.SealRecord(s.team), EncodeSealRecord(r)); err != nil {
		return fmt.Errorf("cannot write %s: %w", paths.SealRecord(s.team), err)
	}
	s.note("seal recorded at step " + string(r.Step))
	return nil
}

// answer is the record's document with the lines this run changed (SC7).
func (s *sealing) answer(r SealRecord) []byte {
	changed := make([]proto.Value, 0, len(s.changed))
	for _, line := range s.changed {
		changed = append(changed, proto.Str(line))
	}
	return proto.Obj(append(sealMembers(r), proto.M("changed", proto.Arr(changed...)))...).Compact()
}

// restoreDeps are the restore reads SC1 shares with RV2 and RV3.
func (s *sealing) restoreDeps() restore.Deps {
	return restore.Deps{Root: s.d.Root, CtlUID: s.d.CtlUID, StateOwnerUID: s.d.StateOwnerUID, Units: s.d.Units, FS: s.d.FS}
}

// validate is SC1 and SC2, writing nothing: the team applied and of v2 origin, no restore
// leftover, the roster with nothing pending, the homeserver live, and its tail read and agreeing
// with roster.json.
func (s *sealing) validate(ctx context.Context) ([]TailEntry, error) {
	rd := s.restoreDeps()
	applied, err := restore.ReadApplied(rd, s.team)
	if err != nil {
		return nil, err
	}
	origin, err := health.Stamps{Root: s.d.Root}.Origin(ctx, s.team)
	if err != nil {
		return nil, err
	}
	if origin == status.OriginMigration {
		return nil, refusedf("team %s is of migration origin (%s): its carry needs the migrate verbs, which are not built", s.team, paths.MigrationDir(s.team)+"/state.json")
	}
	if err = restore.Leftovers(ctx, rd, s.team); err != nil {
		return nil, err
	}
	roster, err := s.roster()
	if err != nil {
		return nil, err
	}
	if p := roster.Pending; p != nil {
		return nil, pendingRefusal(s.team, p)
	}
	if err = s.live(ctx); err != nil {
		return nil, err
	}
	tail, err := s.d.Child.Tail(ctx, s.team, applied.TeamFile)
	if err != nil {
		return nil, fmt.Errorf("the tail of team %s: %w", s.team, err)
	}
	if err = tail.Agrees(RosterFacts{Seq: roster.Seq, Rev: roster.Rev, RestoreEpoch: roster.RestoreEpoch}); err != nil {
		return nil, err
	}
	return tail.Rooms, nil
}

// pendingRefusal is RV5's wording: the pending operation and the verb that completes it.
func pendingRefusal(team paths.TeamID, p *apply.Pending) error {
	verb := "team apply --team " + team.String()
	if word, member := strings.CutPrefix(p.Op, "member."); member {
		verb = "member " + word + " --team " + team.String() + " --handle " + p.Handle
	}
	return refusedf("team %s's roster holds a pending %s: run %s first", team, p.Op, verb)
}

// live is SC1's last rule: the homeserver active and answering server_version, since the tail
// is read on it; otherwise the only way out is team remove.
func (s *sealing) live(ctx context.Context) error {
	unit := paths.ServerUnit(s.team)
	st, err := s.d.Units.State(ctx, unit)
	if err != nil {
		return err
	}
	if st.Active != systemd.ActiveStateActive {
		return refusedf("the homeserver of team %s is %s, so its tail cannot be read: start it with team apply, or take the team down with team remove --team %s", s.team, st.Active, s.team)
	}
	u, err := s.units()
	if err != nil {
		return err
	}
	if err = s.d.Ready.ServerVersion(ctx, u.Port); err != nil {
		return refusedf("the homeserver of team %s does not answer (%v), so its tail cannot be read: the only other way out is team remove --team %s", s.team, err, s.team)
	}
	return nil
}

// stop is SC4: the terminator then the homeserver stopped and disabled, each then stopped by
// RS18, and the start block written.
func (s *sealing) stop(ctx context.Context) error {
	lines, err := host.StopTeamUnits(ctx, s.d.Systemd, s.team, true)
	s.changed = append(s.changed, lines...)
	if err != nil {
		return fmt.Errorf("the stop of team %s: %w", s.team, err)
	}
	for _, unit := range []string{paths.TLSUnit(s.team), paths.ServerUnit(s.team)} {
		st, stateErr := s.d.Systemd.State(ctx, unit)
		if stateErr != nil {
			return stateErr
		}
		if st.Active != "inactive" && st.Active != "failed" {
			return fmt.Errorf("%s is %s after its stop, not stopped: run team seal --team %s again once it is", unit, st.Active, s.team)
		}
	}
	block := paths.RestoreBlock(s.team)
	k, err := s.d.FS.RootKind(block)
	if err != nil || k == restore.KindFile {
		return err
	}
	if err = s.d.FS.WriteFile(block, nil); err != nil {
		return fmt.Errorf("cannot write %s: %w", block, err)
	}
	s.note("restore.block written: no start")
	return nil
}

// facts are SC5's, read after the stop.
func (s *sealing) facts(tail []TailEntry) (SealFacts, error) {
	roster, err := s.roster()
	if err != nil {
		return SealFacts{}, err
	}
	floor, err := s.floor()
	if err != nil {
		return SealFacts{}, err
	}
	u, err := s.units()
	if err != nil {
		return SealFacts{}, err
	}
	port, err := paths.ParsePort(u.Port)
	if err != nil {
		return SealFacts{}, fmt.Errorf("%s: %w", paths.UnitsRecord(s.team), err)
	}
	name, err := s.rootFile(paths.ServerNameFile(s.team), fileMax)
	if err != nil {
		return SealFacts{}, err
	}
	return SealFacts{
		TimeMS: s.d.Clock.Now().UnixMilli(), ServerName: string(name), Port: port, Tuwunel: u.Version, Software: s.d.Software,
		Roster: RosterFacts{Seq: roster.Seq, Rev: roster.Rev, RestoreEpoch: roster.RestoreEpoch}, EpochFloor: floor, Tail: tail,
	}, nil
}

// offTheBooks is SC6: the stamp removed, then the status record.
func (s *sealing) offTheBooks(ctx context.Context) error {
	stamp := paths.StampFile(s.team)
	k, err := s.d.FS.RootKind(stamp)
	if err != nil {
		return err
	}
	if k != restore.KindAbsent {
		if err = s.d.FS.Remove(stamp); err != nil {
			return fmt.Errorf("cannot remove %s: %w", stamp, err)
		}
		s.note("stamp removed")
	}
	line, err := s.d.Status.Remove(ctx, s.team)
	if line != "" {
		s.note(line)
	}
	return err
}

// roster is roster.json, the teamchat-ctl account's file (I-B6).
func (s *sealing) roster() (apply.RosterFile, error) {
	path := paths.RosterFile(s.team)
	raw, found, err := state.ReadOwnedFile(s.d.Root+path, rosterMax, s.d.CtlUID)
	switch {
	case err != nil:
		return apply.RosterFile{}, err
	case !found:
		return apply.RosterFile{}, fmt.Errorf("%s is missing", path)
	}
	return apply.DecodeRosterFile(raw, s.team)
}

// floor is restore-epoch.json, {0, 0} when absent (RM10).
func (s *sealing) floor() (EpochFloor, error) {
	raw, found, err := state.ReadOwnedFile(s.d.Root+paths.RestoreEpochFile(s.team), fileMax, s.d.StateOwnerUID)
	if err != nil || !found {
		return EpochFloor{}, err
	}
	f, err := restore.DecodeEpochFloor(raw)
	return EpochFloor{Epoch: f.Epoch, MinBackup: f.MinBackup}, err
}

// units is units.json's facts.
func (s *sealing) units() (host.Units, error) {
	raw, err := s.rootFile(paths.UnitsRecord(s.team), fileMax)
	if err != nil {
		return host.Units{}, err
	}
	return host.DecodeUnits(raw, s.team)
}

// rootFile is one of root's own files by the trust rule; a missing one is an error naming it.
func (s *sealing) rootFile(path string, limit int64) ([]byte, error) {
	raw, found, err := state.ReadOwnedFile(s.d.Root+path, limit, s.d.StateOwnerUID)
	if err == nil && !found {
		err = fmt.Errorf("%s is missing", path)
	}
	return raw, err
}
