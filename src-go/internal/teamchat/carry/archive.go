// Package carry is the sealed carry of a team (Plan 00209 SPEC-carry, issue #324): its archive,
// one deterministic uncompressed ustar with a digest manifest first, written and streamed back
// under the trust rule; the seal, drill and import records; and the verbs that seal a team,
// export it, and import it onto an empty guest, all beside restore and taking nothing from it.
package carry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// The ustar framing of SX1, as backup's BK1 has it, generalised to directories and to members of
// any size: 512-byte blocks, plain ustar headers, two zero blocks at the end.
const (
	block        = 512
	typeRegular  = '0'
	typeDir      = '5'
	ustarMagic   = "ustar\x00"
	ustarVersion = "00"
)

// The nightly backup's first member, refused by name (SI5).
const backupManifestName = "backup.json"

// Written is an archive written: its size and whole-archive digest (SX6).
type Written struct {
	Size   int64
	SHA256 string
}

// Read is an archive read whole: its size and whole-archive digest (SI1).
type Read = Written

// archiveSize is the size of the archive m describes, with a manifest of manifestLen bytes.
func archiveSize(m Manifest, manifestLen int64) int64 {
	size := block + manifestLen + padding(manifestLen) + 2*block
	for _, mb := range m.Members {
		size += block + mb.Size + padding(mb.Size)
	}
	return size
}

// WriteArchive writes m's archive to w (SX1): the manifest, then every member in m's order,
// each file's bytes from open, checked against the size and digest m lists, which the caller
// made in a first pass over the same sources (SX5); a source that differs fails the write, since
// it was not cold. An archive over maxArchive bytes is refused before anything is written.
func WriteArchive(w io.Writer, m Manifest, open func(Member) (io.ReadCloser, error), maxArchive int64) (Written, error) {
	manifest, err := fits(m, maxArchive)
	if err != nil {
		return Written{}, err
	}
	sum := sha256.New()
	cw := &countWriter{w: io.MultiWriter(w, sum)}
	mtime := m.Seal.TimeMS / 1000
	if err := writeMember(cw, manifestName, false, 0o600, mtime, bytes.NewReader(manifest), int64(len(manifest))); err != nil {
		return Written{}, err
	}
	for _, mb := range m.Members {
		if err := writeSource(cw, mb, mtime, open); err != nil {
			return Written{}, err
		}
	}
	if _, err := cw.Write(make([]byte, 2*block)); err != nil {
		return Written{}, fmt.Errorf("cannot write the archive's end: %w", err)
	}
	return Written{Size: cw.n, SHA256: hex.EncodeToString(sum.Sum(nil))}, nil
}

// fits is m's manifest encoded, refusing a manifest the writer may not write and an archive
// over maxArchive bytes, so a caller can refuse before it makes anything to write into.
func fits(m Manifest, maxArchive int64) ([]byte, error) {
	if err := m.check(); err != nil {
		return nil, err
	}
	manifest := EncodeManifest(m)
	if len(manifest) > maxManifest {
		return nil, refusedf("%s is %d bytes, over its cap of %d bytes", manifestName, len(manifest), maxManifest)
	}
	if size := archiveSize(m, int64(len(manifest))); size > maxArchive {
		return nil, refusedf("the archive would be %d bytes, over the cap of %d bytes (%s)", size, maxArchive, paths.CarryMaxArchiveVar)
	}
	return manifest, nil
}

// writeSource writes one member, a file's bytes checked against the manifest's size and digest.
func writeSource(w io.Writer, mb Member, mtime int64, open func(Member) (io.ReadCloser, error)) error {
	if mb.Dir {
		return writeMember(w, mb.Name, true, mb.Mode, mtime, nil, 0)
	}
	src, err := open(mb)
	if err != nil {
		return fmt.Errorf("cannot open %s: %w", mb.Name, err)
	}
	sum := sha256.New()
	body := io.TeeReader(io.LimitReader(src, mb.Size), sum)
	err = writeMember(w, mb.Name, false, mb.Mode, mtime, body, mb.Size)
	if err == nil {
		err = sameSource(src, sum, mb)
	}
	return errors.Join(err, src.Close())
}

// sameSource fails when the source holds more than the manifest's size or another digest.
func sameSource(src io.Reader, sum hash.Hash, mb Member) error {
	n, err := src.Read(make([]byte, 1))
	if n > 0 || err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s changed since the manifest was made: it is longer than %d bytes", mb.Name, mb.Size)
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != mb.SHA256 {
		return fmt.Errorf("%s changed since the manifest was made: digest %s, not %s", mb.Name, got, mb.SHA256)
	}
	return nil
}

