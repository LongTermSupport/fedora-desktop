package carry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/backup"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
)

// importToken is the carried admin token: OP2's grammar, which the export world's does not hold.
const importToken = "the-admin-token"

// importWorld is an empty guest for team ops: the base units and the Tuwunel binary installed,
// the carry directory made, and an archive in the inbox.
type importWorld struct {
	*sealWorld

	flags ImportFlags
}

// carriedArchive is an export of kind of the export world's team, every SI6 member good; change
// alters a source after the seal took its facts (or before a drill's), so the archive disagrees.
func carriedArchive(t *testing.T, kind Kind, change func(*exportWorld)) []byte {
	t.Helper()
	src := newSourcesWorld(t)
	src.put(paths.CtlState(src.team)+"/"+AdminTokenName, importToken)
	src.putMode(paths.ApprovedFile(src.team), string(declare.EncodeApproved(src.team, sampleInput(t).Gated())), 0o640)
	if kind == KindDrill {
		if change != nil {
			change(src)
		}
		dw := &drillWorld{exportWorld: src}
		if _, err := dw.drill(); err != nil {
			t.Fatal(err)
		}
		return src.archiveAt(paths.CarryDrillExport(src.team))
	}
	if _, err := src.seal(); err != nil {
		t.Fatal(err)
	}
	if change != nil {
		change(src)
	}
	if _, err := src.export(); err != nil {
		t.Fatal(err)
	}
	return src.archive()
}

