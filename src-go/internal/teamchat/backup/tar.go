// Package backup is a team's backups (Plan 00209 SPEC-n2g.md BK, BT, HB1, BU1; DESIGN-v2-C-host.md
// 3.12.1): the tar that pairs root's control state with a database backup Tuwunel made, its one
// parser (I-B5), Tuwunel's backup numbers read beneath a root-owned descriptor, the internal
// backup that `team apply`, `backup` and the restore call under a team lock they hold, the last
// run's record, the health tick's view of a team's backups, and the backup units as text.
package backup

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The members of a backup tar in their order, and each one's cap (BK1, BK2).
const (
	nameManifest = "backup.json"
	nameTeam     = "team.json"
	nameConfig   = "config.toml"
	nameRoster   = "roster.json"
	nameRooms    = "rooms.json"
	nameToken    = "admin.token"

	capSmall   = 4 << 10
	capRoot    = 64 << 10
	capCtl     = 1 << 20
	maxArchive = 4 << 20
)

var members = []struct {
	name string
	cap  int64
}{
	{nameManifest, capSmall},
	{nameTeam, capRoot},
	{nameConfig, capRoot},
	{nameRoster, capCtl},
	{nameRooms, capCtl},
	{nameToken, capSmall},
}

const (
	block        = 512
	manifestVer  = 1
	memberMode   = 0o600
	typeRegular  = '0'
	ustarMagic   = "ustar\x00"
	ustarVersion = "00"
)

// Manifest is a tar's `backup.json` (n2g-D1): the team, Tuwunel's backup number, the backup's
// time in whole seconds as milliseconds, and the team's server name.
type Manifest struct {
	Team       paths.TeamID
	Backup     int64
	TimeMS     int64
	ServerName string
}

func (m Manifest) encode() []byte {
	return proto.Obj(
		proto.M("v", proto.Int(manifestVer)), proto.M("team", proto.Str(m.Team.String())),
		proto.M("backup", proto.Int(m.Backup)), proto.M("time_ms", proto.Int(m.TimeMS)),
		proto.M("server_name", proto.Str(m.ServerName)),
	).Compact()
}

// Sources are the five files a tar pairs with the database backup, as read (BK4).
type Sources struct {
	TeamJSON, Config, Roster, Rooms, Token []byte
}

// Contents is a tar that validated (BK3): every member's bytes, and what they decode to.
type Contents struct {
	Manifest                                Manifest
	Team                                    declare.Team
	Roster                                  apply.RosterFile
	Rooms                                   proto.RoomIDs
	Token                                   string
	TeamJSON, Config, RosterJSON, RoomsJSON []byte
}

// Encode is the tar of BK1: the manifest and the sources as six plain ustar members, each
// 0600, uid and gid 0, with the manifest's time.
func Encode(m Manifest, s Sources) ([]byte, error) {
	if m.TimeMS <= 0 || m.TimeMS%1000 != 0 {
		return nil, errors.New("a backup's time must be whole seconds after the epoch")
	}
	data := [][]byte{m.encode(), s.TeamJSON, s.Config, s.Roster, s.Rooms, s.Token}
	var b bytes.Buffer
	for i, mb := range members {
		if int64(len(data[i])) > mb.cap {
			return nil, fmt.Errorf("%s is over its cap of %d bytes", mb.name, mb.cap)
		}
		b.Write(header(mb.name, int64(len(data[i])), m.TimeMS/1000))
		b.Write(data[i])
		b.Write(make([]byte, padding(int64(len(data[i])))))
	}
	b.Write(make([]byte, 2*block))
	if b.Len() > maxArchive {
		return nil, fmt.Errorf("the backup tar is over its cap of %d bytes", maxArchive)
	}
	return b.Bytes(), nil
}

