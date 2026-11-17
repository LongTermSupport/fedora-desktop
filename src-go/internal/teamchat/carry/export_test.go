package carry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// exportMax is the export world's archive cap.
const exportMax = 64 << 20

// exportWorld is a sealed team with every source SX2 names, on the seal world's tree.
type exportWorld struct {
	*sealWorld

	max    int64
	maxErr error
	// changeAt changes a source on disk as one of its opens is made, for the second pass.
	changeAt map[string]change
	opens    map[string]int
}

func newExportWorld(t *testing.T) *exportWorld {
	t.Helper()
	w := newSourcesWorld(t)
	if _, err := w.seal(); err != nil {
		t.Fatal(err)
	}
	return w
}

// newSourcesWorld is a live team, not sealed, with every source SX2 names.
func newSourcesWorld(t *testing.T) *exportWorld {
	t.Helper()
	w := &exportWorld{sealWorld: newSealWorld(t), max: exportMax, changeAt: map[string]change{}, opens: map[string]int{}}
	team := w.team
	ca, err := pki.Maker{Clock: clock.NewFake(0, sealMS)}.NewCA("teamchat ops CA")
	if err != nil {
		t.Fatal(err)
	}
	w.putMode(paths.CACertFile(team), string(ca.CertPEM()), 0o644)
	w.putMode(paths.CAKeyFile(team), string(ca.KeyPEM()), 0o600)
	w.put(paths.BackupTar(team, 8), "the tar of backup 8")
	w.put(paths.BackupTar(team, 10), "the tar of backup 10")
	w.put(paths.BackupLastRun(team), `{"v":1}`)
	w.put(paths.FirewallRecord(team), `{"not":"carried"}`)
	w.put(paths.RoomsFile(team), `{"rooms":"of ops"}`)
	w.put(paths.CtlState(team)+"/"+AdminTokenName, "the admin token")
	w.put(paths.GuestTokenCopy(team, mustSeat(t, "box1")), "guest token one")
	w.put(paths.RestoreLostFile(team, 3), "{\"lost\":1}\n")
	w.putMode(paths.ApprovedFile(team), `{"approved":{}}`, 0o640)
	hs := paths.HomeserverPrivateState(team)
	w.put(hs+"/db/CURRENT", "MANIFEST-000005\n")
	w.put(hs+"/db/000004.sst", strings.Repeat("s", 3000))
	w.put(hs+"/backups/meta/8", "meta 8")
	w.put(hs+"/backups/private/8/000004.sst", strings.Repeat("s", 3000))
	w.put(hs+"/backups/shared_checksum/x_1.sst", "shared")
	return w
}