// newGuest is an empty guest for team ops at an hour past the seal.
func newGuest(t *testing.T) *importWorld {
	t.Helper()
	team := sampleTeam(t)
	g := &sealWorld{
		t: t, team: team, root: t.TempDir(), uid: ownUID(t), clock: &sealClock{now: time.UnixMilli(sealMS).Add(time.Hour)},
		units: map[string]*sealUnit{}, decl: sampleInput(t).Team,
	}
	for _, dir := range []string{paths.CarryDir, paths.AdminRunDir, paths.AdminStateRoot} {
		if err := os.MkdirAll(g.root+dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	g.put(paths.ServerUnitTemplateFile, "[Unit]\n")
	g.put(paths.TLSUnitTemplateFile, "[Unit]\n")
	g.put(paths.ServerBinary(mustVersion(t, "1.9.4")), "tuwunel")
	return &importWorld{sealWorld: g}
}

// newImportWorld is an empty guest with an archive of kind in its inbox, and the flag the kind
// takes.
func newImportWorld(t *testing.T, kind Kind, change func(*exportWorld)) *importWorld {
	t.Helper()
	w := newGuest(t)
	w.flags.Drill = kind == KindDrill
	w.inbox(carriedArchive(t, kind, change))
	return w
}

// inbox places raw as the archive the import reads, 0600, the flag deciding which.
func (w *importWorld) inbox(raw []byte) {
	w.t.Helper()
	path := paths.CarryExport(w.team)
	if w.flags.Drill {
		path = paths.CarryDrillExport(w.team)
	}
	w.put(path, string(raw))
}

func (w *importWorld) importDeps() Deps {
	d := w.deps()
	d.Files = OS{Root: w.root, StateOwnerUID: w.uid}
	d.MaxArchive = func() (int64, error) { return exportMax, nil }
	return d
}

func (w *importWorld) validate() error {
	return newImporting(w.importDeps(), w.team, w.flags, exportMax).validate(context.Background())
}

// tree is every path under the guest's root with its mode and size, so a test can show that
// nothing was written.
func (w *importWorld) tree() []string {
	w.t.Helper()
	var out []string
	err := filepath.WalkDir(w.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, p+" "+info.Mode().String()+" "+strconv.FormatInt(info.Size(), 10))
		return nil
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return out
}

// SI1-SI7, SI13: a sealed archive without --drill and a drill archive with it pass validation
// on an empty guest that can serve them, and validation writes nothing.
func TestImportValidationPassesAGoodArchive(t *testing.T) {
	for _, kind := range []Kind{KindSealed, KindDrill} {
		w := newImportWorld(t, kind, nil)
		before := w.tree()
		if err := w.validate(); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if after := w.tree(); !slices.Equal(before, after) {
			t.Fatalf("%s: validation changed the guest:\n%v\n%v", kind, before, after)
		}
	}
}

// importRefusal is one refusal of SI2-SI7 or SI13: the archive's kind, a change to its sources,
// a change to the guest, and what the refusal says.
type importRefusal struct {
	kind   Kind
	change func(*exportWorld)
	guest  func(*importWorld)
	says   string
}

func otherTeam(t *testing.T) paths.TeamID {
	t.Helper()
	return mustParseTeam(t, "dev")
}

// otherUnits is units.json of team dev recording port.
func otherUnits(port int) string {
	return strings.Replace(unitsJSON(port), `"team":"ops"`, `"team":"dev"`, 1)
}

func emptyGuestRefusals(t *testing.T) map[string]importRefusal {
	t.Helper()
	team := sampleTeam(t)
	out := map[string]importRefusal{}
	for _, root := range paths.TeamStateRoots(team) {
		out["SI2 "+root] = importRefusal{guest: func(w *importWorld) {
			if err := os.MkdirAll(w.root+root, 0o700); err != nil {
				t.Fatal(err)
			}
		}, says: root}
	}
	out["SI2 a link"] = importRefusal{guest: func(w *importWorld) {
		if err := os.Symlink(w.root+paths.CarryDir, w.root+paths.AdminState(team)); err != nil {
			t.Fatal(err)
		}
	}, says: paths.AdminState(team)}
	unit := func(name, active string) func(*importWorld) {
		return func(w *importWorld) { w.units[name] = &sealUnit{active: active, loaded: true} }
	}
	out["SI2 the server active"] = importRefusal{guest: unit(paths.ServerUnit(team), "active"), says: paths.ServerUnit(team)}
	out["SI2 the terminator activating"] = importRefusal{guest: unit(paths.TLSUnit(team), "activating"), says: paths.TLSUnit(team)}
	out["SI2 the restore unit failed"] = importRefusal{guest: unit(paths.RestoreUnit(team), "failed"), says: paths.RestoreUnit(team)}
	return out
}

func serveRefusals(t *testing.T) map[string]importRefusal {
	t.Helper()
	binary := paths.ServerBinary(mustVersion(t, "1.9.4"))
	gone := func(path string) func(*importWorld) {
		return func(w *importWorld) {
			if err := os.Remove(w.root + path); err != nil {
				t.Fatal(err)
			}
		}
	}
	return map[string]importRefusal{
		"SI3 no server template": {guest: gone(paths.ServerUnitTemplateFile), says: paths.ServerUnitTemplateFile},
		"SI3 no TLS template":    {guest: gone(paths.TLSUnitTemplateFile), says: paths.TLSUnitTemplateFile},
		"SI3 no binary":          {guest: gone(binary), says: binary},
		"SI3 only another version's binary": {guest: func(w *importWorld) {
			gone(binary)(w)
			w.put(paths.ServerBinary(mustVersion(t, "1.9.5")), "tuwunel")
		}, says: binary},
		"SI3 a binary that is a directory": {guest: func(w *importWorld) {
			gone(binary)(w)
			if err := os.Mkdir(w.root+binary, 0o700); err != nil {
				t.Fatal(err)
			}
		}, says: binary},
		"SI3 the port recorded by another team": {guest: func(w *importWorld) {
			w.put(paths.UnitsRecord(otherTeam(t)), otherUnits(8448))
		}, says: "port 8448"},
	}
}

func kindRefusals(t *testing.T) map[string]importRefusal {
	t.Helper()
	dev := otherTeam(t)
	return map[string]importRefusal{
		"SI4 a sealed archive with --drill": {kind: KindSealed, guest: func(w *importWorld) {
			w.flags.Drill = true
			w.inbox(carriedArchive(t, KindSealed, nil))
		}, says: "--drill"},
		"SI4 a drill archive without --drill": {kind: KindDrill, guest: func(w *importWorld) {
			w.flags.Drill = false
			w.inbox(carriedArchive(t, KindDrill, nil))
		}, says: "--drill"},
		"SI4 a drill onto a guest with a stamped team": {kind: KindDrill, guest: func(w *importWorld) {
			decl := sampleInput(t).Team
			decl.ID, decl.ServerName = dev, paths.ServerName(dev)
			w.put(paths.TeamDeclaration(dev), string(decl.Canonical()))
			w.put(paths.StampFile(dev), `{"v":1,"team":"dev","config":"c","software":"s","tuwunel":"1.9.4","applied_ms":1}`)
		}, says: "team dev"},
		"SI4 a drill onto a guest with a team that is no drill import": {kind: KindDrill, guest: func(w *importWorld) {
			w.put(paths.UnitsRecord(dev), otherUnits(8449))
		}, says: paths.AdminState(dev)},
		"SI4 a drill onto a guest with a sealed import": {kind: KindDrill, guest: func(w *importWorld) {
			w.put(paths.CarriedRecord(dev), string(EncodeCarriedRecord(CarriedRecord{
				Team: dev, Kind: KindSealed, SHA256: strings.Repeat("a", 64), SealedMS: 1, ImportedMS: 2,
			})))
		}, says: paths.AdminState(dev)},
	}
}

func archiveRefusals(t *testing.T) map[string]importRefusal {
	t.Helper()
	nightly, err := backup.Encode(backup.Manifest{Team: sampleTeam(t), Backup: 1, TimeMS: sealMS, ServerName: "ops.teamchat.internal"},
		backup.Sources{TeamJSON: []byte("{}"), Config: []byte("x"), Roster: []byte("{}"), Rooms: []byte("{}"), Token: []byte("t")})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]importRefusal{
		"SI5 a nightly backup": {guest: func(w *importWorld) { w.inbox(nightly) }, says: "a nightly backup is restore's input"},
		"SI13 format 2": {guest: func(w *importWorld) {
			w.inbox(archiveWithManifest(t, formatManifest(t, `"format":2`)))
		}, says: "the export is format 2, this teamchat-admin imports format 1"},
	}
}

func agreeRefusals(t *testing.T) map[string]importRefusal {
	t.Helper()
	other, err := pki.Maker{Clock: clock.NewFake(0, sealMS)}.NewCA("another CA")
	if err != nil {
		t.Fatal(err)
	}
	roster := func(change func(*apply.RosterFile)) func(*exportWorld) {
		return func(src *exportWorld) { src.putRoster(change) }
	}
	return map[string]importRefusal{
		"SI6 the roster's seq": {change: roster(func(r *apply.RosterFile) { r.Seq = 21 }), says: "ctl/roster.json"},
		"SI6 a pending roster": {change: roster(func(r *apply.RosterFile) {
			r.Pending = &apply.Pending{Op: apply.OpMemberRetire, Step: "rooms", SinceMS: 1, Handle: "web.1+desk.podman"}
		}), says: "ctl/roster.json"},
		"SI6 the epoch floor": {change: func(src *exportWorld) {
			src.put(paths.RestoreEpochFile(src.team), string(restore.EncodeEpochFloor(restore.EpochFloor{Epoch: 3, MinBackup: 9})))
		}, says: "admin/restore-epoch.json"},
		"SI6 no epoch floor": {change: func(src *exportWorld) { src.remove(paths.RestoreEpochFile(src.team)) }, says: "admin/restore-epoch.json"},
		"SI6 team.json's server name": {change: func(src *exportWorld) {
			decl := src.decl
			decl.ServerName = "other.example.org"
			src.put(paths.TeamDeclaration(src.team), string(decl.Canonical()))
		}, says: "admin/team.json"},
		"SI6 server-name":       {change: func(src *exportWorld) { src.put(paths.ServerNameFile(src.team), "other.example.org") }, says: "admin/server-name"},
		"SI6 units.json's port": {change: func(src *exportWorld) { src.put(paths.UnitsRecord(src.team), unitsJSON(8449)) }, says: "admin/units.json"},
		"SI6 units.json's version": {change: func(src *exportWorld) {
			src.put(paths.UnitsRecord(src.team), strings.Replace(unitsJSON(8448), "1.9.4", "1.9.5", 1))
		}, says: "admin/units.json"},
		"SI6 a CA key of another pair": {guest: func(w *importWorld) {
			w.inbox(tampered(t, carriedArchive(t, KindSealed, nil), func(data map[string][]byte) {
				data[memberCAKey] = other.KeyPEM()
			}))
		}, says: "admin/tls"},
		"SI6 no admin token": {guest: func(w *importWorld) {
			w.inbox(tampered(t, carriedArchive(t, KindSealed, nil), func(data map[string][]byte) { delete(data, memberToken) }))
		}, says: "the archive holds no " + memberToken},
		"SI6 approved.json": {change: func(src *exportWorld) {
			src.putMode(paths.ApprovedFile(src.team), `{"approved":{}}`, 0o640)
		}, says: "status/approved.json"},
		"SI6 admin.token": {change: func(src *exportWorld) {
			src.put(paths.CtlState(src.team)+"/"+AdminTokenName, "a token with spaces")
		}, says: "ctl/admin.token"},
	}
}

// tampered is raw written again with each file member's bytes edit leaves, the manifest's sizes
// and digests made to agree: an archive no export of a world would write.
func tampered(t *testing.T, raw []byte, edit func(data map[string][]byte)) []byte {
	t.Helper()
	m, names, data, rd := readExport(t, raw, sampleTeam(t))
	if len(names) == 0 || rd.Size == 0 {
		t.Fatal("the archive to tamper with is empty")
	}
	edit(data)
	kept := m.Members[:0]
	for _, mb := range m.Members {
		if !mb.Dir {
			b, ok := data[mb.Name]
			if !ok {
				continue
			}
			sum := sha256.Sum256(b)
			mb.Size, mb.SHA256 = int64(len(b)), hex.EncodeToString(sum[:])
		}
		kept = append(kept, mb)
	}
	m.Members = kept
	var out bytes.Buffer
	if _, err := WriteArchive(&out, m, func(mb Member) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data[mb.Name])), nil
	}, exportMax); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func ageRefusals() map[string]importRefusal {
	at := func(d time.Duration) func(*importWorld) {
		return func(w *importWorld) { w.clock.now = time.UnixMilli(sealMS).Add(d) }
	}
	return map[string]importRefusal{
		"SI7 a sealed export past 72h":  {guest: at(MaxAge + time.Millisecond), says: "over the limit of 72h0m0s"},
		"SI7 a drill export past 24h":   {kind: KindDrill, guest: at(MaxAgeDrill + time.Millisecond), says: "over the limit of 24h0m0s"},
		"SI7 a seal ahead of the clock": {guest: at(-maxAhead - time.Millisecond), says: "ahead of this guest's clock"},
	}
}

