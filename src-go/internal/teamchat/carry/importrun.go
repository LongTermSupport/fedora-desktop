package carry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// carriedName is carried.json's name in the admin state, staged before its rename (SI9 step 4).
const carriedName = "carried.json"

// Import is `team import --team T --sha256 <hex> [--drill] [--resume|--discard]` (SI1-SI13):
// the archive cap read first (CN5), then the team lock taken without waiting, since nothing
// else may hold a team that has no state (SL1), with SG5's guard: the import record absent on a
// fresh import and present on --resume or --discard. A fresh import validates (SI1-SI7, SI13),
// stages (SI8) and places (SI9); --resume carries on from the record's step and --discard rolls
// back to an empty guest for T (SI10). The answer is SI11's, or the discard's. Nothing is
// cleaned up in a defer but the lock's release (SM5).
func Import(ctx context.Context, d Deps, team paths.TeamID, flags ImportFlags) ([]byte, error) {
	if flags.Resume && flags.Discard {
		return nil, errors.New("--resume and --discard are two ways on: give one")
	}
	maxArchive, err := archiveCap(d)
	if err != nil {
		return nil, err
	}
	lock, err := teamlock.Take(ctx, d.LockDir, team, "team.import", 0, d.Clock, importGuard(d.Root, flags.Resume || flags.Discard))
	if err != nil {
		return nil, err
	}
	im := newImporting(d, team, flags, maxArchive)
	var out []byte
	func() {
		defer func() { err = errors.Join(err, lock.Release()) }()
		out, err = im.run(ctx)
	}()
	return out, err
}

// importGuard is SG5's: a restore, a seal or a drill refuses an import; the import record
// refuses a fresh one, and its absence refuses --resume and --discard.
func importGuard(root string, again bool) teamlock.Guard {
	if !again {
		return teamlock.All(restore.Guard(root), Guard(root))
	}
	others := teamlock.All(restore.Guard(root), refuseRecords(root, RecordSeal, RecordDrill))
	return func(team paths.TeamID) error {
		if err := others(team); err != nil {
			return err
		}
		found, err := Recorded(root, team)
		if err != nil {
			return err
		}
		if !slices.Contains(found, RecordImport) {
			return refusedf("team %s has no import record (%s): --resume and --discard carry on an import; a new one is team import --team %s --sha256 <hex>",
				team, paths.CarryImportRecord(team), team)
		}
		return nil
	}
}

func (im *importing) kindFlag() Kind {
	if im.flags.Drill {
		return KindDrill
	}
	return KindSealed
}

func (im *importing) run(ctx context.Context) ([]byte, error) {
	if !im.flags.Resume && !im.flags.Discard {
		return im.fresh(ctx)
	}
	rec, err := im.readRecord()
	if err != nil {
		return nil, err
	}
	if rec.SHA256 != im.flags.SHA256 || rec.Kind != im.kindFlag() {
		return nil, refusedf("the import record of team %s is of a %s archive of sha256 %s: --sha256 and --drill must be the record's",
			im.team, rec.Kind, rec.SHA256)
	}
	if im.flags.Discard {
		return im.discard(rec)
	}
	switch rec.Step {
	case ImportStaging:
		if err = im.removeStaging(); err != nil {
			return nil, err
		}
		return im.fresh(ctx)
	case ImportStaged:
		if err = im.checkStaged(); err != nil {
			return nil, err
		}
	case ImportPlacing:
	}
	return im.place(ctx, rec)
}

// readRecord is the import record, by the trust rule as root's own file.
func (im *importing) readRecord() (ImportRecord, error) {
	path := paths.CarryImportRecord(im.team)
	raw, found, err := state.ReadOwnedFile(im.d.Root+path, maxRecord, im.d.StateOwnerUID)
	switch {
	case err != nil:
		return ImportRecord{}, err
	case !found:
		return ImportRecord{}, refusedf("team %s has no import record (%s)", im.team, path)
	}
	return DecodeImportRecord(raw, im.team)
}

// record writes the import record at its step (SI8, SI9): 0600, fsynced, through FS.
func (im *importing) record(r ImportRecord) error {
	if err := im.d.FS.WriteFile(paths.CarryImportRecord(im.team), EncodeImportRecord(r)); err != nil {
		return fmt.Errorf("cannot write %s: %w", paths.CarryImportRecord(im.team), err)
	}
	return nil
}