func mustSeat(t *testing.T, s string) paths.SeatName {
	t.Helper()
	n, err := paths.ParseSeatName(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (w *exportWorld) putMode(path, content string, mode fs.FileMode) {
	w.t.Helper()
	w.put(path, content)
	w.chmod(path, mode)
}

// exportFiles is carry.OS with each source's opens counted, and a source changed on disk at the
// open changeAt names.
type exportFiles struct {
	OS

	w *exportWorld
}

func (f exportFiles) Open(path string) (io.ReadCloser, Info, error) {
	f.w.opens[path]++
	if c, ok := f.w.changeAt[path]; ok && c.at == f.w.opens[path] {
		c.do()
	}
	return f.OS.Open(path)
}

// The calls that change the carry directory are effects, so each is a kill point (SM5).

func (f exportFiles) MakeCarryDir() error {
	f.w.effect("files mkdir")
	return f.OS.MakeCarryDir()
}

func (f exportFiles) Create(path string) (Part, error) {
	f.w.effect("files create " + filepath.Base(path))
	return f.OS.Create(path)
}

func (f exportFiles) Rename(from, to string) error {
	f.w.effect("files rename " + filepath.Base(from))
	return f.OS.Rename(from, to)
}

func (f exportFiles) Remove(path string) error {
	f.w.effect("files remove " + filepath.Base(path))
	return f.OS.Remove(path)
}

func (f exportFiles) SyncDir(dir string) error {
	f.w.effect("files syncdir")
	return f.OS.SyncDir(dir)
}

// change is a source changed on disk at its open number at.
type change struct {
	at int
	do func()
}

// changeOn replaces the source at path by content when its open number at is made.
func (w *exportWorld) changeOn(path, content string, at int) {
	w.changeAt[path] = change{at: at, do: func() { w.put(path, content) }}
}

func (w *exportWorld) exportDeps() Deps {
	d := w.deps()
	d.Files = exportFiles{OS: OS{Root: w.root, StateOwnerUID: w.uid}, w: w}
	d.MaxArchive = func() (int64, error) { return w.max, w.maxErr }
	return d
}

func (w *exportWorld) export() ([]byte, error) {
	return Export(context.Background(), w.exportDeps(), w.team)
}

// archive is the export on disk; nil when there is none.
func (w *exportWorld) archive() []byte { return w.archiveAt(paths.CarryExport(w.team)) }

// archiveAt is the archive at path; nil when there is none.
func (w *exportWorld) archiveAt(path string) []byte {
	w.t.Helper()
	if !w.has(path) {
		return nil
	}
	raw, err := os.ReadFile(filepath.Clean(w.root + path))
	if err != nil {
		w.t.Fatal(err)
	}
	return raw
}

// exportMembers are the world's members in archive order (SX2: the trees in order, each in byte
// order of name).
func (w *exportWorld) exportMembers() []string {
	return []string{
		"admin", "admin/backups", "admin/backups/10.tar", "admin/backups/8.tar", "admin/backups/last-run.json",
		"admin/config.toml", "admin/restore-epoch.json", "admin/server-name", "admin/team.json",
		"admin/tls", "admin/tls/ca.crt", "admin/tls/ca.key", "admin/units.json",
		"ctl", "ctl/admin.token", "ctl/guest", "ctl/guest/box1.token", "ctl/restore-3-lost.jsonl", "ctl/rooms.json", "ctl/roster.json",
		"status", "status/approved.json",
		"hs", "hs/backups", "hs/backups/meta", "hs/backups/meta/8", "hs/backups/private", "hs/backups/private/8",
		"hs/backups/private/8/000004.sst", "hs/backups/shared_checksum", "hs/backups/shared_checksum/x_1.sst",
		"hs/db", "hs/db/000004.sst", "hs/db/CURRENT",
	}
}

// sourceOf is a member's source path on the host.
func (w *exportWorld) sourceOf(member string) string {
	w.t.Helper()
	tree, rest, found := strings.Cut(member, "/")
	base := map[string]string{
		"admin": paths.AdminState(w.team), "ctl": paths.CtlState(w.team), "status": paths.StatusTeamDir(w.team),
		"hs": paths.HomeserverPrivateState(w.team),
	}[tree]
	if !found {
		return base
	}
	return base + "/" + rest
}

// readExport reads raw back as the import would, collecting each member's bytes.
func readExport(t *testing.T, raw []byte, team paths.TeamID) (Manifest, []string, map[string][]byte, Read) {
	t.Helper()
	var m Manifest
	var names []string
	data := map[string][]byte{}
	rd, err := ReadArchive(bytes.NewReader(raw), team, exportMax, func(got Manifest) error { m = got; return nil },
		func(mb Member, body io.Reader) error {
			names = append(names, mb.Name)
			if mb.Dir {
				return nil
			}
			b, rerr := io.ReadAll(body)
			data[mb.Name] = b
			return rerr
		})
	if err != nil {
		t.Fatalf("the export does not read back: %v", err)
	}
	return m, names, data, rd
}

// exportAnswer is SX6's answer read strictly.
type exportAnswer struct {
	team, kind, path, sum string
	v, size, sealedMS     int64
}

func exportAnswerOf(t *testing.T, raw []byte, kind Kind) exportAnswer {
	t.Helper()
	v, err := proto.Parse(raw)
	if err != nil {
		t.Fatalf("the answer %q is not JSON: %v", raw, err)
	}
	keys := make([]string, 0, len(v.Members()))
	for _, m := range v.Members() {
		keys = append(keys, m.Key)
	}
	if want := []string{"v", "team", "kind", "path", "size", "sha256", timeKey(kind)}; !slices.Equal(keys, want) {
		t.Fatalf("the answer's keys are %v, want %v", keys, want)
	}
	get := func(k string) proto.Value {
		got, found := v.Get(k)
		if !found {
			t.Fatalf("the answer has no %s", k)
		}
		return got
	}
	return exportAnswer{
		team: get("team").Str(), kind: get("kind").Str(), path: get("path").Str(), sum: get("sha256").Str(),
		v: get("v").Int(), size: get("size").Int(), sealedMS: get(timeKey(kind)).Int(),
	}
}

// SX1, SX2, SX5, SX6: the export of a sealed team is one archive at CarryExport, 0600 in a 0700
// carry directory, no part left; it reads back with the seal's facts in its manifest and every
// member of SX2 in order with its source's bytes, and nothing SX2 leaves behind. The answer names
// the path, the size, the whole-archive digest and the seal's time.
func TestExportWritesTheSealedArchive(t *testing.T) {
	w := newExportWorld(t)
	out, err := w.export()
	if err != nil {
		t.Fatal(err)
	}
	raw := w.archive()
	if raw == nil {
		t.Fatal("there is no export")
	}
	got := w.answered(out, raw, KindSealed)
	m, names, data, rd := readExport(t, raw, w.team)
	if rd.SHA256 != got.sum || rd.Size != got.size {
		t.Errorf("read %+v, answered %+v", rd, got)
	}
	if m.Kind != KindSealed || m.Format != ExportFormat || m.ServerName != w.decl.ServerName || !sealEqual(m.Seal, w.record().Seal) {
		t.Errorf("manifest %+v", m)
	}
	if !slices.Equal(names, w.exportMembers()) {
		t.Errorf("members\n%q\nwant\n%q", names, w.exportMembers())
	}
	w.carriedAsIs(data)
	modes := map[string]fs.FileMode{}
	for _, mb := range m.Members {
		modes[mb.Name] = mb.Mode
	}
	if modes["admin/tls/ca.crt"] != 0o644 || modes["status/approved.json"] != 0o640 || modes["ctl/roster.json"] != 0o600 || modes["hs/db"] != 0o700 {
		t.Errorf("modes %v", modes)
	}
}

// answered checks SX6's answer against the archive raw on disk, and SX1's files: the export
// 0600 in a 0700 carry directory, and no part left. It is the answer read.
func (w *exportWorld) answered(out, raw []byte, kind Kind) exportAnswer {
	w.t.Helper()
	sum := sha256.Sum256(raw)
	got := exportAnswerOf(w.t, out, kind)
	export := paths.CarryExport(w.team)
	if kind == KindDrill {
		export = paths.CarryDrillExport(w.team)
	}
	want := exportAnswer{team: "ops", kind: string(kind), path: export, sum: hex.EncodeToString(sum[:]), v: 1, size: int64(len(raw)), sealedMS: sealMS}
	if got != want {
		w.t.Errorf("answer %+v, want %+v", got, want)
	}
	for path, mode := range map[string]fs.FileMode{export: 0o600, paths.CarryDir: fs.ModeDir | 0o700} {
		if info, err := os.Lstat(w.root + path); err != nil || info.Mode() != mode {
			w.t.Errorf("%s: %v %v, want mode %v", path, info, err, mode)
		}
	}
	if w.has(paths.CarryPart(w.team, kind == KindDrill)) {
		w.t.Error("the part is left")
	}
	return got
}

// carriedAsIs checks each file member's bytes, as read back, are its source's.
func (w *exportWorld) carriedAsIs(data map[string][]byte) {
	w.t.Helper()
	files := 0
	for _, name := range w.exportMembers() {
		src, err := os.ReadFile(filepath.Clean(w.root + w.sourceOf(name)))
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) && errors.Is(err, syscall.EISDIR) {
			continue
		}
		files++
		if body, read := data[name]; err != nil || !read || !bytes.Equal(src, body) {
			w.t.Errorf("%s: %d bytes read back, not its source's (%v)", name, len(body), err)
		}
	}
	if len(data) != files {
		w.t.Errorf("%d files read back, want %d", len(data), files)
	}
}

// SX1, SX7: two exports of one sealed team are byte-identical, the second answers the same and
// replaces the first; it is also the proof the state is cold.
func TestTwoExportsAreByteIdentical(t *testing.T) {
	w := newExportWorld(t)
	first, err := w.export()
	if err != nil {
		t.Fatal(err)
	}
	a := w.archive()
	second, err := w.export()
	if err != nil {
		t.Fatalf("the second export: %v", err)
	}
	if !bytes.Equal(a, w.archive()) || !bytes.Equal(first, second) {
		t.Fatal("two exports of one sealed team differ")
	}
}

// SX5: a source that changes between the digest pass and the write fails the export (1, not a
// refusal), naming the source, whether its size changed or only its bytes; nothing is left in
// the carry directory.
func TestExportFailsWhenASourceChangesBetweenPasses(t *testing.T) {
	for member, content := range map[string]string{
		"ctl/roster.json":     `{"shorter":1}`,
		"hs/db/000004.sst":    strings.Repeat("s", 3001),
		"admin/backups/8.tar": "the tar of backup 9",
	} {
		w := newExportWorld(t)
		w.changeOn(w.sourceOf(member), content, 2)
		_, err := w.export()
		if err == nil || errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), member) || !strings.Contains(err.Error(), "changed") {
			t.Errorf("%s: %v, want a failure naming the changed source", member, err)
		}
		if w.has(paths.CarryExport(w.team)) || w.has(paths.CarryPart(w.team, false)) {
			t.Errorf("%s: an export or a part is left", member)
		}
	}
}

