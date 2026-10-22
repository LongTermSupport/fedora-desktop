package apply

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// maxFile caps the roster file and the rooms record (AP1).
const maxFile = 1 << 20

// fileVersion is the `v` of both files.
const fileVersion = 1

// fileMode is both files' mode: the control user's alone.
const fileMode = 0o600

// The member states of C 3.6.1. MemberClaimed is the intent state: a handle `member add` has
// claimed whose account is not made yet (n2b-D12).
const (
	MemberActive  = "active"
	MemberParked  = "parked"
	MemberRetired = "retired"
	MemberLost    = "lost"
	MemberClaimed = "claimed"
)

// The human states (n2b-D7).
const (
	HumanActive  = "active"
	HumanRemoved = "removed"
)

const keySince = "since_ms"

const (
	credPassword = "password"
	credToken    = "token"
	needsReissue = "reissue"
	needsPass    = "password"
)

// OpTeamApply is `pending`'s op while `team apply` runs (n2b-D8).
const OpTeamApply = "team.apply"

var kindRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// RosterFile is `ctl/<team>/roster.json` (C 3.6.1, AP1).
type RosterFile struct {
	Team         paths.TeamID
	Seq          int64
	Rev          int64
	RestoreEpoch int64
	Counters     map[string]int64
	Members      map[string]Member
	Humans       map[string]Human
	// Coordinator and CoordinatorWanted are handles, empty for none.
	Coordinator       string
	CoordinatorWanted string
	Pending           *Pending
}

// Member is one agent of the roster file; Cred and Needs are empty for null.
type Member struct {
	State     string
	Kind      string
	HumanText bool
	SinceMS   int64
	Cred      string
	CredSeq   int64
	CredMS    int64
	Needs     string
}

// Human is one human of the roster file; Needs is empty for null.
type Human struct {
	State      string
	SinceMS    int64
	RevokedSeq int64
	Needs      string
}

// Pending is the one operation in progress; Handle is a member operation's only.
type Pending struct {
	Op, Step string
	SinceMS  int64
	Handle   string
}

// NewRosterFile is a new team's empty roster, at seq 0.
func NewRosterFile(team paths.TeamID) RosterFile {
	return RosterFile{Team: team, Counters: map[string]int64{}, Members: map[string]Member{}, Humans: map[string]Human{}}
}

func nullable(s string) proto.Value {
	if s == "" {
		return proto.Null()
	}
	return proto.Str(s)
}

// sortedMembers is a map as object members in key order, each value encoded by enc.
func sortedMembers[V any](m map[string]V, enc func(V) proto.Value) []proto.Member {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]proto.Member, 0, len(keys))
	for _, k := range keys {
		out = append(out, proto.M(k, enc(m[k])))
	}
	return out
}

// Encode is the file's canonical JSON: keys in C 3.6.1's order, maps in key order.
func (f RosterFile) Encode() []byte {
	pending := proto.Null()
	if p := f.Pending; p != nil {
		members := []proto.Member{proto.M("op", proto.Str(p.Op)), proto.M("step", proto.Str(p.Step)), proto.M("since_ms", proto.Int(p.SinceMS))}
		if p.Handle != "" {
			members = append(members, proto.M("handle", proto.Str(p.Handle)))
		}
		pending = proto.Obj(members...)
	}
	return proto.Obj(
		proto.M("v", proto.Int(fileVersion)), proto.M("team", proto.Str(f.Team.String())),
		proto.M("seq", proto.Int(f.Seq)), proto.M("rev", proto.Int(f.Rev)), proto.M("restore_epoch", proto.Int(f.RestoreEpoch)),
		proto.M("counters", proto.Obj(sortedMembers(f.Counters, proto.Int)...)),
		proto.M("members", proto.Obj(sortedMembers(f.Members, Member.encode)...)),
		proto.M("humans", proto.Obj(sortedMembers(f.Humans, Human.encode)...)),
		proto.M("coordinator", nullable(f.Coordinator)), proto.M("coordinator_wanted", nullable(f.CoordinatorWanted)),
		proto.M("pending", pending),
	).Compact()
}

