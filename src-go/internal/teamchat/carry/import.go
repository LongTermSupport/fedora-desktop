package carry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// The age limits of an export at its import (SI7, O1), and how far a seal's time may run ahead
// of the guest's clock. Constants of this repository, never flags or settings (sc-D9).
const (
	MaxAge      = 72 * time.Hour
	MaxAgeDrill = 24 * time.Hour
	maxAhead    = 5 * time.Minute
)

// ImportFlags are `team import`'s flags past --team: the whole-archive digest the export
// answered (SI1), the drill kind (SI4), and the way on from an import record (SI10).
type ImportFlags struct {
	SHA256          string
	Drill           bool
	Resume, Discard bool
}

// The members SI6 reads before anything is placed, each with its own reader's cap (SX4).
const (
	memberTeam       = "admin/team.json"
	memberServerName = "admin/server-name"
	memberUnits      = "admin/units.json"
	memberEpoch      = "admin/restore-epoch.json"
	memberCACert     = "admin/tls/ca.crt"
	memberCAKey      = "admin/tls/ca.key"
	memberRoster     = "ctl/roster.json"
	memberToken      = "ctl/" + AdminTokenName
	memberApproved   = "status/approved.json"
)

// heldCaps are the members SI6 holds, each with its cap; restore-epoch.json alone may be absent.
var heldCaps = map[string]int64{
	memberTeam: adminCap, memberServerName: adminCap, memberUnits: adminCap, memberEpoch: adminCap,
	memberCACert: adminCap, memberCAKey: adminCap, memberRoster: ctlCap, memberToken: tokenCap,
	memberApproved: approvedCap,
}

// errJudged ends the validation's read at the homeserver tree: SI6 needs nothing in it.
var errJudged = errors.New("carry: the members SI6 reads are read")

// importing is one `team import` (SI1-SI13).
type importing struct {
	d     Deps
	team  paths.TeamID
	flags ImportFlags
	max   int64
	m     Manifest
	held  map[string][]byte
}

func newImporting(d Deps, team paths.TeamID, flags ImportFlags, maxArchive int64) *importing {
	return &importing{d: d, team: team, flags: flags, max: maxArchive, held: map[string][]byte{}}
}

// inbox is the one archive the import reads (SI1, sc-D6).
func (im *importing) inbox() string {
	if im.flags.Drill {
		return paths.CarryDrillExport(im.team)
	}
	return paths.CarryExport(im.team)
}