// SX7: a part a kill left is removed and the export starts again, its result the same bytes as
// an export with none (each world's CA is its own, so the clean export is the same world's); a
// part that is not a regular file fails the export, and is left.
func TestExportRemovesALeftPart(t *testing.T) {
	w := newExportWorld(t)
	if _, err := w.export(); err != nil {
		t.Fatal(err)
	}
	clean := w.archive()
	w.remove(paths.CarryExport(w.team))
	w.put(paths.CarryPart(w.team, false), "half an archive")
	if _, err := w.export(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.archive(), clean) || w.has(paths.CarryPart(w.team, false)) {
		t.Fatal("the export after a left part is not the clean export's bytes, or the part is left")
	}
	w = newExportWorld(t)
	if err := os.Mkdir(w.root+paths.CarryPart(w.team, false), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := w.export(); err == nil || errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), paths.CarryPart(w.team, false)) {
		t.Fatalf("a part that is a directory: %v", err)
	}
	if w.has(paths.CarryExport(w.team)) || !w.has(paths.CarryPart(w.team, false)) {
		t.Error("an export was made, or the directory was removed")
	}
}

// SX7: an existing export of another digest means the sealed state changed: the export fails
// (1) naming both digests, and the existing archive is left as it was.
func TestAChangedExportIsAFailure(t *testing.T) {
	w := newExportWorld(t)
	if _, err := w.export(); err != nil {
		t.Fatal(err)
	}
	before := w.archive()
	sumBefore := sha256.Sum256(before)
	w.put(paths.ServerConfig(w.team), "server_name = \"changed\"\n")
	_, err := w.export()
	if err == nil || errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), hex.EncodeToString(sumBefore[:])) || strings.Count(err.Error(), "sha256 ") < 2 {
		t.Fatalf("got %v, want a failure naming both digests", err)
	}
	if !bytes.Equal(before, w.archive()) || w.has(paths.CarryPart(w.team, false)) {
		t.Fatal("the existing export was changed, or the part is left")
	}
}

