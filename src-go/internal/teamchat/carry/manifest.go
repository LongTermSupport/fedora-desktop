package carry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// ExportFormat is the manifest's `format`: the one compatibility check between an exporter and
// an importer, raised whenever a member, a field or the order of SX2 changes in a way an older
// import would misread (SX8, O9). A constant of this repository, never a flag or a setting.
const ExportFormat = 1

// Kind is an export's kind: a sealed team's, or a drill's of a live one (SX5, SD3).
type Kind string

// The two kinds.
const (
	KindSealed Kind = "sealed"
	KindDrill  Kind = "drill"
)

// RosterFacts are the roster's `seq`, `rev` and restore epoch at the seal (SC5).
type RosterFacts struct{ Seq, Rev, RestoreEpoch int64 }

// EpochFloor is restore-epoch.json's `{epoch, min_backup}`, `{0, 0}` when absent (SC5).
type EpochFloor struct{ Epoch, MinBackup int64 }

// TailEntry is a room's newest event at the seal (SC2): the room's purpose in rooms.json, the
// event's id and its origin_server_ts.
type TailEntry struct {
	Room, EventID string
	TsMS          int64
}

// SealFacts are what a seal or a drill read of the team after its stop (SC7, SD3): TimeMS is
// `sealed_ms` for a sealed export and `drilled_ms` for a drill.
type SealFacts struct {
	TimeMS     int64
	ServerName string
	Port       paths.Port
	Tuwunel    paths.ServerVersion
	Software   paths.Digest
	Roster     RosterFacts
	EpochFloor EpochFloor
	Tail       []TailEntry
}

// Member is one archive member after the manifest (SX5): a file with its size and digest, or a
// directory with neither.
type Member struct {
	Name   string
	Dir    bool
	Mode   fs.FileMode
	Size   int64
	SHA256 string
}

// Manifest is the archive's first member, carry.json (SX5).
type Manifest struct {
	Format     int64
	Kind       Kind
	Team       paths.TeamID
	ServerName string
	Seal       SealFacts
	Members    []Member
}

// The manifest member's name and cap (SX4).
const (
	manifestName = "carry.json"
	maxManifest  = 1 << 20
)

// maxNumber is the largest whole number a record or manifest holds (JSON's exact integers).
const maxNumber = 1<<53 - 1