func (m Member) encode() proto.Value {
	return proto.Obj(
		proto.M("state", proto.Str(m.State)), proto.M("kind", proto.Str(m.Kind)), proto.M("human_text", proto.Bool(m.HumanText)),
		proto.M("since_ms", proto.Int(m.SinceMS)), proto.M("cred", nullable(m.Cred)), proto.M("cred_seq", proto.Int(m.CredSeq)),
		proto.M("cred_ms", proto.Int(m.CredMS)), proto.M("needs", nullable(m.Needs)))
}

func (h Human) encode() proto.Value {
	return proto.Obj(
		proto.M("state", proto.Str(h.State)), proto.M("since_ms", proto.Int(h.SinceMS)),
		proto.M("revoked_seq", proto.Int(h.RevokedSeq)), proto.M("needs", nullable(h.Needs)))
}

// reader reads one file strictly and keeps its first refusal, which names the field and never
// the value.
type reader struct {
	file string
	err  error
}

func (r *reader) fail(path, problem string) {
	if r.err == nil {
		r.err = fmt.Errorf("%s: %s %s", r.file, path, problem)
	}
}

// object is v's members when v is an object whose keys are exactly required plus any of
// optional.
func (r *reader) object(v proto.Value, path string, required []string, optional ...string) map[string]proto.Value {
	if v.Kind() != proto.KindObject {
		r.fail(path, "is not an object")
		return map[string]proto.Value{}
	}
	out := map[string]proto.Value{}
	for _, m := range v.Members() {
		if !slices.Contains(required, m.Key) && !slices.Contains(optional, m.Key) {
			r.fail(path+"."+m.Key, "is not a known key")
		}
		out[m.Key] = m.Val
	}
	for _, k := range required {
		if _, ok := out[k]; !ok {
			r.fail(path+"."+k, "is required")
		}
	}
	return out
}

func (r *reader) integer(v proto.Value, path string) int64 {
	if v.Kind() != proto.KindInt || v.Int() < 0 {
		r.fail(path, "is not a non-negative integer")
		return 0
	}
	return v.Int()
}

// str is v's string, which must pass valid; a nullable field reads null as "".
func (r *reader) str(v proto.Value, path string, nullOK bool, valid func(string) bool) string {
	if nullOK && v.Kind() == proto.KindNull {
		return ""
	}
	if v.Kind() != proto.KindString || !valid(v.Str()) {
		r.fail(path, "is not valid")
		return ""
	}
	return v.Str()
}

func (r *reader) boolean(v proto.Value, path string) bool {
	if v.Kind() != proto.KindBool {
		r.fail(path, "is not true or false")
	}
	return v.Bool()
}

func oneOf(values ...string) func(string) bool {
	return func(s string) bool { return slices.Contains(values, s) }
}

func validHandle(s string) bool {
	h, ok := proto.ParseHandle(s)
	return ok && h.String() == s
}

// validCounter is a handle prefix, `<repo>+<site>.<type>`.
func validCounter(s string) bool {
	p, ok := proto.ParsePrefix(s)
	return ok && p.Repo+"+"+p.Site+"."+p.Type == s
}

func parseFile(raw []byte, file string) (proto.Value, error) {
	if len(raw) > maxFile {
		return proto.Value{}, fmt.Errorf("%s: over %d bytes", file, maxFile)
	}
	v, err := proto.Parse(raw)
	if err != nil {
		return proto.Value{}, fmt.Errorf("%s: %w", file, err)
	}
	return v, nil
}