// validate is every refusal before anything is written (SI1-SI7, SI13): the archive opened by
// the trust rule; its manifest's format, then the manifest strictly (SI13, SI5); the guest
// (SI2, SI3), the kind (SI4) and the age (SI7); then the members SI6 reads, read up to the
// homeserver tree and judged against the seal. It writes nothing.
func (im *importing) validate(ctx context.Context) error {
	f, err := im.d.Files.OpenOwned(im.inbox())
	if err != nil {
		return fmt.Errorf("the archive to import: %w", err)
	}
	_, err = ReadArchive(f, im.team, im.max, func(m Manifest) error {
		im.m = m
		return im.judge(ctx)
	}, im.hold)
	if errors.Is(err, errJudged) {
		err = nil
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	return im.agree()
}

// judge is SI2, SI3, SI4 and SI7, on the manifest read.
func (im *importing) judge(ctx context.Context) error {
	for _, check := range []func(context.Context) error{im.kind, im.age, im.empty, im.serves} {
		if err := check(ctx); err != nil {
			return err
		}
	}
	return nil
}

// hold keeps each member SI6 reads, refusing one over its cap; the homeserver tree ends the read.
func (im *importing) hold(mb Member, body io.Reader) error {
	if mb.Name == "hs" || strings.HasPrefix(mb.Name, "hs/") {
		return errJudged
	}
	limit, held := heldCaps[mb.Name]
	if !held {
		return nil
	}
	if mb.Size > limit {
		return refusedf("%s is %d bytes, over its cap of %d bytes", mb.Name, mb.Size, limit)
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	im.held[mb.Name] = raw
	return nil
}

// kind is SI4: a sealed archive without --drill, a drill archive with it; a drill lands only on
// a guest with no stamped team and no admin state of a team that is not a drill import.
func (im *importing) kind(ctx context.Context) error {
	switch {
	case im.m.Kind == KindDrill && !im.flags.Drill:
		return refusedf("the archive is a drill export: import it with --drill, onto a scratch guest")
	case im.m.Kind == KindSealed && im.flags.Drill:
		return refusedf("the archive is a sealed export: --drill imports only a drill export")
	case !im.flags.Drill:
		return nil
	}
	stamped, err := health.Stamps{Root: im.d.Root}.Stamped(ctx)
	if err != nil {
		return err
	}
	if len(stamped) > 0 {
		return refusedf("team %s is applied on this guest: a drill import lands only on a scratch guest", stamped[0].ID)
	}
	entries, err := im.d.Files.List(paths.AdminStateRoot)
	if err != nil {
		return err
	}
	for _, e := range entries {
		other, perr := paths.ParseTeamID(e.Name)
		if perr != nil || other == im.team {
			continue
		}
		if err := im.drillImport(other); err != nil {
			return err
		}
	}
	return nil
}

// drillImport refuses other's admin state unless its carried.json says a drill import.
func (im *importing) drillImport(other paths.TeamID) error {
	raw, found, err := state.ReadOwnedFile(im.d.Root+paths.CarriedRecord(other), maxRecord, im.d.StateOwnerUID)
	if err != nil {
		return err
	}
	if found {
		rec, derr := DecodeCarriedRecord(raw, other)
		if derr != nil {
			return derr
		}
		if rec.Kind == KindDrill {
			return nil
		}
	}
	return refusedf("%s holds team %s, which is no drill import: a drill import lands only on a scratch guest",
		paths.AdminState(other), other)
}

// age is SI7: the seal's time no more than MaxAge (a drill's MaxAgeDrill) before the guest's
// clock, and no more than maxAhead after it.
func (im *importing) age(context.Context) error {
	limit := MaxAge
	if im.m.Kind == KindDrill {
		limit = MaxAgeDrill
	}
	age := im.d.Clock.Now().Sub(time.UnixMilli(im.m.Seal.TimeMS))
	switch {
	case age < -maxAhead:
		return refusedf("the export's %s is %s ahead of this guest's clock, over the %s allowed", timeKey(im.m.Kind), -age, maxAhead)
	case age > limit:
		return refusedf("the export is %s old, over the limit of %s: export the team again, or roll the driver back with team import --team %s --discard",
			age, limit, im.team)
	}
	return nil
}

// empty is SI2: no root of the team's state on this guest, a link counting, and none of its
// units active. A unit of an installed template is loaded though never started, so a unit is
// judged by its active state: inactive, or not loaded.
func (im *importing) empty(ctx context.Context) error {
	var present []string
	for _, root := range paths.TeamStateRoots(im.team) {
		info, err := im.d.Files.Stat(root)
		if err != nil {
			return err
		}
		if info.Kind != restore.KindAbsent {
			present = append(present, root)
		}
	}
	for _, unit := range []string{paths.ServerUnit(im.team), paths.TLSUnit(im.team), paths.RestoreUnit(im.team)} {
		st, err := im.d.Units.State(ctx, unit)
		if err != nil {
			return err
		}
		if !st.NotLoaded() && st.Active != "inactive" {
			present = append(present, unit+" ("+st.Active+")")
		}
	}
	if len(present) > 0 {
		return refusedf("team %s has state on this guest (%s); the import runs only onto a guest where %s was never applied",
			im.team, strings.Join(present, ", "), im.team)
	}
	return nil
}

// serves is SI3: the base units installed, the manifest's Tuwunel installed as root's regular
// file, and the manifest's port recorded by no other team.
func (im *importing) serves(context.Context) error {
	for _, file := range []string{paths.ServerUnitTemplateFile, paths.TLSUnitTemplateFile, paths.ServerBinary(im.m.Seal.Tuwunel)} {
		info, err := im.d.Files.Stat(file)
		if err != nil {
			return err
		}
		if info.Kind != restore.KindFile || info.UID != im.d.StateOwnerUID {
			return refusedf("%s is not installed as root's regular file: run the teamchat play on this guest first", file)
		}
	}
	other, taken, err := host.PortRecordedBy(im.d.Root, im.team, im.m.Seal.Port.Int())
	if err != nil {
		return err
	}
	if taken {
		return refusedf("port %d is recorded by team %s on this guest", im.m.Seal.Port.Int(), other)
	}
	return nil
}

// disagrees is SI6's refusal naming the member.
func disagrees(member string, err error) error {
	if err != nil {
		return refusedf("the seal record disagrees with %s: %v", member, err)
	}
	return refusedf("the seal record disagrees with %s", member)
}

// agree is SI6: each member read agrees with the seal the manifest records.
func (im *importing) agree() error {
	for name := range heldCaps {
		if _, ok := im.held[name]; !ok && name != memberEpoch {
			return refusedf("the archive holds no %s", name)
		}
	}
	for _, check := range []func() error{im.agreeRoster, im.agreeEpoch, im.agreeTeam, im.agreeUnits, im.agreeCA, im.agreeRest} {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

func (im *importing) agreeRoster() error {
	r, err := apply.DecodeRosterFile(im.held[memberRoster], im.team)
	switch {
	case err != nil:
		return disagrees(memberRoster, err)
	case r.Pending != nil:
		return disagrees(memberRoster, fmt.Errorf("it holds a pending %s", r.Pending.Op))
	case (RosterFacts{Seq: r.Seq, Rev: r.Rev, RestoreEpoch: r.RestoreEpoch}) != im.m.Seal.Roster:
		return disagrees(memberRoster, nil)
	}
	return nil
}

func (im *importing) agreeEpoch() error {
	var floor restore.EpochFloor
	if raw, ok := im.held[memberEpoch]; ok {
		var err error
		if floor, err = restore.DecodeEpochFloor(raw); err != nil {
			return disagrees(memberEpoch, err)
		}
	}
	if (EpochFloor{Epoch: floor.Epoch, MinBackup: floor.MinBackup}) != im.m.Seal.EpochFloor {
		return disagrees(memberEpoch, nil)
	}
	return nil
}

func (im *importing) agreeTeam() error {
	t, err := declare.DecodeTeamFile(im.held[memberTeam])
	switch {
	case err != nil:
		return disagrees(memberTeam, err)
	case t.ID != im.team || t.ServerName != im.m.ServerName || t.ServerName != im.m.Seal.ServerName:
		return disagrees(memberTeam, nil)
	case string(im.held[memberServerName]) != im.m.ServerName:
		return disagrees(memberServerName, nil)
	}
	return nil
}

func (im *importing) agreeUnits() error {
	u, err := host.DecodeUnits(im.held[memberUnits], im.team)
	switch {
	case err != nil:
		return disagrees(memberUnits, err)
	case u.Version != im.m.Seal.Tuwunel || u.Port != im.m.Seal.Port.Int():
		return disagrees(memberUnits, nil)
	}
	return nil
}

// agreeCA judges the CA pair alone, as a restore does onto a team with no CA (SI2 holds that).
func (im *importing) agreeCA() error {
	files := []host.BackupFile{
		{Name: filepath.Base(paths.CACertFile(im.team)), Mode: im.mode(memberCACert), Data: im.held[memberCACert]},
		{Name: filepath.Base(paths.CAKeyFile(im.team)), Mode: im.mode(memberCAKey), Data: im.held[memberCAKey]},
	}
	if err := host.CheckRestoreCA(im.d.Root, im.team, im.d.StateOwnerUID, files); err != nil {
		return disagrees("admin/tls", err)
	}
	return nil
}

// mode is a member's mode as the manifest lists it.
func (im *importing) mode(name string) fs.FileMode {
	for _, mb := range im.m.Members {
		if mb.Name == name {
			return mb.Mode
		}
	}
	return 0
}

func (im *importing) agreeRest() error {
	if _, err := declare.DecodeApproved(im.held[memberApproved], im.team); err != nil {
		return disagrees(memberApproved, err)
	}
	if !isToken(im.held[memberToken]) {
		return disagrees(memberToken, errors.New("it is not a token of OP2's grammar"))
	}
	return nil
}

// isToken is ctl's grammar of the admin token (OP2): 1 to tokenCap bytes of printable ASCII
// without a space. carry cannot import ctl, which imports admin, which imports carry.
func isToken(data []byte) bool {
	if len(data) == 0 || len(data) > tokenCap {
		return false
	}
	for _, c := range data {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}
