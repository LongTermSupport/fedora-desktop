package carry

import (
	"fmt"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// recordVersion is every carry record's `v`.
const recordVersion = 1

// maxRecord caps a carry record: a tail of every room with its seal.
const maxRecord = 1 << 20

// SealStep is where a seal stands (SC3, SC7).
type SealStep string

// The seal's steps.
const (
	StepSealing SealStep = "sealing"
	StepSealed  SealStep = "sealed"
)

// SealRecord is sealed.json. At StepSealing it holds the tail read before any stop and the
// time the seal started; at StepSealed, the whole seal (SC7).
type SealRecord struct {
	Team      paths.TeamID
	Step      SealStep
	StartedMS int64
	Tail      []TailEntry
	Seal      SealFacts
}

// DrillStep is where a drill export stands (SD2).
type DrillStep string

// StepStopping is the drill's one step: recorded before any stop, removed once it serves again.
const StepStopping DrillStep = "stopping"

// DrillRecord is drill.json (SD2).
type DrillRecord struct {
	Team      paths.TeamID
	Step      DrillStep
	StartedMS int64
	Tail      []TailEntry
}

// CarriedRecord is carried.json, which an import leaves in the team's admin state (SI9 step 4),
// with the seal's tail, which team carry-check finds again (section 6, sc-D13).
type CarriedRecord struct {
	Team                 paths.TeamID
	Kind                 Kind
	SHA256               string
	SealedMS, ImportedMS int64
	Roster               RosterFacts
	Tail                 []TailEntry
}

// ImportStep is where an import stands (SI8, SI9).
type ImportStep string

// The import's steps.
const (
	ImportStaging ImportStep = "staging"
	ImportStaged  ImportStep = "staged"
	ImportPlacing ImportStep = "placing"
)

// ImportRecord is CarryImportRecord(t): the archive's kind and digest, the step, and each root
// placed so far, appended before its rename so a resume or a discard knows what is its own.
type ImportRecord struct {
	Team   paths.TeamID
	Kind   Kind
	SHA256 string
	Step   ImportStep
	Placed []string
}

// PlacedRoots are the roots an import places, in SI9's order: the homeserver tree, the ctl
// state, the status directory and, last, the admin state.
func PlacedRoots(t paths.TeamID) []string {
	return []string{paths.HomeserverPrivateState(t), paths.CtlState(t), paths.StatusTeamDir(t), paths.AdminState(t)}
}

func tailValue(tail []TailEntry) proto.Value {
	items := make([]proto.Value, 0, len(tail))
	for _, e := range tail {
		items = append(items, proto.Obj(proto.M("room", proto.Str(e.Room)), proto.M("event_id", proto.Str(e.EventID)), proto.M("ts_ms", proto.Int(e.TsMS))))
	}
	return proto.Arr(items...)
}

func rosterValue(r RosterFacts) proto.Value {
	return proto.Obj(proto.M("seq", proto.Int(r.Seq)), proto.M("rev", proto.Int(r.Rev)), proto.M("restore_epoch", proto.Int(r.RestoreEpoch)))
}

func head(t paths.TeamID) []proto.Member {
	return []proto.Member{proto.M("v", proto.Int(recordVersion)), proto.M(keyTeam, proto.Str(t.String()))}
}

// EncodeSealRecord is sealed.json's canonical JSON at its step.
func EncodeSealRecord(r SealRecord) []byte { return proto.Obj(sealMembers(r)...).Compact() }

// sealMembers are sealed.json's members at its step, in their canonical order.
func sealMembers(r SealRecord) []proto.Member {
	members := append(head(r.Team), proto.M(keyStep, proto.Str(string(r.Step))))
	if r.Step == StepSealed {
		return append(members, encodeSealFields(KindSealed, r.Seal)...)
	}
	return append(members, proto.M(keyTail, tailValue(r.Tail)), proto.M("started_ms", proto.Int(r.StartedMS)))
}

// DecodeSealRecord reads team's sealed.json strictly.
func DecodeSealRecord(raw []byte, team paths.TeamID) (SealRecord, error) {
	const file = "sealed.json"
	v, rd, err := parseRecord(raw, file)
	if err != nil {
		return SealRecord{}, err
	}
	at, found := v.Get(keyStep)
	if !found {
		return SealRecord{}, fmt.Errorf("%s: step is required", file)
	}
	step := SealStep(rd.str(at, keyStep, func(s string) bool { return s == string(StepSealing) || s == string(StepSealed) }))
	r := SealRecord{Team: team, Step: step}
	switch step {
	case StepSealed:
		var version, id, stepValue proto.Value
		r.Seal = decodeSeal(rd, v, KindSealed, "record", map[string]*proto.Value{"v": &version, keyTeam: &id, keyStep: &stepValue})
		checkHead(rd, version, id, team)
	case StepSealing:
		f := rd.object(v, "record", []string{"v", keyTeam, keyStep, keyTail, "started_ms"})
		checkHead(rd, f["v"], f[keyTeam], team)
		r.Tail = decodeTail(rd, f[keyTail], keyTail)
		r.StartedMS = rd.integer(f["started_ms"], "started_ms", 1, maxNumber)
	}
	if rd.err != nil {
		return SealRecord{}, rd.err
	}
	return r, nil
}

// EncodeDrillRecord is drill.json's canonical JSON.
func EncodeDrillRecord(r DrillRecord) []byte {
	return proto.Obj(append(head(r.Team), proto.M(keyStep, proto.Str(string(r.Step))), proto.M(keyTail, tailValue(r.Tail)),
		proto.M("started_ms", proto.Int(r.StartedMS)))...).Compact()
}

// DecodeDrillRecord reads team's drill.json strictly.
func DecodeDrillRecord(raw []byte, team paths.TeamID) (DrillRecord, error) {
	v, rd, err := parseRecord(raw, "drill.json")
	if err != nil {
		return DrillRecord{}, err
	}
	f := rd.object(v, "record", []string{"v", keyTeam, keyStep, keyTail, "started_ms"})
	checkHead(rd, f["v"], f[keyTeam], team)
	r := DrillRecord{
		Team: team, Step: DrillStep(rd.str(f[keyStep], keyStep, func(s string) bool { return s == string(StepStopping) })),
		Tail: decodeTail(rd, f[keyTail], keyTail), StartedMS: rd.integer(f["started_ms"], "started_ms", 1, maxNumber),
	}
	if rd.err != nil {
		return DrillRecord{}, rd.err
	}
	return r, nil
}

// EncodeCarriedRecord is carried.json's canonical JSON.
func EncodeCarriedRecord(r CarriedRecord) []byte {
	return proto.Obj(append(head(r.Team), proto.M(keyKind, proto.Str(string(r.Kind))), proto.M("sha256", proto.Str(r.SHA256)),
		proto.M("sealed_ms", proto.Int(r.SealedMS)), proto.M("imported_ms", proto.Int(r.ImportedMS)), proto.M("roster", rosterValue(r.Roster)),
		proto.M(keyTail, tailValue(r.Tail)))...).Compact()
}

// DecodeCarriedRecord reads team's carried.json strictly.
func DecodeCarriedRecord(raw []byte, team paths.TeamID) (CarriedRecord, error) {
	v, rd, err := parseRecord(raw, "carried.json")
	if err != nil {
		return CarriedRecord{}, err
	}
	f := rd.object(v, "record", []string{"v", keyTeam, keyKind, "sha256", "sealed_ms", "imported_ms", "roster", keyTail})
	checkHead(rd, f["v"], f[keyTeam], team)
	ro := rd.object(f["roster"], "roster", []string{"seq", "rev", "restore_epoch"})
	r := CarriedRecord{
		Team: team, Kind: decodeKind(rd, f[keyKind]), SHA256: rd.str(f["sha256"], "sha256", isDigest),
		SealedMS: rd.integer(f["sealed_ms"], "sealed_ms", 1, maxNumber), ImportedMS: rd.integer(f["imported_ms"], "imported_ms", 1, maxNumber),
		Roster: RosterFacts{rd.integer(ro["seq"], "roster.seq", 0, maxNumber), rd.integer(ro["rev"], "roster.rev", 0, maxNumber), rd.integer(ro["restore_epoch"], "roster.restore_epoch", 0, maxNumber)},
		Tail:   decodeTail(rd, f[keyTail], keyTail),
	}
	if rd.err != nil {
		return CarriedRecord{}, rd.err
	}
	return r, nil
}

// EncodeImportRecord is the import record's canonical JSON.
func EncodeImportRecord(r ImportRecord) []byte {
	placed := make([]proto.Value, 0, len(r.Placed))
	for _, p := range r.Placed {
		placed = append(placed, proto.Str(p))
	}
	return proto.Obj(append(head(r.Team), proto.M(keyKind, proto.Str(string(r.Kind))), proto.M("sha256", proto.Str(r.SHA256)),
		proto.M(keyStep, proto.Str(string(r.Step))), proto.M("placed", proto.Arr(placed...)))...).Compact()
}

// DecodeImportRecord reads team's import record strictly: each placed root is one of
// PlacedRoots, at most once.
func DecodeImportRecord(raw []byte, team paths.TeamID) (ImportRecord, error) {
	v, rd, err := parseRecord(raw, "import record")
	if err != nil {
		return ImportRecord{}, err
	}
	f := rd.object(v, "record", []string{"v", keyTeam, keyKind, "sha256", keyStep, "placed"})
	checkHead(rd, f["v"], f[keyTeam], team)
	r := ImportRecord{
		Team: team, Kind: decodeKind(rd, f[keyKind]), SHA256: rd.str(f["sha256"], "sha256", isDigest),
		Step: ImportStep(rd.str(f[keyStep], keyStep, func(s string) bool {
			return slices.Contains([]ImportStep{ImportStaging, ImportStaged, ImportPlacing}, ImportStep(s))
		})),
	}
	roots := PlacedRoots(team)
	if p := f["placed"]; p.Kind() != proto.KindArray {
		rd.fail("placed", "is not a list")
	} else {
		for _, item := range p.Items() {
			root := rd.str(item, "placed[]", func(s string) bool { return slices.Contains(roots, s) })
			if slices.Contains(r.Placed, root) {
				rd.fail("placed[]", "names a root twice")
			}
			r.Placed = append(r.Placed, root)
		}
	}
	if rd.err != nil {
		return ImportRecord{}, rd.err
	}
	return r, nil
}

func parseRecord(raw []byte, file string) (proto.Value, *reader, error) {
	if len(raw) > maxRecord {
		return proto.Value{}, nil, fmt.Errorf("%s: over %d bytes", file, maxRecord)
	}
	v, err := proto.Parse(raw)
	if err != nil {
		return proto.Value{}, nil, fmt.Errorf("%s is not strict JSON", file)
	}
	return v, &reader{file: file}, nil
}

func checkHead(rd *reader, version, id proto.Value, team paths.TeamID) {
	rd.integer(version, "v", recordVersion, recordVersion)
	rd.str(id, keyTeam, func(s string) bool { return s == team.String() })
}

func decodeKind(rd *reader, v proto.Value) Kind {
	return Kind(rd.str(v, keyKind, func(s string) bool { return s == string(KindSealed) || s == string(KindDrill) }))
}

func isDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
