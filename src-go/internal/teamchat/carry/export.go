package carry

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// AdminTokenName is the team's admin token's name in ctl/<team>/ (OP2): carried by the export
// (SX2), placed by carry.place (SC-C2).
const AdminTokenName = "admin.token"

// The caps of SX4: each admin/ and ctl/ file's is its own reader's. noCap is hs/'s: none singly,
// the archive's cap bounds them.
const (
	adminCap    = 1 << 20   // host's fileMax, which reads every admin record, the config and the CA
	backupCap   = 4 << 20   // backup's maxArchive, which reads a <n>.tar back
	ctlCap      = 1 << 20   // BK2's for the roster and the rooms
	tokenCap    = 4 << 10   // BK2's for the admin token, and a guest token's
	lostCap     = 64 << 20  // RS14's for a restore's lost events
	approvedCap = 256 << 10 // declare's maxApproved (AG2)
	noCap       = -1
)

// The names SX2 carries from a listed directory.
var (
	backupTarName = regexp.MustCompile(`^[1-9][0-9]*\.tar$`)
	lostName      = regexp.MustCompile(`^restore-[1-9][0-9]*-lost\.jsonl$`)
	guestName     = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._+-]{0,121}\.token$`)
)

// Info is what a source is: its kind, its permission bits and its size.
type Info struct {
	Kind restore.Kind
	Mode fs.FileMode
	Size int64
}

// Part is the archive being written.
type Part interface {
	io.Writer
	Sync() error
	Close() error
}

// Files is the export's file system under Deps.Root (SX1-SX3, SX7). The sources are read
// without following a link anywhere in their path; the carry directory is root's, 0700.
type Files interface {
	// Stat is what path is, a link being KindOther; an absent one is KindAbsent.
	Stat(path string) (Info, error)
	// List names a directory's entries by name, each with its kind.
	List(dir string) ([]restore.Entry, error)
	// Open opens a regular file for reading, with what fstat says of it.
	Open(path string) (io.ReadCloser, Info, error)
	// MakeCarryDir makes paths.CarryDir 0700 or converges the one there, refusing a link or a
	// directory of another owner.
	MakeCarryDir() error
	// Create makes a new file 0600, never over one that exists.
	Create(path string) (Part, error)
	// Rename moves a file onto another name in its directory, replacing what is there.
	Rename(from, to string) error
	// Remove unlinks a file.
	Remove(path string) error
	// SyncDir fsyncs a directory.
	SyncDir(dir string) error
	// Digest is an archive root wrote, read by the trust rule: its size and digest.
	Digest(path string) (Written, error)
}

// Export is `team export --team T` (SX1-SX7): the sealed team's archive, written to its part,
// fsynced and renamed to CarryExport under the team lock, waited for host.PlayWait (SL1), with
// SG4's guard. The archive cap (CN5) is read first. Every source is digested in a first pass,
// which makes the manifest, and again by the write, which fails if any differs (SX5). A part a
// kill left is removed first; an existing export is replaced only by one of the same digest
// (SX7). The answer is SX6's. Nothing is cleaned up in a defer but the lock's release (SM5).
func Export(ctx context.Context, d Deps, team paths.TeamID) ([]byte, error) {
	maxArchive, err := archiveCap(d)
	if err != nil {
		return nil, err
	}
	lock, err := teamlock.Take(ctx, d.LockDir, team, "team.export", host.PlayWait, d.Clock, SealedGuard(d.Root, d.StateOwnerUID))
	if err != nil {
		return nil, err
	}
	x := newExporting(d, team, maxArchive, KindSealed)
	var out []byte
	func() {
		defer func() { err = errors.Join(err, lock.Release()) }()
		out, err = x.run()
	}()
	return out, err
}

// archiveCap is carry.MaxArchive, read from its setting (CN5): a positive number of bytes.
func archiveCap(d Deps) (int64, error) {
	if d.MaxArchive == nil {
		return 0, fmt.Errorf("this wiring has no %s", paths.CarryMaxArchiveVar)
	}
	maxArchive, err := d.MaxArchive()
	if err != nil {
		return 0, err
	}
	if maxArchive <= 0 {
		return 0, fmt.Errorf("%s is %d: the archive cap must be a positive number of bytes", paths.CarryMaxArchiveVar, maxArchive)
	}
	return maxArchive, nil
}

// exporting is one archive of kind written: a sealed export, or a drill's (SD3).
type exporting struct {
	d       Deps
	team    paths.TeamID
	max     int64
	kind    Kind
	members []Member
	// sources is each file member's source path.
	sources map[string]string
}

func newExporting(d Deps, team paths.TeamID, maxArchive int64, kind Kind) *exporting {
	return &exporting{d: d, team: team, max: maxArchive, kind: kind, sources: map[string]string{}}
}

func (x *exporting) run() ([]byte, error) {
	rec, err := ReadSeal(x.d.Root, x.d.StateOwnerUID, x.team)
	if err != nil {
		return nil, err
	}
	written, err := x.archive(rec.Seal)
	if err != nil {
		return nil, err
	}
	return x.answer(written, rec.Seal.TimeMS), nil
}

// drill is whether the archive is a drill's.
func (x *exporting) drill() bool { return x.kind == KindDrill }

// target is the archive's path: CarryExport, or CarryDrillExport for a drill.
func (x *exporting) target() string {
	if x.drill() {
		return paths.CarryDrillExport(x.team)
	}
	return paths.CarryExport(x.team)
}

// archive is SX2 to SX5 over facts: the survey, the manifest checked against the cap, and the
// write.
func (x *exporting) archive(facts SealFacts) (Written, error) {
	if err := x.survey(); err != nil {
		return Written{}, err
	}
	m := Manifest{Format: ExportFormat, Kind: x.kind, Team: x.team, ServerName: facts.ServerName, Seal: facts, Members: x.members}
	if _, err := fits(m, x.max); err != nil {
		return Written{}, err
	}
	return x.write(m)
}

// answer is SX6's: the path, size and digest of the archive written, and the seal's or the
// drill's time under its key.
func (x *exporting) answer(written Written, timeMS int64) []byte {
	return proto.Obj(append(head(x.team),
		proto.M("kind", proto.Str(string(x.kind))), proto.M("path", proto.Str(x.target())),
		proto.M("size", proto.Int(written.Size)), proto.M("sha256", proto.Str(written.SHA256)),
		proto.M(timeKey(x.kind), proto.Int(timeMS)))...).Compact()
}

// write is SX1 and SX7: the carry directory made, a left part removed, the archive written to a
// new part and fsynced, then renamed over an export of the same digest, or to a new one, and the
// directory fsynced. A drill's archive replaces the last drill's whatever its digest: the team
// was live between them. A failed write removes its part.
func (x *exporting) write(m Manifest) (Written, error) {
	f := x.d.Files
	part, export := paths.CarryPart(x.team, x.drill()), x.target()
	if err := f.MakeCarryDir(); err != nil {
		return Written{}, err
	}
	if err := x.removePart(part); err != nil {
		return Written{}, err
	}
	out, err := f.Create(part)
	if err != nil {
		return Written{}, fmt.Errorf("cannot make %s: %w", part, err)
	}
	buf := bufio.NewWriterSize(out, 1<<20)
	written, err := WriteArchive(buf, m, x.open, x.max)
	if err == nil {
		err = buf.Flush()
	}
	if err == nil {
		err = out.Sync()
	}
	if err = errors.Join(err, out.Close()); err == nil && !x.drill() {
		err = x.same(export, written)
	}
	if err != nil {
		return Written{}, errors.Join(fmt.Errorf("the export of team %s: %w", x.team, err), f.Remove(part))
	}
	if err = f.Rename(part, export); err != nil {
		return Written{}, err
	}
	return written, f.SyncDir(paths.CarryDir)
}

// removePart removes a part a kill left, once Lstat shows it a regular file (SX7).
func (x *exporting) removePart(part string) error {
	info, err := x.d.Files.Stat(part)
	switch {
	case err != nil:
		return err
	case info.Kind == restore.KindAbsent:
		return nil
	case info.Kind != restore.KindFile:
		return fmt.Errorf("%s is not a regular file, so it is not a part an export left: remove it by hand", part)
	}
	return x.d.Files.Remove(part)
}

// same refuses an existing export whose digest is not written's (SX7): the sealed state changed.
func (x *exporting) same(export string, written Written) error {
	info, err := x.d.Files.Stat(export)
	switch {
	case err != nil:
		return err
	case info.Kind == restore.KindAbsent:
		return nil
	case info.Kind != restore.KindFile:
		return fmt.Errorf("%s is not a regular file", export)
	}
	prev, err := x.d.Files.Digest(export)
	if err != nil {
		return err
	}
	if prev.SHA256 != written.SHA256 {
		return fmt.Errorf("%s is an export of sha256 %s, and the sealed team now exports as sha256 %s: its state changed since that export", export, prev.SHA256, written.SHA256)
	}
	return nil
}

// open is the write's second pass over a member's source: the same kind and mode, its bytes
// checked against the manifest by WriteArchive.
func (x *exporting) open(mb Member) (io.ReadCloser, error) {
	path := x.sources[mb.Name]
	rc, info, err := x.d.Files.Open(path)
	if err != nil {
		return nil, err
	}
	if info.Mode != mb.Mode {
		return nil, errors.Join(fmt.Errorf("%s changed since the manifest was made: mode %04o, not %04o", mb.Name, info.Mode, mb.Mode), rc.Close())
	}
	return rc, nil
}

// survey is SX5's first pass: every member of SX2, its source checked by SX3 and SX4 and
// digested, the trees in their order and each tree in byte order of name.
func (x *exporting) survey() error {
	for _, tree := range []func() error{x.admin, x.ctl, x.status, x.hs} {
		from := len(x.members)
		if err := tree(); err != nil {
			return err
		}
		slices.SortFunc(x.members[from:], func(a, b Member) int { return strings.Compare(a.Name, b.Name) })
	}
	return nil
}

// admin is SX2's admin/: the applied records, the CA as host.CABackupFiles judges it, and root's
// backups.
func (x *exporting) admin() error {
	t := x.team
	x.tree("admin")
	for _, f := range []struct {
		name, path string
		required   bool
	}{
		{"team.json", paths.TeamDeclaration(t), true},
		{"server-name", paths.ServerNameFile(t), true},
		{"config.toml", paths.ServerConfig(t), true},
		{"units.json", paths.UnitsRecord(t), true},
		{"restore-epoch.json", paths.RestoreEpochFile(t), false},
	} {
		if err := x.file("admin/"+f.name, f.path, 0o600, adminCap, f.required); err != nil {
			return err
		}
	}
	ca, err := host.CABackupFiles(x.d.Root, t, x.d.StateOwnerUID)
	switch {
	case err != nil:
		return refusedf("the CA of team %s: %v", t, err)
	case ca == nil:
		return refusedf("team %s has no CA (%s): an export without it would make the target a new one", t, paths.CADir(t))
	}
	if err = x.dir("admin/tls", paths.CADir(t)); err != nil {
		return err
	}
	if err = x.file("admin/tls/ca.crt", paths.CACertFile(t), 0o644, adminCap, true); err != nil {
		return err
	}
	if err = x.file("admin/tls/ca.key", paths.CAKeyFile(t), 0o600, adminCap, true); err != nil {
		return err
	}
	return x.backups()
}

// backups is admin/backups/: every <n>.tar and last-run.json, when the directory is there.
func (x *exporting) backups() error {
	dir := paths.BackupDir(x.team)
	info, err := x.d.Files.Stat(dir)
	if err != nil || info.Kind == restore.KindAbsent {
		return err
	}
	if err = x.dir("admin/backups", dir); err != nil {
		return err
	}
	return x.listed("admin/backups", dir, func(e restore.Entry) (fs.FileMode, int64, bool) {
		switch {
		case e.Name == "last-run.json":
			return 0o600, adminCap, true
		case backupTarName.MatchString(e.Name):
			return 0o600, backupCap, true
		}
		return 0, 0, false
	})
}

// ctl is SX2's ctl/: the roster, the rooms and the admin token, the guest tokens and a past
// restore's lost events. Any other entry is refused.
func (x *exporting) ctl() error {
	t := x.team
	x.tree("ctl")
	for _, f := range []struct {
		name string
		cap  int64
	}{{"roster.json", ctlCap}, {"rooms.json", ctlCap}, {AdminTokenName, tokenCap}} {
		if err := x.file("ctl/"+f.name, paths.CtlState(t)+"/"+f.name, 0o600, f.cap, true); err != nil {
			return err
		}
	}
	guest := false
	err := x.listed("ctl", paths.CtlState(t), func(e restore.Entry) (fs.FileMode, int64, bool) {
		switch {
		case e.Name == "roster.json" || e.Name == "rooms.json" || e.Name == AdminTokenName:
			return 0, 0, true
		case e.Name == "guest" && e.Kind == restore.KindDirectory:
			guest = true
			return 0, 0, true
		case lostName.MatchString(e.Name):
			return 0o600, lostCap, true
		}
		return 0, 0, false
	})
	if err != nil || !guest {
		return err
	}
	if err = x.dir("ctl/guest", paths.GuestTokenDir(t)); err != nil {
		return err
	}
	return x.listed("ctl/guest", paths.GuestTokenDir(t), func(e restore.Entry) (fs.FileMode, int64, bool) {
		return 0o600, tokenCap, guestName.MatchString(e.Name)
	})
}

// status is SX2's status/: the approval alone (O2).
func (x *exporting) status() error {
	x.tree("status")
	return x.file("status/approved.json", paths.ApprovedFile(x.team), 0o640, approvedCap, true)
}

// hs is SX2's hs/: the whole of the homeserver's state, every directory 0700 and file 0600.
func (x *exporting) hs() error {
	base := paths.HomeserverPrivateState(x.team)
	info, err := x.d.Files.Stat(base)
	switch {
	case err != nil:
		return err
	case info.Kind != restore.KindDirectory:
		return refusedf("%s is not a directory", base)
	}
	x.tree("hs")
	return x.walk("hs", base)
}

func (x *exporting) walk(member, dir string) error {
	entries, err := x.d.Files.List(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name, path := member+"/"+e.Name, dir+"/"+e.Name
		if !validName(name) {
			return refusedf("an entry of %s has a name outside the member grammar", member)
		}
		switch e.Kind {
		case restore.KindDirectory:
			if err = x.dir(name, path); err == nil {
				err = x.walk(name, path)
			}
		case restore.KindFile:
			err = x.file(name, path, 0o600, noCap, true)
		case restore.KindAbsent, restore.KindOther:
			err = refusedf("%s is not a regular file or a directory", name)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// listed adds each entry of dir that carried admits as a file of its mode and cap, refusing
// any other; an entry carried admits with mode 0 is one the caller adds itself.
func (x *exporting) listed(member, dir string, carried func(restore.Entry) (fs.FileMode, int64, bool)) error {
	entries, err := x.d.Files.List(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := member + "/" + e.Name
		if !validName(name) {
			return refusedf("an entry of %s has a name outside the member grammar", member)
		}
		mode, limit, ok := carried(e)
		switch {
		case !ok:
			return refusedf("%s is not a member SX2 carries: remove it, or move it out of %s", name, dir)
		case mode == 0:
			continue
		case e.Kind != restore.KindFile:
			return refusedf("%s is not a regular file", name)
		}
		if err = x.file(name, dir+"/"+e.Name, mode, limit, true); err != nil {
			return err
		}
	}
	return nil
}

// tree adds one of SX2's trees: a directory member with no source of its own to judge.
func (x *exporting) tree(name string) {
	x.members = append(x.members, Member{Name: name, Dir: true, Mode: dirMode})
}

// dir adds a directory inside a tree, its source a directory of mode 0700 (SX3).
func (x *exporting) dir(name, path string) error {
	info, err := x.d.Files.Stat(path)
	switch {
	case err != nil:
		return err
	case info.Kind != restore.KindDirectory:
		return refusedf("%s is not a directory", name)
	case info.Mode != dirMode:
		return refusedf("%s is a directory of mode %04o, not %04o", name, info.Mode, dirMode)
	}
	x.members = append(x.members, Member{Name: name, Dir: true, Mode: dirMode})
	return nil
}

// file adds a file member: its source a regular file of mode, at most limit bytes (noCap: no
// limit), digested. An absent one that is not required is no member.
func (x *exporting) file(name, path string, mode fs.FileMode, limit int64, required bool) error {
	info, err := x.d.Files.Stat(path)
	switch {
	case err != nil:
		return err
	case info.Kind == restore.KindAbsent && !required:
		return nil
	case info.Kind == restore.KindAbsent:
		return refusedf("%s is missing (%s)", name, path)
	case info.Kind != restore.KindFile:
		return refusedf("%s is not a regular file (%s)", name, path)
	}
	rc, info, err := x.d.Files.Open(path)
	if err != nil {
		return err
	}
	switch {
	case info.Mode != mode:
		return errors.Join(refusedf("%s is of mode %04o, not %04o (SX3): it is never normalised", name, info.Mode, mode), rc.Close())
	case limit != noCap && info.Size > limit:
		return errors.Join(refusedf("%s is %d bytes, over its cap of %d bytes", name, info.Size, limit), rc.Close())
	}
	sum := sha256.New()
	n, err := io.Copy(sum, rc)
	switch err = errors.Join(err, rc.Close()); {
	case err != nil:
		return fmt.Errorf("cannot read %s: %w", path, err)
	case n != info.Size:
		return fmt.Errorf("%s changed while it was read: %d bytes, not %d", name, n, info.Size)
	}
	x.members = append(x.members, Member{Name: name, Mode: mode, Size: n, SHA256: hex.EncodeToString(sum.Sum(nil))})
	x.sources[name] = path
	return nil
}

// OS is Files on the host under the prefix Root ("" on a host); StateOwnerUID owns the carry
// directory and its archives (0 on a host).
type OS struct {
	Root          string
	StateOwnerUID uint32
}

func infoOf(f *os.File) (Info, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return Info{}, &fs.PathError{Op: "fstat", Path: f.Name(), Err: err}
	}
	k := restore.KindOther
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		k = restore.KindDirectory
	case unix.S_IFREG:
		k = restore.KindFile
	}
	return Info{Kind: k, Mode: fs.FileMode(st.Mode & 0o7777), Size: st.Size}, nil
}

// Stat implements Files.
func (o OS) Stat(path string) (Info, error) {
	f, err := o.openNoLinks(path, unix.O_PATH)
	if errors.Is(err, unix.ENOENT) {
		return Info{Kind: restore.KindAbsent}, nil
	}
	if errors.Is(err, unix.ELOOP) {
		return Info{Kind: restore.KindOther}, nil
	}
	if err != nil {
		return Info{}, err
	}
	info, err := infoOf(f)
	return info, errors.Join(err, f.Close())
}

// List implements Files.
func (o OS) List(dir string) ([]restore.Entry, error) {
	d, err := o.openNoLinks(dir, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, err
	}
	entries, err := d.ReadDir(-1)
	err = errors.Join(err, d.Close())
	out := make([]restore.Entry, 0, len(entries))
	for _, e := range entries {
		k := restore.KindOther
		switch {
		case e.Type().IsDir():
			k = restore.KindDirectory
		case e.Type().IsRegular():
			k = restore.KindFile
		}
		out = append(out, restore.Entry{Name: e.Name(), Kind: k})
	}
	slices.SortFunc(out, func(a, b restore.Entry) int { return strings.Compare(a.Name, b.Name) })
	return out, err
}

// Open implements Files.
func (o OS) Open(path string) (io.ReadCloser, Info, error) {
	f, err := o.openNoLinks(path, unix.O_RDONLY|unix.O_NONBLOCK)
	if err != nil {
		return nil, Info{}, err
	}
	info, err := infoOf(f)
	if err == nil && info.Kind != restore.KindFile {
		err = fmt.Errorf("%s is not a regular file", f.Name())
	}
	if err != nil {
		return nil, Info{}, errors.Join(err, f.Close())
	}
	return f, info, nil
}

// MakeCarryDir implements Files, as backup makes its directory (BT3).
func (o OS) MakeCarryDir() error {
	path := o.Root + paths.CarryDir
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("cannot make %s: %w", path, err)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("%s must be a directory, not a link: %w", path, err)
	}
	var st unix.Stat_t
	err = unix.Fstat(fd, &st)
	switch {
	case err != nil:
	case st.Uid != o.StateOwnerUID:
		err = fmt.Errorf("%s is owned by uid %d, not %d", path, st.Uid, o.StateOwnerUID)
	default:
		err = unix.Fchmod(fd, 0o700)
	}
	return errors.Join(err, unix.Close(fd))
}

// Create implements Files.
func (o OS) Create(path string) (Part, error) {
	full := o.Root + path
	fd, err := unix.Open(full, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, &fs.PathError{Op: "create", Path: full, Err: err}
	}
	return os.NewFile(uintptr(fd), full), nil
}

// Rename implements Files.
func (o OS) Rename(from, to string) error { return os.Rename(o.Root+from, o.Root+to) }

// Remove implements Files.
func (o OS) Remove(path string) error {
	if err := unix.Unlink(o.Root + path); err != nil {
		return &fs.PathError{Op: "remove", Path: o.Root + path, Err: err}
	}
	return nil
}

// SyncDir implements Files.
func (o OS) SyncDir(dir string) error {
	d, err := o.openNoLinks(dir, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

// Digest implements Files.
func (o OS) Digest(path string) (Written, error) {
	f, err := state.OpenOwnedFile(o.Root+path, o.StateOwnerUID)
	if err != nil {
		return Written{}, err
	}
	sum := sha256.New()
	n, err := io.Copy(sum, f)
	if err = errors.Join(err, f.Close()); err != nil {
		return Written{}, fmt.Errorf("cannot read %s: %w", o.Root+path, err)
	}
	return Written{Size: n, SHA256: hex.EncodeToString(sum.Sum(nil))}, nil
}

// openNoLinks opens path under Root with no link followed anywhere in it.
func (o OS) openNoLinks(path string, flags uint64) (*os.File, error) {
	full := o.Root + path
	fd, err := unix.Openat2(unix.AT_FDCWD, full, &unix.OpenHow{Flags: flags | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: full, Err: err}
	}
	return os.NewFile(uintptr(fd), full), nil
}