// exportRefusal is one refusal of TestExportRefusals: what makes it, what its message says, and
// a name it must not quote.
type exportRefusal struct {
	setup  func(w *exportWorld)
	says   string
	quotes string
}

func exportRefusals() map[string]exportRefusal {
	hs := func(w *exportWorld) string { return paths.HomeserverPrivateState(w.team) }
	return map[string]exportRefusal{
		"not sealed": {setup: func(w *exportWorld) { w.remove(paths.SealRecord(w.team)) }, says: "not sealed"},
		"sealing": {setup: func(w *exportWorld) {
			w.put(paths.SealRecord(w.team), string(EncodeSealRecord(SealRecord{Team: w.team, Step: StepSealing, StartedMS: 1, Tail: w.tail.Rooms})))
		}, says: "step sealing"},
		"a drill record":  {setup: func(w *exportWorld) { w.put(paths.DrillRecord(w.team), "{}") }, says: "drill"},
		"an import":       {setup: func(w *exportWorld) { w.put(paths.CarryImportRecord(w.team), "{}") }, says: "import"},
		"a ctl mode":      {setup: func(w *exportWorld) { w.chmod(paths.RosterFile(w.team), 0o640) }, says: "ctl/roster.json"},
		"a cert mode":     {setup: func(w *exportWorld) { w.chmod(paths.CACertFile(w.team), 0o600) }, says: "admin/tls/ca.crt"},
		"an approval":     {setup: func(w *exportWorld) { w.chmod(paths.ApprovedFile(w.team), 0o644) }, says: "status/approved.json"},
		"a hs mode":       {setup: func(w *exportWorld) { w.chmod(hs(w)+"/db/CURRENT", 0o644) }, says: "hs/db/CURRENT"},
		"a hs dir mode":   {setup: func(w *exportWorld) { w.chmod(hs(w)+"/db", 0o750) }, says: "hs/db"},
		"a guest dir":     {setup: func(w *exportWorld) { w.chmod(paths.GuestTokenDir(w.team), 0o755) }, says: "ctl/guest"},
		"a hs link":       {setup: func(w *exportWorld) { w.link(hs(w) + "/db/LINK") }, says: "hs/db/LINK"},
		"a backups link":  {setup: func(w *exportWorld) { w.link(paths.BackupDir(w.team) + "/9.tar") }, says: "admin/backups/9.tar"},
		"a linked source": {setup: func(w *exportWorld) { w.relink(paths.ServerConfig(w.team)) }, says: "admin/config.toml"},
		"a fifo": {setup: func(w *exportWorld) {
			if err := syscall.Mkfifo(w.root+hs(w)+"/db/FIFO", 0o600); err != nil {
				w.t.Fatal(err)
			}
		}, says: "hs/db/FIFO"},
		"over its cap":   {setup: func(w *exportWorld) { w.put(paths.ServerConfig(w.team), strings.Repeat("x", 1<<20+1)) }, says: "1048577 bytes"},
		"a token's cap":  {setup: func(w *exportWorld) { w.put(paths.CtlState(w.team)+"/"+AdminTokenName, strings.Repeat("x", 4097)) }, says: "4096 bytes"},
		"the archive":    {setup: func(w *exportWorld) { w.max = 20000 }, says: "20000 bytes"},
		"a ctl stray":    {setup: func(w *exportWorld) { w.put(paths.CtlState(w.team)+"/roster.json.previous", "x") }, says: "ctl/roster.json.previous"},
		"a backup stray": {setup: func(w *exportWorld) { w.put(paths.BackupDir(w.team)+"/8.tar.tmp", "x") }, says: "admin/backups/8.tar.tmp"},
		"a dir in guest": {setup: func(w *exportWorld) { w.put(paths.GuestTokenDir(w.team)+"/sub/x.token", "x") }, says: "ctl/guest/sub"},
		"a bad name":     {setup: func(w *exportWorld) { w.put(hs(w)+"/db/a b", "x") }, says: "hs/db", quotes: "a b"},
		"no ca":          {setup: func(w *exportWorld) { w.remove(paths.CACertFile(w.team)); w.remove(paths.CAKeyFile(w.team)) }, says: "CA"},
		"no roster":      {setup: func(w *exportWorld) { w.remove(paths.RosterFile(w.team)) }, says: "ctl/roster.json"},
		"no approval":    {setup: func(w *exportWorld) { w.remove(paths.ApprovedFile(w.team)) }, says: "status/approved.json"},
	}
}