func header(name string, size, mtime int64) []byte {
	h := make([]byte, block)
	copy(h[0:100], name)
	copy(h[100:108], octal(memberMode, 7)+"\x00")
	copy(h[108:116], octal(0, 7)+"\x00")
	copy(h[116:124], octal(0, 7)+"\x00")
	copy(h[124:136], octal(size, 11)+"\x00")
	copy(h[136:148], octal(mtime, 11)+"\x00")
	h[156] = typeRegular
	copy(h[257:263], ustarMagic)
	copy(h[263:265], ustarVersion)
	copy(h[329:337], octal(0, 7)+"\x00")
	copy(h[337:345], octal(0, 7)+"\x00")
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

// Decode is the one parser of a backup tar (BK3, I-B5): backup n of team, or the reason it is
// refused. No error quotes a member's content or a name the tar holds.
func Decode(raw []byte, team paths.TeamID, n int64) (Contents, error) {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("backup %d of team %s: "+format, append([]any{n, team}, args...)...)
	}
	if len(raw) > maxArchive {
		return Contents{}, fail("the tar is over its cap of %d bytes", maxArchive)
	}
	data, mtimes, err := framing(raw)
	if err != nil {
		return Contents{}, fail("%w", err)
	}
	m, err := decodeManifest(data[0], team, n)
	if err != nil {
		return Contents{}, fail("%s: %w", nameManifest, err)
	}
	for i, mt := range mtimes {
		if mt != m.TimeMS/1000 {
			return Contents{}, fail("%s carries a time that is not the manifest's", members[i].name)
		}
	}
	c, err := decodeMembers(data, m, team)
	if err != nil {
		return Contents{}, fail("%w", err)
	}
	return c, nil
}

// framing reads the six members' bytes and times, then exactly the two end blocks.
func framing(raw []byte) ([][]byte, []int64, error) {
	data := make([][]byte, len(members))
	mtimes := make([]int64, len(members))
	off := 0
	for i, mb := range members {
		if off+block > len(raw) || allZero(raw[off:off+block]) {
			return nil, nil, fmt.Errorf("the tar ends before its member %d, %s", i+1, mb.name)
		}
		size, mtime, err := checkHeader(raw[off:off+block], i)
		if err != nil {
			return nil, nil, err
		}
		if size > mb.cap {
			return nil, nil, fmt.Errorf("%s is over its cap of %d bytes", mb.name, mb.cap)
		}
		off += block
		end := off + int(size+padding(size))
		if end > len(raw) {
			return nil, nil, fmt.Errorf("%s is cut short", mb.name)
		}
		if !allZero(raw[off+int(size) : end]) {
			return nil, nil, fmt.Errorf("%s is padded with bytes that are not zero", mb.name)
		}
		data[i], mtimes[i] = raw[off:off+int(size)], mtime
		off = end
	}
	rest := raw[off:]
	switch {
	case len(rest) >= block && !allZero(rest[:block]):
		return nil, nil, errors.New("the tar holds a member beyond the six of a backup")
	case len(rest) < 2*block:
		return nil, nil, errors.New("the tar has no end blocks")
	case len(rest) > 2*block || !allZero(rest):
		return nil, nil, errors.New("the tar has data after its end blocks")
	}
	return data, mtimes, nil
}

// checkHeader holds member i's header to BK1 and gives its size and time.
func checkHeader(h []byte, i int) (int64, int64, error) {
	want := members[i].name
	at := fmt.Sprintf("member %d", i+1)
	sum, err := number(h[148:156])
	switch {
	case err != nil || sum != checksum(h):
		return 0, 0, errors.New(at + " has a wrong header checksum")
	case string(h[257:263]) != ustarMagic || string(h[263:265]) != ustarVersion:
		return 0, 0, errors.New(at + " is not a plain ustar header")
	case h[156] != typeRegular:
		return 0, 0, errors.New(at + " is not a regular file")
	}
	name, ok := field(h[0:100])
	if !ok {
		return 0, 0, errors.New(at + " has a malformed name")
	}
	if name != want {
		return 0, 0, fmt.Errorf("%s is not %s: %s", at, want, misplaced(name))
	}
	mode, err := number(h[100:108])
	if err != nil || mode != memberMode {
		return 0, 0, fmt.Errorf("%s does not have mode 0600 and no special bit", want)
	}
	if err = checkFields(h, want); err != nil {
		return 0, 0, err
	}
	size, err := number(h[124:136])
	if err != nil {
		return 0, 0, fmt.Errorf("%s has a size that is not octal", want)
	}
	mtime, err := number(h[136:148])
	if err != nil {
		return 0, 0, fmt.Errorf("%s has a time that is not octal", want)
	}
	return size, mtime, nil
}

