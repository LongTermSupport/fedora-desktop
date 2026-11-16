package carry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/backup"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// sampleTeam is the team every archive test carries.
func sampleTeam(t *testing.T) paths.TeamID {
	t.Helper()
	return mustParseTeam(t, "ops")
}

func mustParseTeam(t *testing.T, s string) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return team
}

// sampleSeal is a seal of sampleTeam at 1700000000123 ms.
func sampleSeal(t *testing.T) Seal {
	t.Helper()
	version, err := paths.ParseServerVersion("1.4.0")
	if err != nil {
		t.Fatal(err)
	}
	software, err := paths.ParseDigest(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	port, err := paths.ParsePort(8448)
	if err != nil {
		t.Fatal(err)
	}
	return Seal{
		TimeMS: 1700000000123, ServerName: "ops.teamchat.internal", Port: port, Tuwunel: version, Software: software,
		Roster:     RosterFacts{Seq: 12, Rev: 7, RestoreEpoch: 1},
		EpochFloor: EpochFloor{Epoch: 1, MinBackup: 3},
		Tail:       []TailEntry{{Room: "main", EventID: "$abc:ops.teamchat.internal", TsMS: 1699999999000}},
	}
}

// sampleContent is the bytes of each file member of sampleManifest.
var sampleContent = map[string][]byte{
	"admin/team.json":          []byte(`{"team":"ops"}`),
	"admin/tls/ca.crt":         []byte("cert"),
	"ctl/roster.json":          []byte(`{"seq":12}`),
	"status/approved.json":     []byte(`{}`),
	"hs/db/000001.sst":         bytes.Repeat([]byte{7}, 1500),
	"hs/backups/meta/1":        []byte("1"),
	"hs/db/CURRENT":            {},
	"admin/backups/1.tar":      []byte("tar"),
	"ctl/restore-2-lost.jsonl": []byte("{}\n"),
}

// sampleMembers are sampleManifest's members, in archive order.
func sampleMembers() []Member {
	dir := func(name string) Member { return Member{Name: name, Dir: true, Mode: 0o700} }
	file := func(name string, mode fs.FileMode) Member {
		sum := sha256.Sum256(sampleContent[name])
		return Member{Name: name, Mode: mode, Size: int64(len(sampleContent[name])), SHA256: hex.EncodeToString(sum[:])}
	}
	return []Member{
		dir("admin"), file("admin/team.json", 0o600), dir("admin/backups"), file("admin/backups/1.tar", 0o600),
		dir("admin/tls"), file("admin/tls/ca.crt", 0o644),
		dir("ctl"), file("ctl/restore-2-lost.jsonl", 0o600), file("ctl/roster.json", 0o600),
		dir("status"), file("status/approved.json", 0o640),
		dir("hs"), dir("hs/backups"), dir("hs/backups/meta"), file("hs/backups/meta/1", 0o600),
		dir("hs/db"), file("hs/db/000001.sst", 0o600), file("hs/db/CURRENT", 0o600),
	}
}

func sampleManifest(t *testing.T) Manifest {
	t.Helper()
	return Manifest{Format: ExportFormat, Kind: KindSealed, Team: sampleTeam(t), ServerName: "ops.teamchat.internal", Seal: sampleSeal(t), Members: sampleMembers()}
}

// openSample opens a member's sample content.
func openSample(m Member) (io.ReadCloser, error) {
	data, ok := sampleContent[m.Name]
	if !ok {
		return nil, fmt.Errorf("no sample for %s", m.Name)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

const testCap = 1 << 20

func writeWritten(t *testing.T, m Manifest) ([]byte, Written) {
	t.Helper()
	var b bytes.Buffer
	w, err := WriteArchive(&b, m, openSample, testCap)
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes(), w
}

func writeSample(t *testing.T, m Manifest) []byte {
	t.Helper()
	raw, w := writeWritten(t, m)
	if w.Size != int64(len(raw)) {
		t.Fatalf("written size %d, %d bytes", w.Size, len(raw))
	}
	return raw
}

// readResult is what ReadArchive of an input gave the callbacks, and its answer.
type readResult struct {
	manifest *Manifest
	members  []string
	data     map[string][]byte
	read     Read
	err      error
}

func read(t *testing.T, raw []byte) readResult {
	t.Helper()
	c := readResult{data: map[string][]byte{}}
	c.read, c.err = ReadArchive(bytes.NewReader(raw), sampleTeam(t), testCap,
		func(m Manifest) error { c.manifest = &m; return nil },
		func(m Member, body io.Reader) error {
			c.members = append(c.members, m.Name)
			if m.Dir {
				return nil
			}
			data, err := io.ReadAll(body)
			c.data[m.Name] = data
			return err
		})
	return c
}

// SX1, SX5: a written archive reads back member for member, with the manifest first and the
// whole-archive digest and size answered by both sides.
func TestArchiveRoundTrip(t *testing.T) {
	raw, w := writeWritten(t, sampleManifest(t))
	sum := sha256.Sum256(raw)
	if w.SHA256 != hex.EncodeToString(sum[:]) || w.Size != int64(len(raw)) {
		t.Errorf("written %+v, want the digest and size of the bytes", w)
	}
	c := read(t, raw)
	if c.err != nil {
		t.Fatal(c.err)
	}
	if c.read.SHA256 != w.SHA256 || c.read.Size != w.Size {
		t.Errorf("read %+v, written %+v", c.read, w)
	}
	if c.manifest == nil || c.manifest.Seal.TimeMS != 1700000000123 || len(c.manifest.Members) != len(sampleMembers()) {
		t.Fatalf("manifest: %+v", c.manifest)
	}
	for _, m := range sampleMembers() {
		if !m.Dir && !bytes.Equal(c.data[m.Name], sampleContent[m.Name]) {
			t.Errorf("%s read back %d bytes, not its content", m.Name, len(c.data[m.Name]))
		}
	}
	if strings.Join(c.members, " ") != strings.Join(names(sampleMembers()), " ") {
		t.Errorf("visited %q", c.members)
	}
}

func names(ms []Member) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}

// SX1: two archives of one state are byte-identical, every header root's with the seal's time.
func TestArchiveIsDeterministic(t *testing.T) {
	a := writeSample(t, sampleManifest(t))
	if !bytes.Equal(a, writeSample(t, sampleManifest(t))) {
		t.Fatal("two archives of one state differ")
	}
	for off := 0; off+block <= len(a); {
		h := a[off : off+block]
		if allZero(h) {
			break
		}
		if got := mustOctal(t, h[136:148]); got != 1700000000 {
			t.Errorf("header at %d has mtime %d", off, got)
		}
		if mustOctal(t, h[108:116]) != 0 || mustOctal(t, h[116:124]) != 0 || !allZero(h[265:329]) {
			t.Errorf("header at %d is not root's with no names", off)
		}
		size := mustOctal(t, h[124:136])
		off += block + int(size+padding(size))
	}
}

func mustOctal(t *testing.T, f []byte) int64 {
	t.Helper()
	v, err := strconv.ParseInt(strings.TrimRight(string(f), "\x00 "), 8, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// SX5's second pass: a source that changed after the manifest was made fails the write.
func TestWriteFailsWhenASourceChanged(t *testing.T) {
	m := sampleManifest(t)
	changed := func(mb Member) (io.ReadCloser, error) {
		if mb.Name == "ctl/roster.json" {
			return io.NopCloser(strings.NewReader(`{"seq":13}`)), nil
		}
		return openSample(mb)
	}
	w, err := WriteArchive(io.Discard, m, changed, testCap)
	if err == nil || errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "ctl/roster.json") || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("got %+v %v, want a failure naming the changed source", w, err)
	}
	longer := func(mb Member) (io.ReadCloser, error) {
		if mb.Name == "admin/team.json" {
			return io.NopCloser(strings.NewReader(`{"team":"ops","x":1}`)), nil
		}
		return openSample(mb)
	}
	if w, err := WriteArchive(io.Discard, m, longer, testCap); err == nil || !strings.Contains(err.Error(), "admin/team.json") {
		t.Fatalf("a longer source: %+v %v", w, err)
	}
}

// SX4, CN5: an archive over the cap is refused before anything is written, naming the size and
// the cap in bytes.
func TestWriteRefusesAnArchiveOverTheCap(t *testing.T) {
	var b bytes.Buffer
	w, err := WriteArchive(&b, sampleManifest(t), openSample, 4096)
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "4096 bytes") || b.Len() != 0 {
		t.Fatalf("got %+v %v and %d bytes, want a refusal naming the cap and nothing written", w, err, b.Len())
	}
}

// SX4: a manifest the writer would make unreadable is refused: bad names, a file before its
// directory, a duplicate, a mode SX3 does not give.
func TestWriteRefusesABadManifest(t *testing.T) {
	for name, edit := range map[string]func(*Manifest){
		"dot dot":         func(m *Manifest) { m.Members[1].Name = "admin/../x" },
		"absolute":        func(m *Manifest) { m.Members[1].Name = "/admin/team.json" },
		"dot":             func(m *Manifest) { m.Members[1].Name = "admin/." },
		"space":           func(m *Manifest) { m.Members[1].Name = "admin/a b" },
		"no parent":       func(m *Manifest) { m.Members = m.Members[1:] },
		"duplicate":       func(m *Manifest) { m.Members = append(m.Members, m.Members[1]) },
		"setuid":          func(m *Manifest) { m.Members[1].Mode = 0o600 | fs.ModeSetuid },
		"group writable":  func(m *Manifest) { m.Members[1].Mode = 0o620 },
		"open directory":  func(m *Manifest) { m.Members[0].Mode = 0o755 },
		"unknown root":    func(m *Manifest) { m.Members[0].Name = "etc" },
		"no members":      func(m *Manifest) { m.Members = nil },
		"wrong format":    func(m *Manifest) { m.Format = 2 },
		"too long a name": func(m *Manifest) { m.Members[1].Name = "admin/" + strings.Repeat("a", 129) },
		"no tail":         func(m *Manifest) { m.Seal.Tail = nil },
	} {
		m := sampleManifest(t)
		m.Members = append([]Member{}, m.Members...)
		edit(&m)
		if w, err := WriteArchive(io.Discard, m, openSample, testCap); err == nil {
			t.Errorf("%s: written %+v", name, w)
		}
	}
}

// headerAt is the offset of member i's header (0 is the manifest) in raw.
func headerAt(t *testing.T, raw []byte, i int) int {
	t.Helper()
	off := 0
	for range i {
		size := mustOctal(t, raw[off+124:off+136])
		off += block + int(size+padding(size))
	}
	return off
}

// rewrite edits member i's header in a copy of raw and fixes its checksum.
func rewrite(t *testing.T, raw []byte, i int, edit func(h []byte)) []byte {
	t.Helper()
	out := append([]byte{}, raw...)
	off := headerAt(t, out, i)
	h := out[off : off+block]
	edit(h)
	copy(h[148:156], octal(checksum(h), 6)+"\x00 ")
	return out
}

func setOctal(h []byte, lo, hi int, v int64) {
	copy(h[lo:hi], octal(v, hi-lo-1)+"\x00")
}

// hostileArchives are every hostile variant of raw that BK3, generalised, refuses (SI8).
func hostileArchives(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	const fileAt = 2 // admin/team.json
	flip := func(at int) []byte {
		out := append([]byte{}, raw...)
		out[at] ^= 1
		return out
	}
	return map[string][]byte{
		"a hard link":           rewrite(t, raw, fileAt, func(h []byte) { h[156] = '1' }),
		"a symlink":             rewrite(t, raw, fileAt, func(h []byte) { h[156] = '2'; copy(h[157:], "/etc/shadow") }),
		"a character device":    rewrite(t, raw, fileAt, func(h []byte) { h[156] = '3' }),
		"a block device":        rewrite(t, raw, fileAt, func(h []byte) { h[156] = '4' }),
		"a FIFO":                rewrite(t, raw, fileAt, func(h []byte) { h[156] = '6' }),
		"a PAX header":          rewrite(t, raw, fileAt, func(h []byte) { h[156] = 'x' }),
		"a global PAX":          rewrite(t, raw, fileAt, func(h []byte) { h[156] = 'g' }),
		"a GNU long name":       rewrite(t, raw, fileAt, func(h []byte) { h[156] = 'L' }),
		"dot dot":               rewrite(t, raw, fileAt, func(h []byte) { copy(h[0:100], "admin/../team.json\x00") }),
		"an absolute name":      rewrite(t, raw, fileAt, func(h []byte) { copy(h[0:100], "/admin/team.json") }),
		"setuid":                rewrite(t, raw, fileAt, func(h []byte) { setOctal(h, 100, 108, 0o4600) }),
		"a wrong mode":          rewrite(t, raw, fileAt, func(h []byte) { setOctal(h, 100, 108, 0o644) }),
		"an owner":              rewrite(t, raw, fileAt, func(h []byte) { setOctal(h, 108, 116, 1000) }),
		"a group":               rewrite(t, raw, fileAt, func(h []byte) { setOctal(h, 116, 124, 1000) }),
		"a user name":           rewrite(t, raw, fileAt, func(h []byte) { copy(h[265:], "root") }),
		"another time":          rewrite(t, raw, fileAt, func(h []byte) { setOctal(h, 136, 148, 1) }),
		"a GNU magic":           rewrite(t, raw, fileAt, func(h []byte) { copy(h[257:265], "ustar  \x00") }),
		"a directory as a file": rewrite(t, raw, 1, func(h []byte) { h[156] = '0' }),
		"a file as a directory": rewrite(t, raw, fileAt, func(h []byte) { h[156] = '5' }),
		"a size not listed":     rewrite(t, raw, fileAt, func(h []byte) { setOctal(h, 124, 136, 3) }),
		"a wrong checksum":      flip(headerAt(t, raw, fileAt) + 10),
		"padding not zero":      flip(headerAt(t, raw, fileAt) + block + len(sampleContent["admin/team.json"])),
		"data after the end":    append(append([]byte{}, raw...), bytes.Repeat([]byte{1}, block)...),
		"zeros after the end":   append(append([]byte{}, raw...), make([]byte, block)...),
		"no end blocks":         raw[:len(raw)-2*block],
		"cut short":             raw[:headerAt(t, raw, 17)+block+100],
		"an empty input":        {},
		"a reordered member":    swapMembers(t, raw, 2, 3),
		"a missing member":      dropMember(t, raw, 18),
		"an extra member":       extraMember(t, raw),
	}
}

// BK3 generalised (SI8): every hostile archive is refused (70), naming no content.
func TestReadRefusesHostileArchives(t *testing.T) {
	for name, input := range hostileArchives(t, writeSample(t, sampleManifest(t))) {
		c := read(t, input)
		if !errors.Is(c.err, ErrRefused) {
			t.Errorf("%s: %v, want a refusal", name, c.err)
			continue
		}
		if strings.Contains(c.err.Error(), "shadow") || strings.Contains(c.err.Error(), `"team":"ops"`) {
			t.Errorf("%s: the error quotes the archive: %v", name, c.err)
		}
	}
}

// swapMembers swaps the members i and j (both headers with their data) of raw.
func swapMembers(t *testing.T, raw []byte, i, j int) []byte {
	t.Helper()
	parts := splitMembers(t, raw)
	parts[i], parts[j] = parts[j], parts[i]
	return bytes.Join(parts, nil)
}

func dropMember(t *testing.T, raw []byte, i int) []byte {
	t.Helper()
	parts := splitMembers(t, raw)
	return bytes.Join(append(parts[:i:i], parts[i+1:]...), nil)
}

func extraMember(t *testing.T, raw []byte) []byte {
	t.Helper()
	parts := splitMembers(t, raw)
	last := len(parts) - 1
	extra := append([]byte{}, parts[last-1]...)
	return bytes.Join(append(parts[:last:last], extra, parts[last]), nil)
}

// splitMembers is raw as each member's header and data, then the end blocks.
func splitMembers(t *testing.T, raw []byte) [][]byte {
	t.Helper()
	var parts [][]byte
	off := 0
	for !allZero(raw[off : off+block]) {
		size := mustOctal(t, raw[off+124:off+136])
		end := off + block + int(size+padding(size))
		parts = append(parts, raw[off:end])
		off = end
	}
	return append(parts, raw[off:])
}

// SI8: a member whose bytes are not the manifest's digest fails the read (1, not a refusal),
// naming the member.
func TestReadFailsOnADigestMismatch(t *testing.T) {
	out := writeSample(t, sampleManifest(t))
	out[headerAt(t, out, 2)+block] ^= 1
	c := read(t, out)
	if c.err == nil || errors.Is(c.err, ErrRefused) || !strings.Contains(c.err.Error(), "admin/team.json") {
		t.Fatalf("got %v, want a failure naming admin/team.json", c.err)
	}
}

// SX4, CN5: an input over the cap is refused, naming the cap in bytes.
func TestReadRefusesAnArchiveOverTheCap(t *testing.T) {
	raw := writeSample(t, sampleManifest(t))
	r, err := ReadArchive(bytes.NewReader(raw), sampleTeam(t), int64(len(raw))-1,
		func(Manifest) error { return nil }, func(Member, io.Reader) error { return nil })
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), strconv.Itoa(len(raw)-1)+" bytes") {
		t.Fatalf("got %+v %v", r, err)
	}
}