// memberName is SX4's grammar of a member name; nameSegment is one segment of it.
var (
	memberName  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._+-]{0,127}(/[A-Za-z0-9_][A-Za-z0-9._+-]{0,127})*$`)
	textGrammar = regexp.MustCompile(`^[\x21-\x7e]{1,255}$`)
)

// topDirs are the trees of SX2, each a directory member of its own.
var topDirs = []string{"admin", "ctl", "status", "hs"}

// The modes SX3 gives: files 0600 but the CA certificate's 0644 and the approval's 0640, and
// directories 0700.
var fileModes = []fs.FileMode{0o600, 0o644, 0o640}

const dirMode fs.FileMode = 0o700

// The keys more than one record or the manifest holds.
const (
	keyKind = "kind"
	keyTail = "tail"
	keyStep = "step"
	keyTeam = "team"
)

// timeKey is the seal's time key for kind.
func timeKey(k Kind) string {
	if k == KindDrill {
		return "drilled_ms"
	}
	return "sealed_ms"
}

// EncodeSealFields are the seal's members from its time on, as SC7 and SX5 give them, for kind.
func encodeSealFields(k Kind, s SealFacts) []proto.Member {
	return []proto.Member{
		proto.M(timeKey(k), proto.Int(s.TimeMS)), proto.M("server_name", proto.Str(s.ServerName)),
		proto.M("port", proto.Int(int64(s.Port.Int()))), proto.M("tuwunel", proto.Str(s.Tuwunel.String())),
		proto.M("software", proto.Str(s.Software.String())),
		proto.M("roster", proto.Obj(proto.M("seq", proto.Int(s.Roster.Seq)), proto.M("rev", proto.Int(s.Roster.Rev)),
			proto.M("restore_epoch", proto.Int(s.Roster.RestoreEpoch)))),
		proto.M("epoch_floor", proto.Obj(proto.M("epoch", proto.Int(s.EpochFloor.Epoch)), proto.M("min_backup", proto.Int(s.EpochFloor.MinBackup)))),
		proto.M(keyTail, tailValue(s.Tail)),
	}
}

// EncodeManifest is the manifest's canonical JSON.
func EncodeManifest(m Manifest) []byte {
	members := make([]proto.Value, 0, len(m.Members))
	for _, mb := range m.Members {
		typ := typeFileText
		fields := []proto.Member{proto.M("name", proto.Str(mb.Name))}
		if mb.Dir {
			typ = typeDirText
		}
		fields = append(fields, proto.M("type", proto.Str(typ)), proto.M("mode", proto.Str(modeText(mb.Mode))))
		if !mb.Dir {
			fields = append(fields, proto.M("size", proto.Int(mb.Size)), proto.M("sha256", proto.Str(mb.SHA256)))
		}
		members = append(members, proto.Obj(fields...))
	}
	return proto.Obj(
		proto.M("format", proto.Int(m.Format)), proto.M(keyKind, proto.Str(string(m.Kind))),
		proto.M(keyTeam, proto.Str(m.Team.String())), proto.M("server_name", proto.Str(m.ServerName)),
		proto.M("seal", proto.Obj(encodeSealFields(m.Kind, m.Seal)...)), proto.M("members", proto.Arr(members...)),
	).Compact()
}

// The manifest's member types.
const (
	typeFileText = "file"
	typeDirText  = "dir"
)

// modeText is a mode as the manifest writes it; a special bit, which no member may have, shows
// as one so the check refuses it.
func modeText(m fs.FileMode) string {
	return fmt.Sprintf("%04o", uint32(m.Perm()|m&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky)))
}

// checkFormat is SI13: the manifest's `format` read alone and leniently, before anything else of
// it is judged; an unknown, absent or non-integer format is refused naming both versions.
func checkFormat(raw []byte) error {
	var top map[string]json.RawMessage
	what := "absent"
	f, ok := []byte(nil), false
	if json.Unmarshal(raw, &top) == nil {
		f, ok = top["format"]
	}
	if ok {
		what = "not a whole number"
		n, err := strconv.ParseInt(string(f), 10, 64)
		switch {
		case err == nil && n == ExportFormat:
			return nil
		case err == nil:
			what = strconv.FormatInt(n, 10)
		}
	}
	return refusedf("the export is format %s, this teamchat-admin imports format %d", what, ExportFormat)
}

var manifestKeys = []string{"format", keyKind, keyTeam, "server_name", "seal", "members"}

// DecodeManifest reads team's manifest strictly: its format first (SI13), then every field, and
// the members as WriteArchive would write them. Every refusal is ErrRefused.
func DecodeManifest(raw []byte, team paths.TeamID) (Manifest, error) {
	if len(raw) > maxManifest {
		return Manifest{}, refusedf("%s is over its cap of %d bytes", manifestName, maxManifest)
	}
	if err := checkFormat(raw); err != nil {
		return Manifest{}, err
	}
	v, err := proto.Parse(raw)
	if err != nil {
		return Manifest{}, refusedf("%s is not strict JSON", manifestName)
	}
	rd := &reader{file: manifestName}
	top := rd.object(v, "manifest", manifestKeys)
	m := Manifest{Format: rd.integer(top["format"], "format", ExportFormat, ExportFormat)}
	m.Kind = Kind(rd.str(top[keyKind], keyKind, func(s string) bool { return s == string(KindSealed) || s == string(KindDrill) }))
	rd.str(top[keyTeam], keyTeam, func(s string) bool { return s == team.String() })
	m.Team = team
	m.ServerName = rd.str(top["server_name"], "server_name", textGrammar.MatchString)
	if rd.err != nil {
		return Manifest{}, refusedf("%v", rd.err)
	}
	m.Seal = decodeSeal(rd, top["seal"], m.Kind, "seal", nil)
	m.Members = decodeMembers(rd, top["members"])
	if rd.err != nil {
		return Manifest{}, refusedf("%v", rd.err)
	}
	if err := m.check(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

var sealKeys = []string{"server_name", "port", "tuwunel", "software", "roster", "epoch_floor", keyTail}

// decodeSeal reads a seal's fields from its time on, with any extra keys a record adds.
func decodeSeal(rd *reader, v proto.Value, k Kind, at string, extra map[string]*proto.Value, optional ...string) SealFacts {
	keys := append([]string{timeKey(k)}, sealKeys...)
	for key := range extra {
		keys = append(keys, key)
	}
	f := rd.object(v, at, keys, optional...)
	for key, p := range extra {
		*p = f[key]
	}
	s := SealFacts{TimeMS: rd.integer(f[timeKey(k)], at+"."+timeKey(k), 1, maxNumber), ServerName: rd.str(f["server_name"], at+".server_name", textGrammar.MatchString)}
	if port, err := paths.ParsePort(int(rd.integer(f["port"], at+".port", 1, 65535))); err == nil {
		s.Port = port
	}
	s.Tuwunel = parsed(rd, f["tuwunel"], at+".tuwunel", paths.ParseServerVersion)
	s.Software = parsed(rd, f["software"], at+".software", paths.ParseDigest)
	r := rd.object(f["roster"], at+".roster", []string{"seq", "rev", "restore_epoch"})
	s.Roster = RosterFacts{rd.integer(r["seq"], at+".roster.seq", 0, maxNumber), rd.integer(r["rev"], at+".roster.rev", 0, maxNumber), rd.integer(r["restore_epoch"], at+".roster.restore_epoch", 0, maxNumber)}
	e := rd.object(f["epoch_floor"], at+".epoch_floor", []string{"epoch", "min_backup"})
	s.EpochFloor = EpochFloor{rd.integer(e["epoch"], at+".epoch_floor.epoch", 0, maxNumber), rd.integer(e["min_backup"], at+".epoch_floor.min_backup", 0, maxNumber)}
	s.Tail = decodeTail(rd, f[keyTail], at+".tail")
	return s
}

func decodeTail(rd *reader, v proto.Value, at string) []TailEntry {
	if v.Kind() != proto.KindArray || len(v.Items()) == 0 {
		rd.fail(at, "is not a list of at least one room")
		return nil
	}
	var out []TailEntry
	for _, item := range v.Items() {
		f := rd.object(item, at+"[]", []string{"room", "event_id", "ts_ms"})
		e := TailEntry{
			Room: rd.str(f["room"], at+"[].room", textGrammar.MatchString), EventID: rd.str(f["event_id"], at+"[].event_id", textGrammar.MatchString),
			TsMS: rd.integer(f["ts_ms"], at+"[].ts_ms", 0, maxNumber),
		}
		if slices.ContainsFunc(out, func(o TailEntry) bool { return o.Room == e.Room }) {
			rd.fail(at+"[].room", "names a room twice")
		}
		out = append(out, e)
	}
	return out
}

// parsed is v's string through parse, an id of paths.
func parsed[T any](rd *reader, v proto.Value, at string, parse func(string) (T, error)) T {
	var zero T
	s := rd.str(v, at, func(string) bool { return true })
	if rd.err != nil {
		return zero
	}
	out, err := parse(s)
	if err != nil {
		rd.fail(at, "is not valid")
		return zero
	}
	return out
}

func decodeMembers(rd *reader, v proto.Value) []Member {
	if v.Kind() != proto.KindArray {
		rd.fail("members", "is not a list")
		return nil
	}
	var out []Member
	for _, item := range v.Items() {
		f := rd.object(item, "members[]", []string{"name", "type", "mode"}, "size", "sha256")
		mb := Member{Name: rd.str(f["name"], "members[].name", memberName.MatchString)}
		mb.Dir = rd.str(f["type"], "members[].type", func(s string) bool { return s == typeFileText || s == typeDirText }) == typeDirText
		mode := rd.str(f["mode"], "members[].mode", func(s string) bool { return len(s) == 4 && strings.Trim(s, "01234567") == "" })
		if n, err := strconv.ParseUint(mode, 8, 32); err == nil {
			mb.Mode = fs.FileMode(n)
		}
		size, sizeOK := f["size"]
		sum, sumOK := f["sha256"]
		switch {
		case mb.Dir && (sizeOK || sumOK):
			rd.fail("members[]", "is a directory with a size or a digest")
		case !mb.Dir && (!sizeOK || !sumOK):
			rd.fail("members[]", "is a file without a size and a digest")
		case !mb.Dir:
			mb.Size = rd.integer(size, "members[].size", 0, maxNumber)
			mb.SHA256 = rd.str(sum, "members[].sha256", func(s string) bool { return len(s) == 64 && strings.Trim(s, "0123456789abcdef") == "" })
		}
		out = append(out, mb)
	}
	return out
}

// check holds a manifest to what the writer may write and the reader stages (SX3, SX4): its
// format, kind and seal, and members of valid names that fit ustar, each listed once, under one
// of SX2's trees whose directory came first, of a mode SX3 gives.
func (m Manifest) check() error {
	switch {
	case m.Format != ExportFormat:
		return refusedf("the export is format %d, this teamchat-admin imports format %d", m.Format, ExportFormat)
	case m.Kind != KindSealed && m.Kind != KindDrill:
		return refusedf("%s has no kind of sealed or drill", manifestName)
	case !textGrammar.MatchString(m.ServerName) || m.Seal.ServerName != m.ServerName:
		return refusedf("%s's server name is not its seal's", manifestName)
	case m.Seal.TimeMS <= 0 || len(m.Seal.Tail) == 0:
		return refusedf("%s's seal has no time or no tail", manifestName)
	case len(m.Members) == 0:
		return refusedf("%s lists no members", manifestName)
	}
	seen := map[string]bool{}
	for i, mb := range m.Members {
		if err := checkMember(mb, seen); err != nil {
			return refusedf("%s member %d: %v", manifestName, i+1, err)
		}
		seen[mb.Name] = mb.Dir
	}
	return nil
}

func checkMember(mb Member, seen map[string]bool) error {
	if err := checkMemberName(mb.Name, seen); err != nil {
		return err
	}
	parent := path.Dir(mb.Name)
	switch {
	case parent == "." && (!mb.Dir || !slices.Contains(topDirs, mb.Name)):
		return fmt.Errorf("%s is not one of the archive's trees", mb.Name)
	case parent != "." && !seen[parent]:
		return fmt.Errorf("%s comes before its directory", mb.Name)
	case mb.Dir && mb.Mode != dirMode:
		return fmt.Errorf("%s is a directory not of mode %04o", mb.Name, dirMode)
	case !mb.Dir && !slices.Contains(fileModes, mb.Mode):
		return fmt.Errorf("%s has a mode no carried file has", mb.Name)
	case !mb.Dir && (mb.Size < 0 || len(mb.SHA256) != 64):
		return fmt.Errorf("%s has no size or digest", mb.Name)
	}
	return nil
}

// checkMemberName refuses a name SX4 does not allow, one that does not fit ustar, or a repeat.
func checkMemberName(name string, seen map[string]bool) error {
	if !validName(name) {
		return errors.New("is not a valid name")
	}
	if _, _, ok := splitName(name); !ok {
		return fmt.Errorf("%s does not fit a ustar header", name)
	}
	if _, dup := seen[name]; dup {
		return fmt.Errorf("%s is listed twice", name)
	}
	return nil
}

// splitName is name as ustar's prefix and name fields (SX4: 155 and 100 bytes, split at a /).
func splitName(name string) (string, string, bool) {
	if len(name) <= 100 {
		return "", name, true
	}
	for i := len(name) - 1; i > 0; i-- {
		if name[i] == '/' && i <= 155 && len(name)-i-1 <= 100 && len(name)-i-1 > 0 {
			return name[:i], name[i+1:], true
		}
	}
	return "", "", false
}