// checkFields refuses an owner, a device, a link, a user or group name, a name prefix or a
// trailing byte in a header.
func checkFields(h []byte, want string) error {
	for _, f := range []struct {
		what   string
		lo, hi int
	}{{"uid", 108, 116}, {"gid", 116, 124}, {"device major", 329, 337}, {"device minor", 337, 345}} {
		if v, err := number(h[f.lo:f.hi]); !allZero(h[f.lo:f.hi]) && (err != nil || v != 0) {
			return fmt.Errorf("%s has a %s that is not 0", want, f.what)
		}
	}
	for _, f := range []struct {
		what   string
		lo, hi int
	}{{"link name", 157, 257}, {"user name", 265, 297}, {"group name", 297, 329}, {"name prefix", 345, 500}, {"trailing field", 500, 512}} {
		if !allZero(h[f.lo:f.hi]) {
			return fmt.Errorf("%s has a %s", want, f.what)
		}
	}
	return nil
}

// misplaced says what a wrong name is without quoting it.
func misplaced(name string) string {
	for _, mb := range members {
		if mb.name == name {
			return "a member out of order or given twice"
		}
	}
	return "a name no backup holds"
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

var manifestKeys = []string{"v", "team", "backup", "time_ms", "server_name"}

func decodeManifest(raw []byte, team paths.TeamID, n int64) (Manifest, error) {
	v, err := proto.Parse(raw)
	if err != nil {
		return Manifest{}, errors.New("is not strict JSON")
	}
	if v.Kind() != proto.KindObject || len(v.Members()) != len(manifestKeys) {
		return Manifest{}, errors.New("is not an object of exactly v, team, backup, time_ms and server_name")
	}
	got, err := manifestFields(v)
	if err != nil {
		return Manifest{}, err
	}
	m := Manifest{Backup: got["backup"].Int(), TimeMS: got["time_ms"].Int(), ServerName: got["server_name"].Str()}
	id, err := paths.ParseTeamID(got["team"].Str())
	switch {
	case got["v"].Int() != manifestVer:
		return Manifest{}, errors.New("is not version 1")
	case err != nil || id != team:
		return Manifest{}, errors.New("names another team")
	case m.Backup != n:
		return Manifest{}, errors.New("names another backup number")
	case m.TimeMS <= 0 || m.TimeMS%1000 != 0:
		return Manifest{}, errors.New("has a time that is not whole seconds after the epoch")
	}
	m.Team = id
	return m, nil
}

// manifestFields are the manifest's members by key, each of its type.
func manifestFields(v proto.Value) (map[string]proto.Value, error) {
	got := map[string]proto.Value{}
	for _, m := range v.Members() {
		got[m.Key] = m.Val
	}
	for _, k := range manifestKeys {
		want := proto.KindInt
		if k == "team" || k == "server_name" {
			want = proto.KindString
		}
		if val, ok := got[k]; !ok || val.Kind() != want {
			return nil, fmt.Errorf("has no %s of the right type", k)
		}
	}
	return got, nil
}

func decodeMembers(data [][]byte, m Manifest, team paths.TeamID) (Contents, error) {
	c := Contents{Manifest: m, TeamJSON: data[1], Config: data[2], RosterJSON: data[3], RoomsJSON: data[4], Token: string(data[5])}
	var err error
	if c.Team, err = declare.DecodeTeamFile(data[1]); err != nil {
		return Contents{}, err
	}
	if c.Team.ID != team || c.Team.ServerName != m.ServerName {
		return Contents{}, fmt.Errorf("%s is not the team and server name the manifest names", nameTeam)
	}
	if !utf8.Valid(c.Config) || bytes.IndexByte(c.Config, 0) >= 0 {
		return Contents{}, fmt.Errorf("%s is not UTF-8 text without NUL", nameConfig)
	}
	if c.Roster, err = apply.DecodeRosterFile(data[3], team); err != nil {
		return Contents{}, err
	}
	if c.Rooms, err = apply.DecodeRooms(data[4], team); err != nil {
		return Contents{}, err
	}
	if !isToken(data[5]) {
		return Contents{}, fmt.Errorf("%s is not 1 to 4096 bytes of printable ASCII without a space", nameToken)
	}
	return c, nil
}

// isToken is ctl's grammar of the admin token (OP2).
func isToken(data []byte) bool {
	if len(data) == 0 || len(data) > capSmall {
		return false
	}
	for _, c := range data {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}