// SI5: a nightly backup is restore's input, never a carry's.
func TestReadRefusesABackupTar(t *testing.T) {
	tar, err := backup.Encode(backup.Manifest{Team: sampleTeam(t), Backup: 1, TimeMS: 1700000000000, ServerName: "ops.teamchat.internal"},
		backup.Sources{TeamJSON: []byte("{}"), Config: []byte("x"), Roster: []byte("{}"), Rooms: []byte("{}"), Token: []byte("t")})
	if err != nil {
		t.Fatal(err)
	}
	c := read(t, tar)
	if !errors.Is(c.err, ErrRefused) || !strings.Contains(c.err.Error(), "a nightly backup is restore's input (DC8), not a carry") || c.manifest != nil {
		t.Fatalf("got %v", c.err)
	}
}

// SI5: a first member that is neither the manifest nor a backup's is refused.
func TestReadRefusesAnotherFirstMember(t *testing.T) {
	out := rewrite(t, writeSample(t, sampleManifest(t)), 0, func(h []byte) { copy(h[0:100], "admin\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00") })
	if c := read(t, out); !errors.Is(c.err, ErrRefused) || !strings.Contains(c.err.Error(), "carry.json") {
		t.Fatalf("got %v", c.err)
	}
}

// SI5: a manifest of another team is refused.
func TestReadRefusesAnotherTeam(t *testing.T) {
	m := sampleManifest(t)
	m.Team = mustParseTeam(t, "dev")
	c := read(t, writeSample(t, m))
	if !errors.Is(c.err, ErrRefused) || c.manifest != nil {
		t.Fatalf("got %v", c.err)
	}
}