// SG4, SX3, SX4: the refusals (70), each before anything is written to the carry directory: a
// team not sealed, or sealed to step sealing; a drill or an import record; a source of a mode
// SX3 does not give; a link, or another kind, among the sources; a source over its cap and the
// archive over carry.MaxArchive, naming the sizes in bytes; an entry SX2 does not name, or a
// missing one; a name outside the member grammar, not quoted.
func TestExportRefusals(t *testing.T) {
	for name, c := range exportRefusals() {
		w := newExportWorld(t)
		c.setup(w)
		mark := len(w.calls)
		out, err := w.export()
		switch {
		case !errors.Is(err, ErrRefused) && !errors.Is(err, ErrSealed) && !errors.Is(err, restore.ErrPending):
			t.Errorf("%s: %v, want a refusal", name, err)
		case !strings.Contains(err.Error(), c.says):
			t.Errorf("%s: %q does not say %q", name, err, c.says)
		case c.quotes != "" && strings.Contains(err.Error(), c.quotes):
			t.Errorf("%s: %q quotes a name outside the grammar", name, err)
		case out != nil:
			t.Errorf("%s: answered %s", name, out)
		}
		if w.has(paths.CarryExport(w.team)) || w.has(paths.CarryPart(w.team, false)) || len(w.mutations(mark)) != 0 {
			t.Errorf("%s: something was written: %v", name, w.mutations(mark))
		}
	}
}

func (w *exportWorld) remove(path string) {
	w.t.Helper()
	if err := os.Remove(w.root + path); err != nil {
		w.t.Fatal(err)
	}
}