// header checks `v` and `team`.
func (r *reader) header(top map[string]proto.Value, team paths.TeamID) {
	if v := top["v"]; v.Kind() != proto.KindInt || v.Int() != fileVersion {
		r.fail("v", "is not 1")
	}
	if t := top["team"]; t.Kind() != proto.KindString || t.Str() != team.String() {
		r.fail("team", "is not the team asked for")
	}
}

var rosterKeys = []string{
	"v", "team", "seq", "rev", "restore_epoch", "counters", "members", "humans", "coordinator", "coordinator_wanted", "pending",
}

// DecodeRosterFile reads team's roster file strictly (AP1).
func DecodeRosterFile(raw []byte, team paths.TeamID) (RosterFile, error) {
	v, err := parseFile(raw, "roster.json")
	if err != nil {
		return RosterFile{}, err
	}
	r := &reader{file: "roster.json"}
	top := r.object(v, "roster", rosterKeys)
	r.header(top, team)
	f := NewRosterFile(team)
	f.Seq, f.Rev, f.RestoreEpoch = r.integer(top["seq"], "seq"), r.integer(top["rev"], "rev"), r.integer(top["restore_epoch"], "restore_epoch")
	for _, m := range r.entries(top["counters"], "counters", validCounter) {
		f.Counters[m.Key] = r.integer(m.Val, "counters."+m.Key)
	}
	for _, m := range r.entries(top["members"], "members", validHandle) {
		f.Members[m.Key] = r.member(m.Val, "members."+m.Key)
	}
	for _, m := range r.entries(top["humans"], "humans", proto.ValidHumanName) {
		f.Humans[m.Key] = r.human(m.Val, "humans."+m.Key)
	}
	f.Coordinator = r.str(top["coordinator"], "coordinator", true, validHandle)
	f.CoordinatorWanted = r.str(top["coordinator_wanted"], "coordinator_wanted", true, validHandle)
	if p := top["pending"]; p.Kind() != proto.KindNull {
		f.Pending = r.pending(p)
	}
	return f, r.err
}

// entries are a map's members, each key passing valid.
func (r *reader) entries(v proto.Value, path string, valid func(string) bool) []proto.Member {
	if v.Kind() != proto.KindObject {
		r.fail(path, "is not an object")
		return nil
	}
	for _, m := range v.Members() {
		if !valid(m.Key) {
			r.fail(path, "has a key that is not valid")
		}
	}
	return v.Members()
}

var memberKeys = []string{"state", "kind", "human_text", keySince, "cred", "cred_seq", "cred_ms", "needs"}

func (r *reader) member(v proto.Value, path string) Member {
	m := r.object(v, path, memberKeys)
	return Member{
		State:     r.str(m["state"], path+".state", false, oneOf(MemberActive, MemberParked, MemberRetired, MemberLost, MemberClaimed)),
		Kind:      r.str(m["kind"], path+".kind", false, kindRe.MatchString),
		HumanText: r.boolean(m["human_text"], path+".human_text"),
		SinceMS:   r.integer(m["since_ms"], path+".since_ms"),
		Cred:      r.str(m["cred"], path+".cred", true, oneOf(credPassword, credToken)),
		CredSeq:   r.integer(m["cred_seq"], path+".cred_seq"),
		CredMS:    r.integer(m["cred_ms"], path+".cred_ms"),
		Needs:     r.str(m["needs"], path+".needs", true, oneOf(needsReissue)),
	}
}

func (r *reader) human(v proto.Value, path string) Human {
	m := r.object(v, path, []string{"state", keySince, "revoked_seq", "needs"})
	return Human{
		State:      r.str(m["state"], path+".state", false, oneOf(HumanActive, HumanRemoved)),
		SinceMS:    r.integer(m["since_ms"], path+".since_ms"),
		RevokedSeq: r.integer(m["revoked_seq"], path+".revoked_seq"),
		Needs:      r.str(m["needs"], path+".needs", true, oneOf(needsPass)),
	}
}