// writeMember writes a header and exactly size bytes of body, then the padding.
func writeMember(w io.Writer, name string, dir bool, mode fs.FileMode, mtime int64, body io.Reader, size int64) error {
	if _, err := w.Write(header(name, dir, mode, size, mtime)); err != nil {
		return fmt.Errorf("cannot write %s: %w", name, err)
	}
	if body != nil {
		n, err := io.Copy(w, body)
		if err != nil {
			return fmt.Errorf("cannot write %s: %w", name, err)
		}
		if n != size {
			return fmt.Errorf("%s changed since the manifest was made: it is %d bytes, not %d", name, n, size)
		}
	}
	if _, err := w.Write(make([]byte, padding(size))); err != nil {
		return fmt.Errorf("cannot write %s: %w", name, err)
	}
	return nil
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// header is a plain ustar header: root's, with no names, links or devices.
func header(name string, dir bool, mode fs.FileMode, size, mtime int64) []byte {
	h := make([]byte, block)
	prefix, base, ok := splitName(name)
	if !ok {
		panic("carry: a member name the manifest check let through does not fit ustar")
	}
	copy(h[0:100], base)
	copy(h[100:108], octal(int64(mode.Perm()), 7)+"\x00")
	copy(h[108:116], octal(0, 7)+"\x00")
	copy(h[116:124], octal(0, 7)+"\x00")
	copy(h[124:136], octal(size, 11)+"\x00")
	copy(h[136:148], octal(mtime, 11)+"\x00")
	h[156] = typeRegular
	if dir {
		h[156] = typeDir
	}
	copy(h[257:263], ustarMagic)
	copy(h[263:265], ustarVersion)
	copy(h[329:337], octal(0, 7)+"\x00")
	copy(h[337:345], octal(0, 7)+"\x00")
	copy(h[345:500], prefix)
	copy(h[148:156], octal(checksum(h), 6)+"\x00 ")
	return h
}

func octal(v int64, width int) string {
	s := strconv.FormatInt(v, 8)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

func padding(size int64) int64 { return (block - size%block) % block }

// checksum is the header's sum with its checksum field read as spaces.
func checksum(h []byte) int64 {
	var sum int64
	for i, c := range h {
		if i >= 148 && i < 156 {
			c = ' '
		}
		sum += int64(c)
	}
	return sum
}

// ReadArchive streams the archive from r for team, never holding it whole (SI5, SI8, SI13):
// the manifest first, its format alone, then strictly, then given to onManifest; then each member
// in the manifest's order given to visit, a file's body as exactly its bytes; then exactly the two
// end blocks. Every structural refusal is ErrRefused; a member whose bytes are not the manifest's
// digest is a failure naming it. No error quotes a member's content or a name the archive holds
// that has not passed the name grammar.
func ReadArchive(r io.Reader, team paths.TeamID, maxArchive int64, onManifest func(Manifest) error, visit func(Member, io.Reader) error) (Read, error) {
	sum := sha256.New()
	in := &capReader{r: io.TeeReader(r, sum), left: maxArchive, limit: maxArchive}
	m, err := readManifest(in, team)
	if err != nil {
		return Read{}, err
	}
	if err := onManifest(m); err != nil {
		return Read{}, err
	}
	mtime := m.Seal.TimeMS / 1000
	for i, mb := range m.Members {
		if err := readMember(in, i+2, mb, mtime, visit); err != nil {
			return Read{}, err
		}
	}
	if err := readEnd(in); err != nil {
		return Read{}, err
	}
	return Read{Size: in.limit - in.left, SHA256: hex.EncodeToString(sum.Sum(nil))}, nil
}

// readManifest reads the first member, which must be the manifest.
func readManifest(in *capReader, team paths.TeamID) (Manifest, error) {
	h, err := in.block()
	if err != nil {
		return Manifest{}, err
	}
	if allZero(h) {
		return Manifest{}, refusedf("the archive holds no member")
	}
	fields, err := parseHeader(h, 1)
	if err != nil {
		return Manifest{}, err
	}
	switch {
	case fields.name == backupManifestName:
		return Manifest{}, refusedf("the archive's first member is %s: a nightly backup is restore's input (DC8), not a carry", backupManifestName)
	case fields.name != manifestName || fields.dir:
		return Manifest{}, refusedf("the archive's first member is not %s", manifestName)
	case fields.mode != 0o600:
		return Manifest{}, refusedf("%s does not have mode 0600", manifestName)
	case fields.size > maxManifest:
		return Manifest{}, refusedf("%s is over its cap of %d bytes", manifestName, maxManifest)
	}
	raw := make([]byte, fields.size)
	if err = in.full(raw); err != nil {
		return Manifest{}, err
	}
	if err = in.padding(fields.size, manifestName); err != nil {
		return Manifest{}, err
	}
	m, err := DecodeManifest(raw, team)
	if err != nil {
		return Manifest{}, err
	}
	if fields.mtime != m.Seal.TimeMS/1000 {
		return Manifest{}, refusedf("%s carries a time that is not its seal's", manifestName)
	}
	return m, nil
}

// readMember reads member number n, which must be mb, and gives it to visit.
func readMember(in *capReader, n int, mb Member, mtime int64, visit func(Member, io.Reader) error) error {
	h, err := in.block()
	if err != nil {
		return err
	}
	if allZero(h) {
		return refusedf("the archive ends before its member %d, %s", n, mb.Name)
	}
	f, err := parseHeader(h, n)
	if err != nil {
		return err
	}
	switch {
	case f.name != mb.Name:
		return refusedf("member %d is not %s: a member not listed, listed twice or out of order", n, mb.Name)
	case f.dir != mb.Dir:
		return refusedf("%s is not of the type the manifest lists", mb.Name)
	case f.mode != mb.Mode:
		return refusedf("%s does not have mode %04o and no special bit", mb.Name, uint32(mb.Mode))
	case f.mtime != mtime:
		return refusedf("%s carries a time that is not the seal's", mb.Name)
	case f.size != mb.Size:
		return refusedf("%s is not of the size the manifest lists", mb.Name)
	}
	sum := sha256.New()
	body := io.TeeReader(&memberReader{in: in, left: mb.Size, name: mb.Name}, sum)
	if mb.Dir {
		return visit(mb, bytes.NewReader(nil))
	}
	if err := visit(mb, body); err != nil {
		return err
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		return err
	}
	if err := in.padding(mb.Size, mb.Name); err != nil {
		return err
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != mb.SHA256 {
		return fmt.Errorf("%s does not have the digest the manifest lists: %s, not %s", mb.Name, got, mb.SHA256)
	}
	return nil
}

// readEnd reads exactly the two end blocks, and nothing after them.
func readEnd(in *capReader) error {
	for range 2 {
		h, err := in.block()
		if err != nil {
			return err
		}
		if !allZero(h) {
			return refusedf("the archive holds a member the manifest does not list")
		}
	}
	n, err := in.Read(make([]byte, 1))
	switch {
	case n > 0:
		return refusedf("the archive has data after its end blocks")
	case errors.Is(err, io.EOF):
		return nil
	case errors.Is(err, ErrRefused):
		return err
	case err != nil:
		return fmt.Errorf("cannot read the archive: %w", err)
	}
	return refusedf("the archive has data after its end blocks")
}

// headerFields are what a header that passed BK3's checks says.
type headerFields struct {
	name        string
	dir         bool
	mode        fs.FileMode
	size, mtime int64
}

// parseHeader holds member n's header to SX1 (BK3, generalised): its checksum, plain ustar, a
// regular file or a directory, a valid name, a plain mode, no owner, names, link or device.
func parseHeader(h []byte, n int) (headerFields, error) {
	at := fmt.Sprintf("member %d", n)
	sum, err := number(h[148:156])
	switch {
	case err != nil || sum != checksum(h):
		return headerFields{}, refusedf("%s has a wrong header checksum", at)
	case string(h[257:263]) != ustarMagic || string(h[263:265]) != ustarVersion:
		return headerFields{}, refusedf("%s is not a plain ustar header", at)
	case h[156] != typeRegular && h[156] != typeDir:
		return headerFields{}, refusedf("%s is neither a regular file nor a directory", at)
	}
	name, ok := headerName(h)
	if !ok {
		return headerFields{}, refusedf("%s has a name that is not valid", at)
	}
	f := headerFields{name: name, dir: h[156] == typeDir}
	if f.mode, err = headerMode(h, name); err != nil {
		return headerFields{}, err
	}
	if f.size, err = number(h[124:136]); err != nil {
		return headerFields{}, refusedf("%s has a size that is not octal", name)
	}
	if f.mtime, err = number(h[136:148]); err != nil {
		return headerFields{}, refusedf("%s has a time that is not octal", name)
	}
	if f.dir && f.size != 0 {
		return headerFields{}, refusedf("%s is a directory with a size", name)
	}
	return f, nil
}

// headerMode is a header's mode, which must be plain permissions, and its other fields' check.
func headerMode(h []byte, name string) (fs.FileMode, error) {
	mode, err := number(h[100:108])
	if err != nil || mode < 0 || mode > 0o777 {
		return 0, refusedf("%s has a mode with a special bit or that is not octal", name)
	}
	return fs.FileMode(uint32(mode)), checkFields(h, name)
}

// headerName is the member name a header holds, its prefix and name fields joined, when both
// fields are well formed and the name passes SX4's grammar.
func headerName(h []byte) (string, bool) {
	base, okBase := field(h[0:100])
	prefix, okPrefix := field(h[345:500])
	name := base
	if prefix != "" {
		name = prefix + "/" + base
	}
	return name, okBase && okPrefix && validName(name)
}

// validName is SX4's grammar with no `.` or `..` segment.
func validName(name string) bool {
	if !memberName.MatchString(name) || path.Clean(name) != name {
		return false
	}
	for s := range strings.SplitSeq(name, "/") {
		if s == "." || s == ".." {
			return false
		}
	}
	return true
}

// checkFields refuses an owner, a device, a link, a user or group name or a trailing byte.
func checkFields(h []byte, name string) error {
	for _, f := range []struct {
		what   string
		lo, hi int
	}{{"uid", 108, 116}, {"gid", 116, 124}, {"device major", 329, 337}, {"device minor", 337, 345}} {
		if v, err := number(h[f.lo:f.hi]); !allZero(h[f.lo:f.hi]) && (err != nil || v != 0) {
			return refusedf("%s has a %s that is not 0", name, f.what)
		}
	}
	for _, f := range []struct {
		what   string
		lo, hi int
	}{{"link name", 157, 257}, {"user name", 265, 297}, {"group name", 297, 329}, {"trailing field", 500, 512}} {
		if !allZero(h[f.lo:f.hi]) {
			return refusedf("%s has a %s", name, f.what)
		}
	}
	return nil
}

// field is a NUL-padded text field; ok is false when bytes follow the first NUL.
func field(f []byte) (string, bool) {
	i := bytes.IndexByte(f, 0)
	if i < 0 {
		return string(f), true
	}
	return string(f[:i]), allZero(f[i:])
}

// number is an octal header field, ended by NULs or spaces.
func number(f []byte) (int64, error) {
	s := string(bytes.TrimRight(f, "\x00 "))
	if s == "" {
		return 0, errors.New("empty")
	}
	for _, c := range s {
		if c < '0' || c > '7' {
			return 0, errors.New("not octal")
		}
	}
	return strconv.ParseInt(s, 8, 64)
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

// capReader reads the archive under its cap: a byte past it is a refusal naming the cap.
type capReader struct {
	r           io.Reader
	left, limit int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		n, err := c.r.Read(make([]byte, 1))
		if n > 0 {
			return 0, refusedf("the archive is over the cap of %d bytes", c.limit)
		}
		return 0, err
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

// full reads exactly len(p) bytes; an archive that ends first is cut short.
func (c *capReader) full(p []byte) error {
	if _, err := io.ReadFull(c, p); err != nil {
		if errors.Is(err, ErrRefused) {
			return err
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return refusedf("the archive is cut short")
		}
		return fmt.Errorf("cannot read the archive: %w", err)
	}
	return nil
}

func (c *capReader) block() ([]byte, error) {
	h := make([]byte, block)
	return h, c.full(h)
}

// padding reads a member's padding, which must be zero.
func (c *capReader) padding(size int64, name string) error {
	p := make([]byte, padding(size))
	if err := c.full(p); err != nil {
		return err
	}
	if !allZero(p) {
		return refusedf("%s is padded with bytes that are not zero", name)
	}
	return nil
}

// memberReader is one member's body: exactly left bytes of the archive.
type memberReader struct {
	in   *capReader
	left int64
	name string
}

func (m *memberReader) Read(p []byte) (int, error) {
	if m.left == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > m.left {
		p = p[:m.left]
	}
	n, err := m.in.Read(p)
	m.left -= int64(n)
	if errors.Is(err, io.EOF) && m.left > 0 {
		return n, refusedf("%s is cut short", m.name)
	}
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return n, err
}