// fresh is a new import: validation, the record at staging, the staging and its whole digest
// against --sha256 (a failure removes the staging and the record: import not done), the record
// at staged, then placement.
func (im *importing) fresh(ctx context.Context) ([]byte, error) {
	if err := im.validate(ctx); err != nil {
		return nil, err
	}
	rec := ImportRecord{Team: im.team, Kind: im.m.Kind, SHA256: im.flags.SHA256, Step: ImportStaging}
	if err := im.record(rec); err != nil {
		return nil, err
	}
	read, err := im.stage()
	if err == nil && read.SHA256 != im.flags.SHA256 {
		err = fmt.Errorf("the archive's sha256 is %s, not --sha256's %s", read.SHA256, im.flags.SHA256)
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("import not done: %w", err), im.removeStaging())
	}
	rec.Step = ImportStaged
	if err = im.record(rec); err != nil {
		return nil, err
	}
	return im.place(ctx, rec)
}

// stagedPath is where a member is staged: hs/ in HomeserverCarryStaging, the rest in
// CarryStaging.
func (im *importing) stagedPath(name string) string {
	if rest, found := strings.CutPrefix(name, "hs"); found && (rest == "" || strings.HasPrefix(rest, "/")) {
		return paths.HomeserverCarryStaging(im.team) + rest
	}
	return paths.CarryStaging(im.team) + "/" + name
}

// stage is SI8: the homeserver's root made, CarryStaging made with the manifest's copy, then the
// archive streamed again, each member written as the parser checks it, each file fsynced, each
// directory fsynced after its entries and the parents last. It answers the whole digest.
func (im *importing) stage() (Read, error) {
	s, staging := im.d.Stage, paths.CarryStaging(im.team)
	if err := s.MakeHomeserverRoot(); err != nil {
		return Read{}, err
	}
	if err := s.Mkdir(staging); err != nil {
		return Read{}, err
	}
	if err := im.write(staging+"/"+manifestName, 0o600, bytes.NewReader(EncodeManifest(im.m))); err != nil {
		return Read{}, err
	}
	f, err := im.d.Files.OpenOwned(im.inbox())
	if err != nil {
		return Read{}, fmt.Errorf("the archive to import: %w", err)
	}
	read, err := ReadArchive(f, im.team, im.max, im.sameManifest, im.stageMember)
	if err = errors.Join(err, f.Close()); err != nil {
		return Read{}, err
	}
	dirs := slices.Clone(im.dirs)
	slices.Reverse(dirs)
	for _, dir := range append(dirs, staging, paths.CarryDir, paths.HomeserverPrivateStateRoot) {
		if err = s.SyncDir(dir); err != nil {
			return Read{}, err
		}
	}
	return read, nil
}

// sameManifest refuses an archive whose manifest is not the one validated.
func (im *importing) sameManifest(m Manifest) error {
	if !bytes.Equal(EncodeManifest(m), EncodeManifest(im.m)) {
		return refusedf("the archive in %s changed since it was validated", im.inbox())
	}
	return nil
}

// stageMember writes one member at its staged path.
func (im *importing) stageMember(mb Member, body io.Reader) error {
	path := im.stagedPath(mb.Name)
	if mb.Dir {
		im.dirs = append(im.dirs, path)
		return im.d.Stage.Mkdir(path)
	}
	return im.write(path, mb.Mode, body)
}