func (w *exportWorld) chmod(path string, mode fs.FileMode) {
	w.t.Helper()
	if err := os.Chmod(w.root+path, mode); err != nil {
		w.t.Fatal(err)
	}
}

// link makes path a link to the team's roster.
func (w *exportWorld) link(path string) {
	w.t.Helper()
	if err := os.Symlink(w.root+paths.RosterFile(w.team), w.root+path); err != nil {
		w.t.Fatal(err)
	}
}

// relink replaces path by a link to the same file under another name.
func (w *exportWorld) relink(path string) {
	w.t.Helper()
	if err := os.Rename(w.root+path, w.root+path+".real"); err != nil {
		w.t.Fatal(err)
	}
	if err := os.Symlink(w.root+path+".real", w.root+path); err != nil {
		w.t.Fatal(err)
	}
}

// sc-D20: SC1's admission needs the export's files; a wiring without them fails, never passes.
func TestAdmissibleNeedsTheFiles(t *testing.T) {
	if err := Admissible(nil, sampleTeam(t)); err == nil || errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want a failure that is not a refusal", err)
	}
}

// CN5: the archive cap's setting is read first; its failure is the export's, unchanged (the
// verb's 78), and a cap that is not positive is refused naming the setting; nothing is written.
func TestExportNeedsTheArchiveCap(t *testing.T) {
	w := newExportWorld(t)
	w.maxErr = errors.New("the setting is absent")
	if _, err := w.export(); !errors.Is(err, w.maxErr) {
		t.Fatalf("got %v", err)
	}
	w.maxErr = nil
	for _, bad := range []int64{0, -1} {
		w.max = bad
		if _, err := w.export(); err == nil || !strings.Contains(err.Error(), paths.CarryMaxArchiveVar) {
			t.Errorf("a cap of %d: %v", bad, err)
		}
	}
	if w.has(paths.CarryExport(w.team)) || w.has(paths.CarryPart(w.team, false)) || len(w.opens) != 0 {
		t.Errorf("something was read or written: %v", w.opens)
	}
}

// SX1: the carry directory is made 0700 when missing, converged to 0700 when looser, and a link
// in its place is refused.
func TestExportMakesTheCarryDirectory(t *testing.T) {
	w := newExportWorld(t)
	if err := os.Remove(w.root + paths.CarryDir); err != nil {
		t.Fatal(err)
	}
	if _, err := w.export(); err != nil {
		t.Fatal(err)
	}
	w.chmod(paths.CarryDir, 0o755)
	if _, err := w.export(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(w.root + paths.CarryDir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("%v %v", info, err)
	}
	w = newExportWorld(t)
	if err := os.Remove(w.root + paths.CarryDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(w.root+paths.AdminRunDir, w.root+paths.CarryDir); err != nil {
		t.Fatal(err)
	}
	if _, err := w.export(); err == nil || !strings.Contains(err.Error(), paths.CarryDir) {
		t.Fatalf("a linked carry directory: %v", err)
	}
}

// SL1: the export waits host.PlayWait for the team lock, then fails busy, reading nothing.
func TestExportWaitsForTheLock(t *testing.T) {
	w := newExportWorld(t)
	held, err := teamlock.Take(context.Background(), w.root+paths.AdminRunDir, w.team, "apply", 0, w.clock, teamlock.Unguarded)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.export()
	if rerr := held.Release(); rerr != nil {
		t.Fatal(rerr)
	}
	if !errors.Is(err, teamlock.ErrBusy) || w.clock.slept < host.PlayWait {
		t.Fatalf("err = %v after waiting %v", err, w.clock.slept)
	}
	if len(w.opens) != 0 {
		t.Errorf("an export that did not get the lock read %v", w.opens)
	}
}

// SX5: every source file is opened twice, once for the digest and once for the write, and
// nothing else is opened.
func TestExportReadsEachSourceTwice(t *testing.T) {
	w := newExportWorld(t)
	if _, err := w.export(); err != nil {
		t.Fatal(err)
	}
	files := 0
	for _, m := range w.exportMembers() {
		info, err := os.Lstat(w.root + w.sourceOf(m))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().IsRegular() {
			files++
			if n := w.opens[w.sourceOf(m)]; n != 2 {
				t.Errorf("%s opened %d times", m, n)
			}
		}
	}
	if len(w.opens) != files {
		t.Errorf("opened %d sources, want %d: %v", len(w.opens), files, w.opens)
	}
}