// pending reads `pending`: a member operation (`member.<verb>`) carries its handle, any other
// does not.
func (r *reader) pending(v proto.Value) *Pending {
	m := r.object(v, "pending", []string{"op", "step", keySince}, "handle")
	p := &Pending{
		Op:      r.str(m["op"], "pending.op", false, func(s string) bool { return s != "" && !strings.ContainsFunc(s, isControl) }),
		Step:    r.str(m["step"], "pending.step", false, func(s string) bool { return !strings.ContainsFunc(s, isControl) }),
		SinceMS: r.integer(m["since_ms"], "pending.since_ms"),
	}
	_, hasHandle := m["handle"]
	switch member := strings.HasPrefix(p.Op, "member."); {
	case member && hasHandle:
		p.Handle = r.str(m["handle"], "pending.handle", false, validHandle)
	case member != hasHandle:
		r.fail("pending.handle", "is a member operation's only, and required by one")
	}
	return p
}

func isControl(c rune) bool { return c < 0x20 || c == 0x7f }

// EncodeRooms is `ctl/<team>/rooms.json` (AP3): each room ID only once known.
func EncodeRooms(team paths.TeamID, ids proto.RoomIDs) []byte {
	members := []proto.Member{proto.M("v", proto.Int(fileVersion)), proto.M("team", proto.Str(team.String()))}
	for _, room := range []struct{ key, id string }{{"main", ids.Main}, {"reports", ids.Reports}, {"questions", ids.Questions}} {
		if room.id != "" {
			members = append(members, proto.M(room.key, proto.Str(room.id)))
		}
	}
	return proto.Obj(members...).Compact()
}

// DecodeRooms reads team's rooms record strictly (AP3).
func DecodeRooms(raw []byte, team paths.TeamID) (proto.RoomIDs, error) {
	v, err := parseFile(raw, "rooms.json")
	if err != nil {
		return proto.RoomIDs{}, err
	}
	r := &reader{file: "rooms.json"}
	top := r.object(v, "rooms", []string{"v", "team"}, "main", "reports", "questions")
	r.header(top, team)
	var ids proto.RoomIDs
	for key, at := range map[string]*string{"main": &ids.Main, "reports": &ids.Reports, "questions": &ids.Questions} {
		if val, ok := top[key]; ok {
			*at = r.str(val, key, false, proto.ValidRoomID)
		}
	}
	if r.err == nil && duplicate(ids) {
		r.fail("rooms", "names one room for two purposes")
	}
	return ids, r.err
}

func duplicate(ids proto.RoomIDs) bool {
	seen := map[string]bool{}
	for _, id := range []string{ids.Main, ids.Reports, ids.Questions} {
		if id != "" && seen[id] {
			return true
		}
		seen[id] = true
	}
	return false
}

// store is one team's two files, loaded once and written through the atomic writer; every write
// of the roster raises its seq (AP2).
type store struct {
	team                  paths.TeamID
	rosterPath, roomsPath string
	roster                RosterFile
	rooms                 proto.RoomIDs
}

func (s *store) load() error {
	raw, found, err := state.ReadFile(s.rosterPath, maxFile)
	switch {
	case err != nil:
		return err
	case found:
		if s.roster, err = DecodeRosterFile(raw, s.team); err != nil {
			return err
		}
	default:
		s.roster = NewRosterFile(s.team)
	}
	raw, found, err = state.ReadFile(s.roomsPath, maxFile)
	if err != nil || !found {
		return err
	}
	s.rooms, err = DecodeRooms(raw, s.team)
	return err
}

func (s *store) saveRoster() error {
	s.roster.Seq++
	if err := state.WriteFile(s.rosterPath, s.roster.Encode(), fileMode, -1); err != nil {
		return errors.Join(err, errors.New("the roster file keeps its last content"))
	}
	return nil
}

func (s *store) saveRooms() error {
	return state.WriteFile(s.roomsPath, EncodeRooms(s.team, s.rooms), fileMode, -1)
}