// write makes a new file of mode holding body, fsynced.
func (im *importing) write(path string, mode fs.FileMode, body io.Reader) error {
	part, err := im.d.Stage.Create(path, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(part, body)
	if err == nil {
		err = part.Sync()
	}
	if err = errors.Join(err, part.Close()); err != nil {
		return fmt.Errorf("cannot stage %s: %w", path, err)
	}
	return nil
}

// manifest is the manifest validation read, or on a resume the staged copy (SI10).
func (im *importing) manifest() (Manifest, error) {
	if im.read {
		return im.m, nil
	}
	path := paths.CarryStaging(im.team) + "/" + manifestName
	raw, err := im.readFile(path, maxManifest)
	if err != nil {
		return Manifest{}, err
	}
	m, err := DecodeManifest(raw, im.team)
	if err != nil {
		return Manifest{}, fmt.Errorf("the staged %s: %w", path, err)
	}
	im.m, im.read = m, true
	return m, nil
}

// readFile reads a file root staged, with no link followed, of at most limit bytes.
func (im *importing) readFile(path string, limit int64) ([]byte, error) {
	rc, info, err := im.d.Files.Open(path)
	if err != nil {
		return nil, err
	}
	if info.Size > limit {
		return nil, errors.Join(fmt.Errorf("%s is %d bytes, over its cap of %d bytes", path, info.Size, limit), rc.Close())
	}
	raw, err := io.ReadAll(io.LimitReader(rc, limit))
	if err = errors.Join(err, rc.Close()); err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	return raw, nil
}

// staged reads a staged file member, which must be the manifest's size and digest.
func (im *importing) staged(mb Member) ([]byte, error) {
	path := im.stagedPath(mb.Name)
	rc, info, err := im.d.Files.Open(path)
	if err != nil {
		return nil, err
	}
	sum := sha256.New()
	var buf bytes.Buffer
	n, err := io.Copy(io.MultiWriter(sum, &buf), io.LimitReader(rc, mb.Size+1))
	if err = errors.Join(err, rc.Close()); err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	if info.Mode != mb.Mode || n != mb.Size || hex.EncodeToString(sum.Sum(nil)) != mb.SHA256 {
		return nil, refusedf("the staged %s is not the member the manifest lists: team import --team %s --discard, then import again", mb.Name, im.team)
	}
	return buf.Bytes(), nil
}

// member is the manifest's member of name.
func (im *importing) member(name string) (Member, error) {
	for _, mb := range im.m.Members {
		if mb.Name == name {
			return mb, nil
		}
	}
	return Member{}, refusedf("the manifest lists no %s", name)
}

// stagedFile is staged() by name.
func (im *importing) stagedFile(name string) ([]byte, error) {
	mb, err := im.member(name)
	if err != nil {
		return nil, err
	}
	return im.staged(mb)
}

// checkStaged is SI10's check at staged: every member staged, each directory a directory and
// each file of its manifest's mode, size and digest.
func (im *importing) checkStaged() error {
	if _, err := im.manifest(); err != nil {
		return err
	}
	for _, mb := range im.m.Members {
		if mb.Dir {
			info, err := im.d.Files.Stat(im.stagedPath(mb.Name))
			if err != nil {
				return err
			}
			if info.Kind != restore.KindDirectory {
				return refusedf("the staged %s is not a directory: team import --team %s --discard, then import again", mb.Name, im.team)
			}
			continue
		}
		if _, err := im.staged(mb); err != nil {
			return err
		}
	}
	return nil
}

// removeStaging removes both staging names, then the record, and fsyncs the carry directory.
func (im *importing) removeStaging() error {
	for _, path := range []string{paths.HomeserverCarryStaging(im.team), paths.CarryStaging(im.team)} {
		if err := im.d.Stage.RemoveTree(path); err != nil {
			return err
		}
	}
	if err := im.d.FS.Remove(paths.CarryImportRecord(im.team)); err != nil {
		return err
	}
	return im.d.Stage.SyncDir(paths.CarryDir)
}

// placing is SI9 over the record: each root of PlacedRoots in its order.
type placing struct {
	*importing

	rec     ImportRecord
	carried *CarriedRecord
}

// place is SI9, from the first root not in the record's placed on a resume (SI10); then step 6:
// the staging and the record removed, the carry directory fsynced. It answers SI11's answer.
func (im *importing) place(ctx context.Context, rec ImportRecord) ([]byte, error) {
	p := &placing{importing: im, rec: rec}
	p.rec.Step = ImportPlacing
	t := im.team
	steps := map[string]func(ctx context.Context, again bool) error{
		paths.HomeserverPrivateState(t): p.homeserver, paths.CtlState(t): p.ctl,
		paths.StatusTeamDir(t): p.status, paths.AdminState(t): p.admin,
	}
	for i, root := range PlacedRoots(t) {
		again := slices.Contains(rec.Placed, root)
		last := again && (i+1 == len(PlacedRoots(t)) || !slices.Contains(rec.Placed, PlacedRoots(t)[i+1]))
		if again && !last && root != paths.HomeserverPrivateState(t) && root != paths.AdminState(t) {
			continue
		}
		if !again {
			if err := p.placed(root); err != nil {
				return nil, err
			}
		}
		if err := steps[root](ctx, again); err != nil {
			return nil, err
		}
	}
	return p.finish()
}

// placed appends root to the record's placed, before the step that makes it (SI9).
func (p *placing) placed(root string) error {
	p.rec.Placed = append(p.rec.Placed, root)
	return p.record(p.rec)
}

// renamed is a placed root's state on a resume: done when the target is there and its staging is
// not; to be renamed when only the staging is there; any other mix is refused, naming both.
func (p *placing) renamed(from, to string) (bool, error) {
	f, err := p.d.Files.Stat(from)
	if err != nil {
		return false, err
	}
	g, err := p.d.Files.Stat(to)
	if err != nil {
		return false, err
	}
	switch {
	case g.Kind == restore.KindDirectory && f.Kind == restore.KindAbsent:
		return true, nil
	case g.Kind == restore.KindAbsent && f.Kind == restore.KindDirectory:
		return false, nil
	}
	return false, refusedf("%s is %s and %s is %s: the import cannot tell which is its own; team import --team %s --discard rolls it back",
		from, f.Kind, to, g.Kind, p.team)
}

// rename moves a staged root into place and fsyncs the target's parent; on a resume a root
// already renamed is done.
func (p *placing) rename(from, to, parent string, again bool, before func() error) error {
	if again {
		done, err := p.renamed(from, to)
		if err != nil || done {
			return err
		}
	}
	if before != nil {
		if err := before(); err != nil {
			return err
		}
	}
	if err := p.d.Stage.Rename(from, to); err != nil {
		return err
	}
	return p.d.Stage.SyncDir(parent)
}

// homeserver is step 1: the staged homeserver tree renamed into place, root's until its first
// start (SH1).
func (p *placing) homeserver(_ context.Context, again bool) error {
	return p.rename(paths.HomeserverCarryStaging(p.team), paths.HomeserverPrivateState(p.team), paths.HomeserverPrivateStateRoot, again, nil)
}

// redo removes a root a resume finds last in placed: its step may have stopped half done, and
// making it again gives the same bytes.
func (p *placing) redo(root string, again bool) error {
	if !again {
		return nil
	}
	return p.d.Stage.RemoveTree(root)
}

// ctl is step 2: ctl/<team>/ made as a first apply makes it, then carry.place with the staged
// files and each lost export streamed after the request (SC-C2, sc-D11).
func (p *placing) ctl(ctx context.Context, again bool) error {
	if err := p.redo(paths.CtlState(p.team), again); err != nil {
		return err
	}
	if err := p.d.Stage.MkdirCtl(p.team); err != nil {
		return err
	}
	args, err := p.placeArgs()
	if err != nil {
		return err
	}
	if _, err = p.d.Child.Place(ctx, p.team, args); err != nil {
		return fmt.Errorf("carry.place for team %s: %w", p.team, err)
	}
	return nil
}

// placeArgs are carry.place's from the staged tree: each file checked against the manifest.
func (p *placing) placeArgs() (PlaceArgs, error) {
	if _, err := p.manifest(); err != nil {
		return PlaceArgs{}, err
	}
	var a PlaceArgs
	files := []struct {
		name string
		to   *[]byte
	}{{memberTeam, &a.TeamFile}, {memberRoster, &a.Roster}, {"ctl/rooms.json", &a.Rooms}}
	for _, f := range files {
		raw, err := p.stagedFile(f.name)
		if err != nil {
			return PlaceArgs{}, err
		}
		*f.to = raw
	}
	token, err := p.stagedFile(memberToken)
	if err != nil {
		return PlaceArgs{}, err
	}
	a.Token = string(token)
	for _, mb := range p.m.Members {
		base, dir := mb.Name[strings.LastIndex(mb.Name, "/")+1:], mb.Name[:max(strings.LastIndex(mb.Name, "/"), 0)]
		switch {
		case mb.Dir:
		case dir == "ctl/guest":
			raw, gerr := p.staged(mb)
			if gerr != nil {
				return PlaceArgs{}, gerr
			}
			a.Guest = append(a.Guest, GuestToken{Handle: strings.TrimSuffix(base, ".token"), Token: string(raw)})
		case dir == "ctl" && lostName.MatchString(base):
			a.Lost = append(a.Lost, LostExport{Name: base, Size: mb.Size, SHA256: mb.SHA256})
		}
	}
	if len(a.Lost) > 0 {
		if a.Stream, err = StagedLost(p.d.Files, p.team, a.Lost); err != nil {
			return PlaceArgs{}, err
		}
	}
	return a, nil
}

// status is step 3: the status directory made, and approved.json saved as team approve saves it.
func (p *placing) status(_ context.Context, again bool) error {
	if err := p.redo(paths.StatusTeamDir(p.team), again); err != nil {
		return err
	}
	if _, err := p.manifest(); err != nil {
		return err
	}
	raw, err := p.stagedFile(memberApproved)
	if err != nil {
		return err
	}
	g, err := declare.DecodeApproved(raw, p.team)
	if err != nil {
		return disagrees(memberApproved, err)
	}
	if err = p.d.Stage.MkdirStatus(p.team); err != nil {
		return err
	}
	return p.d.Stage.SaveApproved(p.team, g)
}

// admin is steps 4 and 5: carried.json written into the staged admin directory, the age checked
// again (SI7), then the admin directory renamed into place: the commit point.
func (p *placing) admin(ctx context.Context, again bool) error {
	from := paths.CarryStaging(p.team) + "/admin"
	return p.rename(from, paths.AdminState(p.team), paths.AdminStateRoot, again, func() error {
		m, err := p.manifest()
		if err != nil {
			return err
		}
		c := CarriedRecord{Team: p.team, Kind: m.Kind, SHA256: p.rec.SHA256, SealedMS: m.Seal.TimeMS, ImportedMS: p.d.Clock.Now().UnixMilli(), Roster: m.Seal.Roster}
		if err = p.d.FS.WriteFile(from+"/"+carriedName, EncodeCarriedRecord(c)); err != nil {
			return err
		}
		p.carried = &c
		return p.age(ctx)
	})
}

// finish is step 6 and the answer: the staging removed, then the record, then the carry
// directory fsynced.
func (p *placing) finish() ([]byte, error) {
	if p.carried == nil {
		raw, found, err := state.ReadOwnedFile(p.d.Root+paths.CarriedRecord(p.team), maxRecord, p.d.StateOwnerUID)
		if err == nil && !found {
			err = fmt.Errorf("%s is missing", paths.CarriedRecord(p.team))
		}
		if err != nil {
			return nil, err
		}
		c, err := DecodeCarriedRecord(raw, p.team)
		if err != nil {
			return nil, err
		}
		p.carried = &c
	}
	if err := p.removeStaging(); err != nil {
		return nil, err
	}
	return p.answer(*p.carried), nil
}

// answer is SI11's: the kind, the seal's time (the drill's for a drill), the roster's facts, the
// roots placed and the next verbs.
func (im *importing) answer(c CarriedRecord) []byte {
	next := []proto.Value{proto.Str("team carry-check --team " + im.team.String())}
	if c.Kind == KindSealed {
		next = append(next, proto.Str("team apply"))
	}
	placed := make([]proto.Value, 0, len(PlacedRoots(im.team)))
	for _, root := range PlacedRoots(im.team) {
		placed = append(placed, proto.Str(root))
	}
	return proto.Obj(append(head(im.team), proto.M(keyKind, proto.Str(string(c.Kind))), proto.M(timeKey(c.Kind), proto.Int(c.SealedMS)),
		proto.M("roster", rosterValue(c.Roster)), proto.M("placed", proto.Arr(placed...)), proto.M("next", proto.Arr(next...)))...).Compact()
}

// discard is SI10's --discard: at placing, each root in placed that is there must be a
// directory, never a link, and is removed; then both staging names and the record. The guest is
// empty for T again. The answer names what was removed.
func (im *importing) discard(rec ImportRecord) ([]byte, error) {
	var removed []proto.Value
	var roots []string
	if rec.Step == ImportPlacing {
		for _, root := range rec.Placed {
			info, err := im.d.Files.Stat(root)
			switch {
			case err != nil:
				return nil, err
			case info.Kind == restore.KindAbsent:
				continue
			case info.Kind != restore.KindDirectory:
				return nil, refusedf("%s is not a directory: --discard removes only a root the import placed, never through a link; remove it by hand", root)
			}
			roots = append(roots, root)
		}
	}
	for _, path := range []string{paths.HomeserverCarryStaging(im.team), paths.CarryStaging(im.team)} {
		info, err := im.d.Files.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.Kind != restore.KindAbsent {
			roots = append(roots, path)
		}
	}
	for _, root := range roots {
		if err := im.d.Stage.RemoveTree(root); err != nil {
			return nil, err
		}
		removed = append(removed, proto.Str(root))
	}
	if err := im.removeStaging(); err != nil {
		return nil, err
	}
	return proto.Obj(append(head(im.team), proto.M("discarded", proto.Arr(removed...)))...).Compact(), nil
}