// formatManifest is sampleManifest's encoding with its format member replaced by value.
func formatManifest(t *testing.T, value string) []byte {
	t.Helper()
	enc := EncodeManifest(sampleManifest(t))
	out := bytes.Replace(enc, []byte(`"format":1`), []byte(value), 1)
	if bytes.Equal(out, enc) {
		t.Fatal("the manifest's format was not replaced")
	}
	return out
}

// archiveWithManifest is a sample archive whose manifest member is manifest.
func archiveWithManifest(t *testing.T, manifest []byte) []byte {
	t.Helper()
	parts := splitMembers(t, writeSample(t, sampleManifest(t)))
	h := append([]byte{}, parts[0][:block]...)
	setOctal(h, 124, 136, int64(len(manifest)))
	copy(h[148:156], octal(checksum(h), 6)+"\x00 ")
	first := append(append(h, manifest...), make([]byte, padding(int64(len(manifest))))...)
	return bytes.Join(append([][]byte{first}, parts[1:]...), nil)
}

// SX8, SI13: an export of a format this importer does not know is refused (70) naming both
// versions, before the manifest is judged otherwise and before anything is staged.
func TestImportRefusesUnknownFormat(t *testing.T) {
	for name, c := range map[string]struct{ value, says string }{
		"format 0":       {`"format":0`, "the export is format 0, this teamchat-admin imports format 1"},
		"format 2":       {`"format":2`, "the export is format 2, this teamchat-admin imports format 1"},
		"a string":       {`"format":"1"`, "this teamchat-admin imports format 1"},
		"a fraction":     {`"format":1.5`, "this teamchat-admin imports format 1"},
		"absent":         {`"formats":1`, "this teamchat-admin imports format 1"},
		"format 2, more": {`"format":2,"extra":true`, "the export is format 2, this teamchat-admin imports format 1"},
	} {
		got := read(t, archiveWithManifest(t, formatManifest(t, c.value)))
		if !errors.Is(got.err, ErrRefused) || !strings.Contains(got.err.Error(), c.says) {
			t.Errorf("%s: %v, want a refusal saying %q", name, got.err, c.says)
		}
		if got.manifest != nil || len(got.members) != 0 {
			t.Errorf("%s: the manifest or a member was passed on", name)
		}
	}
}

