package restore

import (
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// Step is where a recorded restore stands (section 7).
type Step string

// The record's steps. StepRevert alone may carry Stuck (RS13).
const (
	StepStaging Step = "staging"
	StepStaged  Step = "staged"
	StepSwap    Step = "swap"
	StepStart   Step = "start"
	StepClean   Step = "clean"
	StepRevert  Step = "revert"
)

var steps = []Step{StepStaging, StepStaged, StepSwap, StepStart, StepClean, StepRevert}

// recordVersion is the record's `v`.
const recordVersion = 1

// maxRecord caps restore.json: a roster of 1 MiB with its lists.
const maxRecord = 2 << 20

// maxBackup is the highest backup number the record holds.
const maxBackup = 1<<53 - 1

// Record is root/<team>/restore.json (n2g-D10): what a resume needs, since the tar in memory
// dies with the process. Pre is 0 for null; Stuck is RS16's listing when it refused during a
// revert, "" otherwise. FromBackup is a --roster-from-backup restore (RM11), which RS8's step 2
// needs.
type Record struct {
	Team       paths.TeamID
	Backup     int64
	Offline    bool
	FromBackup bool
	Pre        int64
	Step       Step
	Roster     apply.RosterFile
	Lists      Lists
	Lost       *Lost
	Served     bool
	Stuck      string
}

// Lost is the export's result (RC3): its count, the oldest event kept, where it ended, and
// whether the cap cut it.
type Lost struct {
	Count, FirstMS, EndMS int64
	Truncated             bool
}

// The record's list keys.
const (
	listRetire       = "retire"
	listRevoke       = "revoke"
	listRevokeHumans = "revoke_humans"
	listLock         = "lock"
	listReissue      = "reissue"
	listReturn       = "return"
	listPassword     = "password"
	listUnknown      = "unknown"
	listNotes        = "notes"
)

// listKeys are the record's lists, in their order.
var listKeys = []string{listRetire, listRevoke, listRevokeHumans, listLock, listReissue, listReturn, listPassword, listUnknown, listNotes}

// byName is the list a key names.
func (l *Lists) byName(key string) []string {
	return *l.field(key)
}

func (l *Lists) field(key string) *[]string {
	return map[string]*[]string{
		listRetire: &l.Retire, listRevoke: &l.Revoke, listRevokeHumans: &l.RevokeHumans, listLock: &l.Lock,
		listReissue: &l.Reissue, listReturn: &l.Return, listPassword: &l.Password, listUnknown: &l.Unknown, listNotes: &l.Notes,
	}[key]
}

// listValid is each list's item grammar: handles, human names, or one-line text.
func listValid(key string) func(string) bool {
	switch key {
	case listRetire, listRevoke, listReissue, listReturn:
		return validHandle
	case listRevokeHumans, listLock, listPassword:
		return proto.ValidHumanName
	}
	return status.OneLine
}

func validHandle(s string) bool {
	h, ok := proto.ParseHandle(s)
	return ok && h.String() == s
}

// EncodeRecord is the record's canonical JSON.
func EncodeRecord(r Record) []byte {
	lists := make([]proto.Member, 0, len(listKeys)+1)
	for _, k := range listKeys {
		lists = append(lists, proto.M(k, strArray(r.Lists.byName(k))))
	}
	lists = append(lists, proto.M("pending", nullable(r.Lists.Pending)))
	lost := proto.Null()
	if l := r.Lost; l != nil {
		lost = proto.Obj(proto.M("count", proto.Int(l.Count)), proto.M("first_ms", proto.Int(l.FirstMS)),
			proto.M("end_ms", proto.Int(l.EndMS)), proto.M("truncated", proto.Bool(l.Truncated)))
	}
	roster, err := proto.Parse(r.Roster.Encode())
	if err != nil {
		panic("restore: a roster file's own encoding does not parse: " + err.Error())
	}
	members := []proto.Member{
		proto.M("v", proto.Int(recordVersion)), proto.M("team", proto.Str(r.Team.String())),
		proto.M("backup", proto.Int(r.Backup)), proto.M("offline", proto.Bool(r.Offline)),
		proto.M("from_backup", proto.Bool(r.FromBackup)), proto.M("pre", nullableInt(r.Pre)),
		proto.M("step", proto.Str(string(r.Step))), proto.M("roster", roster), proto.M("lists", proto.Obj(lists...)),
		proto.M("lost", lost), proto.M("served", proto.Bool(r.Served)),
	}
	if r.Stuck != "" {
		members = append(members, proto.M("stuck", proto.Str(r.Stuck)))
	}
	return proto.Obj(members...).Compact()
}

var recordKeys = []string{"v", "team", "backup", "offline", "from_backup", "pre", "step", "roster", "lists", "lost", "served"}

// DecodeRecord reads team's record strictly.
func DecodeRecord(raw []byte, team paths.TeamID) (Record, error) {
	v, err := parse(raw, "restore.json", maxRecord)
	if err != nil {
		return Record{}, err
	}
	rd := &reader{file: "restore.json"}
	top := rd.object(v, "record", recordKeys, "stuck")
	rd.integer(top["v"], "v", recordVersion, recordVersion)
	rd.str(top["team"], "team", false, func(s string) bool { return s == team.String() })
	r := Record{
		Team: team, Backup: rd.integer(top["backup"], "backup", 1, maxBackup),
		Offline: rd.boolean(top["offline"], "offline"), FromBackup: rd.boolean(top["from_backup"], "from_backup"),
		Step:   Step(rd.str(top["step"], "step", false, func(s string) bool { return slices.Contains(steps, Step(s)) })),
		Served: rd.boolean(top["served"], "served"),
	}
	if p := top["pre"]; p.Kind() != proto.KindNull {
		r.Pre = rd.integer(p, "pre", 1, maxBackup)
	}
	if s, ok := top["stuck"]; ok {
		if r.Stuck = rd.str(s, "stuck", false, func(s string) bool { return s != "" && len(s) <= maxLine }); r.Step != StepRevert {
			rd.fail("stuck", "is set beside a step other than revert")
		}
	}
	r.Lists = decodeLists(rd, top["lists"])
	if l := top["lost"]; l.Kind() != proto.KindNull {
		m := rd.object(l, "lost", []string{"count", "first_ms", "end_ms", "truncated"})
		r.Lost = &Lost{
			Count: rd.integer(m["count"], "lost.count", 0, maxBackup), FirstMS: rd.integer(m["first_ms"], "lost.first_ms", 0, maxBackup),
			EndMS: rd.integer(m["end_ms"], "lost.end_ms", 0, maxBackup), Truncated: rd.boolean(m["truncated"], "lost.truncated"),
		}
	}
	if rd.err != nil {
		return Record{}, rd.err
	}
	if r.Roster, err = apply.DecodeRosterFile(top["roster"].Compact(), team); err != nil {
		return Record{}, err
	}
	return r, nil
}

func decodeLists(rd *reader, v proto.Value) Lists {
	var l Lists
	m := rd.object(v, "lists", append(append([]string{}, listKeys...), "pending"))
	for _, k := range listKeys {
		*l.field(k) = rd.strings(m[k], "lists."+k, listValid(k))
	}
	l.Pending = rd.str(m["pending"], "lists.pending", true, status.OneLine)
	return l
}

// maxLine caps one line of text the record holds.
const maxLine = 4096