// SI2-SI7, SI13: each refusal is ErrRefused (70), names what refuses, and writes nothing.
func TestImportRefusals(t *testing.T) {
	rows := map[string]importRefusal{}
	for _, set := range []map[string]importRefusal{
		emptyGuestRefusals(t), serveRefusals(t), kindRefusals(t), archiveRefusals(t), agreeRefusals(t), ageRefusals(),
	} {
		maps.Copy(rows, set)
	}
	for name, c := range rows {
		t.Run(name, func(t *testing.T) {
			kind := c.kind
			if kind == "" {
				kind = KindSealed
			}
			w := newImportWorld(t, kind, c.change)
			if c.guest != nil {
				c.guest(w)
			}
			before := w.tree()
			err := w.validate()
			if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), c.says) {
				t.Fatalf("got %v, want a refusal saying %q", err, c.says)
			}
			if after := w.tree(); !slices.Equal(before, after) {
				t.Fatalf("the refusal changed the guest:\n%v\n%v", before, after)
			}
		})
	}
}

// SI7: the age limits are inclusive, and a drill onto a guest whose only other team is a drill
// import passes SI4.
func TestImportValidationEdges(t *testing.T) {
	w := newImportWorld(t, KindSealed, nil)
	w.clock.now = time.UnixMilli(sealMS).Add(MaxAge)
	if err := w.validate(); err != nil {
		t.Fatalf("an export exactly %s old: %v", MaxAge, err)
	}
	w.clock.now = time.UnixMilli(sealMS).Add(maxAhead)
	if err := w.validate(); err != nil {
		t.Fatalf("a seal exactly %s ahead: %v", maxAhead, err)
	}
	w = newImportWorld(t, KindDrill, nil)
	dev := otherTeam(t)
	w.put(paths.CarriedRecord(dev), string(EncodeCarriedRecord(CarriedRecord{
		Team: dev, Kind: KindDrill, SHA256: strings.Repeat("a", 64), SealedMS: 1, ImportedMS: 2,
	})))
	if err := w.validate(); err != nil {
		t.Fatalf("a drill beside a drill import: %v", err)
	}
}

// SI1: no archive in the inbox, or a link in its place, fails before anything is judged.
func TestImportNeedsTheInboxArchive(t *testing.T) {
	w := newGuest(t)
	if err := w.validate(); err == nil || !strings.Contains(err.Error(), paths.CarryExport(w.team)) {
		t.Fatalf("no archive: %v", err)
	}
	w = newImportWorld(t, KindSealed, nil)
	export := w.root + paths.CarryExport(w.team)
	if err := os.Rename(export, export+".real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(export+".real", export); err != nil {
		t.Fatal(err)
	}
	if err := w.validate(); err == nil || !strings.Contains(err.Error(), paths.CarryExport(w.team)) {
		t.Fatalf("an archive that is a link: %v", err)
	}
}