// SI5: a manifest that does not decode strictly, once its format passed, is refused.
func TestReadRefusesAManifestThatIsNotStrict(t *testing.T) {
	for name, manifest := range map[string][]byte{
		"an unknown key":  formatManifest(t, `"format":1,"extra":true`),
		"a duplicate key": formatManifest(t, `"format":1,"format":1`),
		"not JSON":        []byte(`{"format":1,`),
	} {
		if c := read(t, archiveWithManifest(t, manifest)); !errors.Is(c.err, ErrRefused) || c.manifest != nil {
			t.Errorf("%s: %v", name, c.err)
		}
	}
}

// The manifest round-trips strictly, and its seal's time key is the kind's.
func TestManifestRoundTrip(t *testing.T) {
	for _, kind := range []Kind{KindSealed, KindDrill} {
		m := sampleManifest(t)
		m.Kind = kind
		raw := EncodeManifest(m)
		key := map[Kind]string{KindSealed: `"sealed_ms":`, KindDrill: `"drilled_ms":`}[kind]
		if !bytes.Contains(raw, []byte(key)) {
			t.Errorf("%s: %s has no %s", kind, raw, key)
		}
		got, err := DecodeManifest(raw, sampleTeam(t))
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if !bytes.Equal(EncodeManifest(got), raw) {
			t.Errorf("%s: the decoded manifest encodes differently", kind)
		}
	}
}
