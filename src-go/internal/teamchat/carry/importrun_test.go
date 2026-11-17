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
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// runWorld is an empty guest with an archive in its inbox, the source it was exported from, and
// every effect of the import a kill point: the staging's file calls, the record's writes and
// removals, and the child's carry.place.
type runWorld struct {
	*importWorld

	src *exportWorld
	raw []byte
	// onPlace runs inside the child's carry.place, before it writes anything.
	onPlace func()
	// placed is each carry.place asked: its lost exports and the bytes streamed.
	placed []PlaceArgs
	stream [][]byte
}

// newRunWorld is a guest for an archive of kind, the flags its kind and digest.
func newRunWorld(t *testing.T, kind Kind) *runWorld {
	t.Helper()
	src, raw := carriedSource(t, kind, nil)
	return guestFor(t, src, raw, kind)
}

// guestFor is a fresh empty guest holding raw, an archive of kind exported from src.
func guestFor(t *testing.T, src *exportWorld, raw []byte, kind Kind) *runWorld {
	t.Helper()
	w := &runWorld{importWorld: newGuest(t), src: src, raw: raw}
	// The directories the play and systemd make above a team's own.
	for dir, mode := range map[string]os.FileMode{
		filepath.Dir(paths.HomeserverPrivateStateRoot): 0o700, paths.HomeserverPrivateStateRoot: 0o755,
		paths.CtlStateRoot: 0o755, filepath.Dir(paths.StatusTeamDir(w.team)): 0o755,
	} {
		if err := os.MkdirAll(w.root+dir, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(w.root+dir, mode); err != nil {
			t.Fatal(err)
		}
	}
	w.flags.Drill = kind == KindDrill
	w.flags.SHA256 = digestOf(raw)
	w.inbox(raw)
	return w
}

func digestOf(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (w *runWorld) runDeps() Deps {
	d := w.importDeps()
	d.Stage = stageFiles{StageOS: StageOS{
		Root: w.root, StateOwnerUID: w.uid, CtlUID: int(w.uid), CtlGID: os.Getgid(),
		OwnerGroup: func() (int, error) { return os.Getgid(), nil },
	}, w: w}
	d.Child = importChild{w}
	return d
}

func (w *runWorld) run(flags ImportFlags) ([]byte, error) {
	return Import(context.Background(), w.runDeps(), w.team, flags)
}

func (w *runWorld) fresh() ([]byte, error) { return w.run(w.flags) }

func (w *runWorld) resume() ([]byte, error) {
	f := w.flags
	f.Resume = true
	return w.run(f)
}

func (w *runWorld) discard() ([]byte, error) {
	f := w.flags
	f.Discard = true
	return w.run(f)
}

// stageFiles is StageOS with every call an effect, so each is a kill point (SM5) and a
// failure point (fault).
type stageFiles struct {
	StageOS

	w *runWorld
}

func (f stageFiles) MakeHomeserverRoot() error {
	if err := f.w.fault("stage hsroot"); err != nil {
		return err
	}
	return f.StageOS.MakeHomeserverRoot()
}

func (f stageFiles) Mkdir(path string) error {
	if err := f.w.fault("stage mkdir " + path); err != nil {
		return err
	}
	return f.StageOS.Mkdir(path)
}

func (f stageFiles) Create(path string, mode fs.FileMode) (Part, error) {
	if err := f.w.fault("stage create " + path); err != nil {
		return nil, err
	}
	return f.StageOS.Create(path, mode)
}

func (f stageFiles) Rename(from, to string) error {
	if err := f.w.fault("stage rename " + from); err != nil {
		return err
	}
	return f.StageOS.Rename(from, to)
}

func (f stageFiles) RemoveTree(path string) error {
	if err := f.w.fault("stage remove " + path); err != nil {
		return err
	}
	return f.StageOS.RemoveTree(path)
}

func (f stageFiles) SyncDir(dir string) error {
	if err := f.w.fault("stage sync " + dir); err != nil {
		return err
	}
	return f.StageOS.SyncDir(dir)
}

func (f stageFiles) MkdirCtl(team paths.TeamID) error {
	if err := f.w.fault("stage mkdir ctl"); err != nil {
		return err
	}
	return f.StageOS.MkdirCtl(team)
}

func (f stageFiles) MkdirStatus(team paths.TeamID) error {
	if err := f.w.fault("stage mkdir status"); err != nil {
		return err
	}
	return f.StageOS.MkdirStatus(team)
}

func (f stageFiles) SaveApproved(team paths.TeamID, g declare.Gated) error {
	if err := f.w.fault("stage approve"); err != nil {
		return err
	}
	return f.StageOS.SaveApproved(team, g)
}

// importChild is the teamchat-ctl child's carry.place as ctl serves it: every target absent, the
// roster written in its canonical form, the lost exports read from the stream to exactly their
// sizes and digests, then each file created.
type importChild struct{ w *runWorld }

func (c importChild) Tail(context.Context, paths.TeamID, []byte) (Tail, error) {
	c.w.t.Error("the import read a tail")
	return Tail{}, errors.New("no tail")
}

func (c importChild) Check(context.Context, paths.TeamID, CheckArgs) (Presence, error) {
	c.w.t.Error("the import checked a tail")
	return Presence{}, errors.New("no check")
}

func (c importChild) Place(_ context.Context, team paths.TeamID, a PlaceArgs) ([]string, error) {
	c.w.effect("child place")
	if c.w.onPlace != nil {
		c.w.onPlace()
	}
	c.w.placed = append(c.w.placed, a)
	if string(a.TeamFile) != string(c.w.src.decl.Canonical()) {
		return nil, errors.New("carry.place was given another team file")
	}
	roster, err := apply.DecodeRosterFile(a.Roster, team)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{"roster.json": roster.Encode(), "rooms.json": a.Rooms, AdminTokenName: []byte(a.Token)}
	for _, g := range a.Guest {
		files["guest/"+g.Handle+".token"] = []byte(g.Token)
	}
	var streamed []byte
	if a.Stream != nil {
		if streamed, err = io.ReadAll(a.Stream); err != nil {
			return nil, err
		}
	}
	c.w.stream = append(c.w.stream, streamed)
	at := int64(0)
	for _, l := range a.Lost {
		if at+l.Size > int64(len(streamed)) {
			return nil, errors.New("a short stream")
		}
		body := streamed[at : at+l.Size]
		if digestOf(body) != l.SHA256 {
			return nil, errors.New("a lost export of another digest")
		}
		files[l.Name] = body
		at += l.Size
	}
	if at != int64(len(streamed)) {
		return nil, errors.New("a long stream")
	}
	dir := c.w.root + paths.CtlState(team)
	names := slices.Sorted(maps.Keys(files))
	for _, name := range names {
		data := files[name]
		if err = os.MkdirAll(filepath.Dir(dir+"/"+name), 0o700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(filepath.Clean(dir+"/"+name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, err
		}
		_, err = f.Write(data)
		if err = errors.Join(err, f.Close()); err != nil {
			return nil, err
		}
	}
	return names, nil
}

// contents is every path under the guest's root but the lock directory, relative, with its type,
// mode and the digest of its bytes.
func (w *runWorld) contents() map[string]string {
	w.t.Helper()
	out := map[string]string{}
	tree := os.DirFS(w.root)
	err := fs.WalkDir(tree, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix("/"+p, "/.")
		if rel == paths.AdminRunDir {
			return filepath.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		desc := info.Mode().String()
		if info.Mode().IsRegular() {
			raw, rerr := fs.ReadFile(tree, p)
			if rerr != nil {
				return rerr
			}
			desc += " " + digestOf(raw)
		}
		out[rel] = desc
		return nil
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return out
}

// sameContents fails at naming each path the two disagree on.
func sameContents(t *testing.T, at string, got, want map[string]string) {
	t.Helper()
	var diff []string
	for p, v := range want {
		if got[p] != v {
			diff = append(diff, fmt.Sprintf("%s: %q, want %q", p, got[p], v))
		}
	}
	for p, v := range got {
		if _, ok := want[p]; !ok {
			diff = append(diff, fmt.Sprintf("%s: %q, not expected", p, v))
		}
	}
	if len(diff) > 0 {
		slices.Sort(diff)
		t.Fatalf("%s:\n%s", at, strings.Join(diff, "\n"))
	}
}

func (w *runWorld) read(path string) []byte {
	w.t.Helper()
	raw, err := os.ReadFile(filepath.Clean(w.root + path))
	if err != nil {
		w.t.Fatal(err)
	}
	return raw
}

func (w *runWorld) mode(path string) fs.FileMode {
	w.t.Helper()
	info, err := os.Lstat(w.root + path)
	if err != nil {
		w.t.Fatal(err)
	}
	return info.Mode().Perm()
}

func (w *runWorld) recorded() bool { return w.has(paths.CarryImportRecord(w.team)) }

func (w *runWorld) importRecord() (ImportRecord, bool) {
	w.t.Helper()
	if !w.recorded() {
		return ImportRecord{}, false
	}
	r, err := DecodeImportRecord(w.read(paths.CarryImportRecord(w.team)), w.team)
	if err != nil {
		w.t.Fatal(err)
	}
	return r, true
}

// importAnswer is SI11's answer.
type importAnswer struct {
	team, kind string
	timeMS     int64
	roster     RosterFacts
	placed     []string
	next       []string
}

func importAnswerOf(t *testing.T, raw []byte, kind Kind) importAnswer {
	t.Helper()
	v, err := proto.Parse(raw)
	if err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	keys := []string{"v", keyTeam, keyKind, timeKey(kind), "roster", "placed", "next"}
	got := make([]string, 0, len(v.Members()))
	for _, m := range v.Members() {
		got = append(got, m.Key)
	}
	if !slices.Equal(got, keys) {
		t.Fatalf("the answer's keys are %v, not %v", got, keys)
	}
	get := func(k string) proto.Value {
		x, found := v.Get(k)
		if !found {
			t.Fatalf("no %s", k)
		}
		return x
	}
	strs := func(k string) []string {
		out := make([]string, 0, len(get(k).Items()))
		for _, x := range get(k).Items() {
			out = append(out, x.Str())
		}
		return out
	}
	r := get("roster")
	num := func(k string) int64 {
		x, found := r.Get(k)
		if !found {
			t.Fatalf("roster has no %s", k)
		}
		return x.Int()
	}
	return importAnswer{
		team: get(keyTeam).Str(), kind: get(keyKind).Str(), timeMS: get(timeKey(kind)).Int(),
		roster: RosterFacts{Seq: num("seq"), Rev: num("rev"), RestoreEpoch: num("restore_epoch")},
		placed: strs("placed"), next: strs("next"),
	}
}

// placedAsCarried holds the guest to the import's end state against the source: every carried
// file placed with its bytes, the roster and the epoch file byte for byte (SI12), the records
// made, the modes of SX3 and SI9, and nothing of the import's own left.
func (w *runWorld) placedAsCarried(kind Kind) {
	w.t.Helper()
	t, team, src := w.t, w.team, w.src
	for _, path := range []string{
		paths.TeamDeclaration(team), paths.ServerNameFile(team), paths.ServerConfig(team), paths.UnitsRecord(team),
		paths.RestoreEpochFile(team), paths.CACertFile(team), paths.CAKeyFile(team), paths.BackupTar(team, 8),
		paths.BackupTar(team, 10), paths.BackupLastRun(team), paths.RosterFile(team), paths.RoomsFile(team),
		paths.CtlState(team) + "/" + AdminTokenName, paths.GuestTokenCopy(team, mustSeat(t, "box1")),
		paths.RestoreLostFile(team, 3), paths.HomeserverPrivateState(team) + "/db/000004.sst",
		paths.HomeserverPrivateState(team) + "/backups/meta/8",
	} {
		if !bytes.Equal(w.read(path), src.read(path)) {
			t.Errorf("%s is not the source's bytes", path)
		}
	}
	for path, mode := range map[string]fs.FileMode{
		paths.CACertFile(team): 0o644, paths.CAKeyFile(team): 0o600, paths.ApprovedFile(team): 0o640,
		paths.CtlState(team): 0o700, paths.AdminState(team): 0o700, paths.HomeserverPrivateState(team): 0o700,
		paths.RosterFile(team): 0o600, paths.HomeserverPrivateState(team) + "/db/CURRENT": 0o600,
	} {
		if got := w.mode(path); got != mode {
			t.Errorf("%s has mode %04o, not %04o", path, got, mode)
		}
	}
	if _, err := declare.DecodeApproved(w.read(paths.ApprovedFile(team)), team); err != nil {
		t.Errorf("approved.json: %v", err)
	}
	rec, err := DecodeCarriedRecord(w.read(paths.CarriedRecord(team)), team)
	if err != nil {
		t.Fatalf("carried.json: %v", err)
	}
	want := CarriedRecord{
		Team: team, Kind: kind, SHA256: w.flags.SHA256, SealedMS: sealMS, ImportedMS: w.clock.Now().UnixMilli(),
		Roster: RosterFacts{Seq: 20, Rev: 5, RestoreEpoch: 2}, Tail: w.src.tail.Rooms,
	}
	if kind == KindDrill {
		want.SealedMS = rec.SealedMS
	}
	if !bytes.Equal(EncodeCarriedRecord(rec), EncodeCarriedRecord(want)) {
		t.Errorf("carried.json is %+v, not %+v", rec, want)
	}
	for _, path := range []string{paths.CarryStaging(team), paths.HomeserverCarryStaging(team), paths.CarryImportRecord(team), paths.HomeserverState(team), paths.StampFile(team)} {
		if w.has(path) {
			t.Errorf("%s is left", path)
		}
	}
	if !bytes.Equal(w.read(w.inboxPath()), w.raw) {
		t.Error("the import changed the archive in the inbox")
	}
}

func (w *runWorld) inboxPath() string {
	if w.flags.Drill {
		return paths.CarryDrillExport(w.team)
	}
	return paths.CarryExport(w.team)
}

// read is a source file's bytes.
func (w *exportWorld) read(path string) []byte {
	w.t.Helper()
	raw, err := os.ReadFile(filepath.Clean(w.root + path))
	if err != nil {
		w.t.Fatal(err)
	}
	return raw
}

// SI8, SI9, SI11, SI12: an import of each kind stages, places every root in its order, answers,
// and leaves the source's bytes; carry.place gets the lost export by name, size and sha256 and
// its bytes in the stream.
func TestImportPlacesTheTeam(t *testing.T) {
	for _, kind := range []Kind{KindSealed, KindDrill} {
		w := newRunWorld(t, kind)
		out, err := w.fresh()
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		w.placedAsCarried(kind)
		w.answered(out, kind)
		w.askedPlace(kind)
	}
}

// SH1's premise, the import's half: the placed homeserver tree is the state owner's (uid 0 on a
// host) at every level, directories and regular files alone, and HomeserverState, the link, is
// not made; systemd hands the tree to the dynamic uid and makes the link at the first start,
// which host check SH1 proves on the scratch guest.
func TestImportLeavesTheHomeserverTreeToSystemd(t *testing.T) {
	for _, kind := range []Kind{KindSealed, KindDrill} {
		w := newRunWorld(t, kind)
		if _, err := w.fresh(); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if bad := notOwnedBy(t, w.root+paths.HomeserverPrivateState(w.team), w.uid); len(bad) > 0 {
			t.Errorf("%s: not the state owner's (%d) regular file or directory: %v", kind, w.uid, bad)
		}
		if w.has(paths.HomeserverState(w.team)) {
			t.Errorf("%s: %s was made before the first start", kind, paths.HomeserverState(w.team))
		}
	}
}

// notOwnedBy names each entry under dir, dir included, that is not a directory or regular
// file owned by uid.
func notOwnedBy(t *testing.T, dir string, uid uint32) []string {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	var bad []string
	err = fs.WalkDir(root.FS(), ".", func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, lerr := root.Lstat(path)
		if lerr != nil {
			return lerr
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != uid || (!info.IsDir() && !info.Mode().IsRegular()) {
			bad = append(bad, path)
		}
		return nil
	})
	if err = errors.Join(err, root.Close()); err != nil {
		t.Fatal(err)
	}
	return bad
}

// answered holds out to SI11's answer for kind.
func (w *runWorld) answered(out []byte, kind Kind) {
	w.t.Helper()
	a := importAnswerOf(w.t, out, kind)
	next := []string{"team carry-check --team ops", "team apply"}
	if kind == KindDrill {
		next = next[:1]
	}
	if a.team != "ops" || a.kind != string(kind) || a.roster != (RosterFacts{Seq: 20, Rev: 5, RestoreEpoch: 2}) ||
		!slices.Equal(a.placed, PlacedRoots(w.team)) || !slices.Equal(a.next, next) {
		w.t.Errorf("%s: the answer is %+v", kind, a)
	}
	if kind == KindSealed && a.timeMS != sealMS {
		w.t.Errorf("the answer's sealed_ms is %d", a.timeMS)
	}
}

// askedPlace holds carry.place's one call to the carried token, guest and lost export.
func (w *runWorld) askedPlace(kind Kind) {
	t := w.t
	t.Helper()
	lost := w.src.read(paths.RestoreLostFile(w.team, 3))
	if len(w.placed) != 1 || len(w.placed[0].Lost) != 1 ||
		w.placed[0].Lost[0] != (LostExport{Name: "restore-3-lost.jsonl", Size: int64(len(lost)), SHA256: digestOf(lost)}) ||
		!bytes.Equal(w.stream[0], lost) {
		t.Errorf("%s: carry.place was asked %+v with %q", kind, w.placed, w.stream)
	}
	if len(w.placed) == 1 && (w.placed[0].Token != importToken || len(w.placed[0].Guest) != 1 || w.placed[0].Guest[0] != (GuestToken{Handle: "box1", Token: "guest token one"})) {
		t.Errorf("%s: carry.place's token and guests are %q, %+v", kind, w.placed[0].Token, w.placed[0].Guest)
	}
}

// SI1, SI10: an archive whose whole digest is not --sha256 is staged, then the staging and the
// record are removed: "import not done" (1), the guest as it was.
func TestImportDigestMismatchRemovesTheStaging(t *testing.T) {
	w := newRunWorld(t, KindSealed)
	before := w.contents()
	w.flags.SHA256 = strings.Repeat("0", 64)
	_, err := w.fresh()
	if err == nil || errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "import not done") || !strings.Contains(err.Error(), digestOf(w.raw)) {
		t.Fatalf("err = %v", err)
	}
	sameContents(t, "after the mismatch", w.contents(), before)
}

// SI7: the age is checked again just before the commit point; past it the import aborts (70)
// with its record at placing, and --discard leaves the guest empty for T again.
func TestImportAgeAgainBeforeTheCommitPoint(t *testing.T) {
	w := newRunWorld(t, KindSealed)
	before := w.contents()
	w.onPlace = func() { w.clock.now = w.clock.now.Add(MaxAge) }
	_, err := w.fresh()
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "--discard") {
		t.Fatalf("err = %v", err)
	}
	rec, found := w.importRecord()
	if !found || rec.Step != ImportPlacing || w.has(paths.AdminState(w.team)) {
		t.Fatalf("record %+v (%v), admin state %v", rec, found, w.has(paths.AdminState(w.team)))
	}
	if _, err = w.resume(); !errors.Is(err, ErrRefused) {
		t.Fatalf("a resume past the age: %v", err)
	}
	if _, err = w.discard(); err != nil {
		t.Fatal(err)
	}
	sameContents(t, "after the discard", w.contents(), before)
	w.clock.now = w.clock.now.Add(-MaxAge)
	if err = w.validate(); err != nil {
		t.Fatalf("the guest is not empty for ops again: %v", err)
	}
}

// SG5: a fresh import refuses an import record; --resume and --discard refuse its absence, and a
// --sha256 or a kind that is not the record's.
func TestImportGuardsItsRecord(t *testing.T) {
	w := newRunWorld(t, KindSealed)
	if _, err := w.resume(); !errors.Is(err, ErrRefused) {
		t.Fatalf("a resume with no record: %v", err)
	}
	if _, err := w.discard(); !errors.Is(err, ErrRefused) {
		t.Fatalf("a discard with no record: %v", err)
	}
	w.put(paths.CarryImportRecord(w.team), string(EncodeImportRecord(ImportRecord{Team: w.team, Kind: KindSealed, SHA256: w.flags.SHA256, Step: ImportStaging})))
	if _, err := w.fresh(); !errors.Is(err, ErrSealed) {
		t.Fatalf("a fresh import beside a record: %v", err)
	}
	other := w.flags
	other.Resume, other.SHA256 = true, strings.Repeat("1", 64)
	if _, err := w.run(other); !errors.Is(err, ErrRefused) {
		t.Fatalf("a resume of another digest: %v", err)
	}
	other.SHA256, other.Drill = w.flags.SHA256, true
	if _, err := w.run(other); !errors.Is(err, ErrRefused) {
		t.Fatalf("a resume of another kind: %v", err)
	}
	if _, err := w.resume(); err != nil {
		t.Fatalf("a resume at staging: %v", err)
	}
	w.placedAsCarried(KindSealed)
}

// SI10: a resume at placing takes a renamed root (target present, staging absent) as done, and
// refuses any other mix, naming both paths.
func TestImportResumeRefusesAMix(t *testing.T) {
	w := newRunWorld(t, KindSealed)
	w.onPlace = func() { w.clock.now = w.clock.now.Add(MaxAge) }
	if _, err := w.fresh(); !errors.Is(err, ErrRefused) {
		t.Fatal(err)
	}
	w.onPlace = nil
	w.clock.now = w.clock.now.Add(-MaxAge)
	if err := os.Mkdir(w.root+paths.HomeserverCarryStaging(w.team), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := w.resume()
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), paths.HomeserverCarryStaging(w.team)) ||
		!strings.Contains(err.Error(), paths.HomeserverPrivateState(w.team)) {
		t.Fatalf("err = %v", err)
	}
	if err = os.Remove(w.root + paths.HomeserverCarryStaging(w.team)); err != nil {
		t.Fatal(err)
	}
	if _, err = w.resume(); err != nil {
		t.Fatal(err)
	}
	w.placedAsCarried(KindSealed)
}

// SI10: --discard at placing removes only a root that is a directory, never through a link.
func TestImportDiscardRefusesALinkedRoot(t *testing.T) {
	w := newRunWorld(t, KindSealed)
	w.onPlace = func() { w.clock.now = w.clock.now.Add(MaxAge) }
	if _, err := w.fresh(); !errors.Is(err, ErrRefused) {
		t.Fatal(err)
	}
	hs := w.root + paths.HomeserverPrivateState(w.team)
	if err := os.Rename(hs, hs+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hs+".moved", hs); err != nil {
		t.Fatal(err)
	}
	if _, err := w.discard(); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), paths.HomeserverPrivateState(w.team)) {
		t.Fatalf("err = %v", err)
	}
	if !w.has(paths.HomeserverPrivateState(w.team)+".moved/db/CURRENT") || !w.recorded() {
		t.Fatal("the discard removed through a link, or removed the record")
	}
}

// SI10's answer to --discard: the roots and staging names removed.
func TestImportDiscardAnswers(t *testing.T) {
	w := newRunWorld(t, KindSealed)
	before := w.contents()
	w.kill = w.callAt(t, "child place")
	killImport(t, w, w.fresh)
	w.kill = 0
	out, err := w.discard()
	if err != nil {
		t.Fatal(err)
	}
	v, err := proto.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	removed, found := v.Get("discarded")
	if !found || len(removed.Items()) == 0 {
		t.Fatalf("the answer is %s", out)
	}
	sameContents(t, "after the discard", w.contents(), before)
}

// callAt is the number of the first call named name in a whole import on a fresh guest.
func (w *runWorld) callAt(t *testing.T, name string) int {
	t.Helper()
	i := slices.Index(w.callsOf(t), name)
	if i < 0 {
		t.Fatalf("no call %s", name)
	}
	return i + 1
}

// callsOf is the calls of a whole import on a fresh guest for the same archive.
func (w *runWorld) callsOf(t *testing.T) []string {
	t.Helper()
	ref := guestFor(t, w.src, w.raw, kindOf(w.flags))
	if _, err := ref.fresh(); err != nil {
		t.Fatal(err)
	}
	return ref.calls
}

func kindOf(f ImportFlags) Kind {
	if f.Drill {
		return KindDrill
	}
	return KindSealed
}

// killImport runs the import until the kill and asserts it was killed.
func killImport(t *testing.T, w *runWorld, run func() ([]byte, error)) {
	t.Helper()
	done := make(chan bool, 1)
	go func() {
		returned := false
		defer func() { done <- returned }()
		if _, err := run(); err != nil {
			t.Errorf("the run failed before its kill: %v", err)
		}
		returned = true
	}()
	if <-done {
		t.Fatalf("the import was not killed at call %d", w.kill)
	}
}

// SM5, SI10: a kill at every call of the import, then the way on: with a record, --resume ends
// in the same guest as an import never killed, and --discard in the guest before it; with none,
// either the import had landed or nothing was written and a fresh import lands.
func TestImportKillAtEveryCall(t *testing.T) {
	for _, kind := range []Kind{KindSealed, KindDrill} {
		src, raw := carriedSource(t, kind, nil)
		ref := guestFor(t, src, raw, kind)
		before := ref.contents()
		if _, err := ref.fresh(); err != nil {
			t.Fatal(err)
		}
		landed := ref.contents()
		for k := 1; k <= len(ref.calls); k++ {
			for _, way := range []string{"resume", "discard"} {
				at := string(kind) + " kill " + strconv.Itoa(k) + " (" + ref.calls[k-1] + "), then " + way
				w := guestFor(t, src, raw, kind)
				w.kill = k
				killImport(t, w, w.fresh)
				w.kill = 0
				w.wayOn(at, way, before, landed)
			}
		}
	}
}

// SI10: a failure answered by any file call of the import or its record (not a kill: the code
// sees it) fails the import with that error, never a refusal; the way on then ends as after a
// kill: --resume in the landed guest, --discard in the guest before it.
func TestImportFailureAtEveryCall(t *testing.T) {
	for _, kind := range []Kind{KindSealed, KindDrill} {
		src, raw := carriedSource(t, kind, nil)
		ref := guestFor(t, src, raw, kind)
		before := ref.contents()
		if _, err := ref.fresh(); err != nil {
			t.Fatal(err)
		}
		landed := ref.contents()
		for k, call := range ref.calls {
			if !strings.HasPrefix(call, "stage ") && !strings.HasPrefix(call, "write ") && !strings.HasPrefix(call, "remove ") {
				continue
			}
			for _, way := range []string{"resume", "discard"} {
				at := string(kind) + " failure " + strconv.Itoa(k+1) + " (" + call + "), then " + way
				w := guestFor(t, src, raw, kind)
				w.fail = k + 1
				_, err := w.fresh()
				w.fail = 0
				if !errors.Is(err, errInjected) || errors.Is(err, ErrRefused) {
					t.Fatalf("%s: err = %v", at, err)
				}
				w.wayOn(at, way, before, landed)
			}
		}
	}
}

// wayOn is the way on after a kill, and the guest it must end in: landed after a resume, a
// fresh import, or an import the kill did not stop; before after a discard, or after a kill
// before anything was written.
func (w *runWorld) wayOn(at, way string, before, landed map[string]string) {
	t := w.t
	t.Helper()
	var err error
	want := landed
	switch {
	case !w.recorded() && w.has(paths.AdminState(w.team)):
	case !w.recorded():
		sameContents(t, at+": nothing written", w.contents(), before)
		if way == "discard" {
			return
		}
		_, err = w.fresh()
	case way == "resume":
		_, err = w.resume()
	default:
		_, err = w.discard()
		want = before
	}
	if err != nil {
		t.Fatalf("%s: %v", at, err)
	}
	sameContents(t, at, w.contents(), want)
}

// SI10: a resume at staged re-checks the staged tree against the manifest copy, and refuses a
// staged file that changed.
func TestImportResumeAtStagedChecksTheStagedTree(t *testing.T) {
	w := stagedRunWorld(t)
	w.put(paths.CarryStaging(w.team)+"/admin/units.json", unitsJSON(9000))
	if _, err := w.resume(); err == nil || !strings.Contains(err.Error(), "admin/units.json") {
		t.Fatalf("err = %v", err)
	}
}

// stagedRunWorld is a sealed import killed with its record at step staged.
func stagedRunWorld(t *testing.T) *runWorld {
	t.Helper()
	w := newRunWorld(t, KindSealed)
	// The record is written at staging, at staged, then at placing once per root; a kill at the
	// third write leaves it at staged.
	k, writes := -1, 0
	for i, c := range w.callsOf(t) {
		if c == "write ops.import.json" {
			writes++
			if writes == 3 {
				k = i
				break
			}
		}
	}
	w.kill = k + 1
	killImport(t, w, w.fresh)
	w.kill = 0
	rec, found := w.importRecord()
	if !found || rec.Step != ImportStaged {
		t.Fatalf("the record is %+v (%v)", rec, found)
	}
	return w
}

// SL1, CN5: the import reads the archive cap first, and waits 0 for the lock: any holder is busy.
func TestImportNeedsTheCapAndTheLock(t *testing.T) {
	w := newRunWorld(t, KindSealed)
	d := w.runDeps()
	d.MaxArchive = func() (int64, error) { return 0, errors.New("no setting") }
	if _, err := Import(context.Background(), d, w.team, w.flags); err == nil || !strings.Contains(err.Error(), "no setting") {
		t.Fatalf("err = %v", err)
	}
	held, err := teamlock.Take(context.Background(), w.root+paths.AdminRunDir, w.team, "apply", 0, w.clock, teamlock.Unguarded)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.fresh()
	if rerr := held.Release(); rerr != nil {
		t.Fatal(rerr)
	}
	if !errors.Is(err, teamlock.ErrBusy) || w.clock.slept != 0 || len(w.calls) != 0 {
		t.Fatalf("err = %v, slept %v, calls %v", err, w.clock.slept, w.calls)
	}
}
