package declare

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/duty"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// A team's `duties` (PROPOSAL-seat-kinds.md, OD1-OD5): a sparse map of member handle to duty,
// overriding the default duty.Of derives. The coordinator's duty is derived from `coordinator`
// alone (C30: one truth per fact), so `duties` never names the coordinator and never gives the
// coordinator duty; a host member is never the coordinator (OD1). The field is gated (OD3).

const keyDuties = "duties"

// maxDuties caps a team's `duties` entries.
const maxDuties = 256

// dutySep joins a handle and its duty in a gate entry, `<handle>=<duty>` (AG3, AG6).
const dutySep = "="

const (
	problemHostCoordinator = "is a host member, which is never the coordinator"
	problemNotHandle       = "is not an agent handle"
)

// coordinatorProblem is why handle cannot be the team's coordinator, "" when it can.
func coordinatorProblem(handle string) string {
	if duty.HostHandle(handle) {
		return problemHostCoordinator
	}
	return ""
}

// dutyProblem is why `duties` may not give handle the duty d in a team whose coordinator is
// coordinator, "" when it may.
func dutyProblem(coordinator, handle string, d duty.Duty) string {
	switch {
	case !validHandle(handle):
		return problemNotHandle
	case handle == coordinator:
		return "is the team's coordinator, whose duty follows from coordinator and is not declared here"
	case d == duty.Coordinator && duty.HostHandle(handle):
		return problemHostCoordinator
	case d == duty.Coordinator:
		return "is given coordinator, which only the team's coordinator holds; name it in coordinator"
	}
	return ""
}

// dutiesMsg is an unknown duty's refusal.
const dutiesMsg = "is not a duty: coordinator, worker or host"

// duties reads n as a `duties` object: at most maxDuties agent handles, each to a duty; nil for
// an empty one. The rules that need the coordinator are checkDuties'.
func (n node) duties() (map[string]duty.Duty, error) {
	if n.v.Kind() != proto.KindObject {
		return nil, n.fail("is not an object")
	}
	members := n.v.Members()
	if len(members) > maxDuties {
		return nil, n.fail("holds more than " + strconv.Itoa(maxDuties) + " entries")
	}
	var out map[string]duty.Duty
	for _, m := range members {
		at := node{path: n.key(m.Key).path, v: m.Val}
		if !validHandle(m.Key) {
			return nil, at.fail(problemNotHandle)
		}
		s, err := at.str()
		if err != nil {
			return nil, err
		}
		d, ok := duty.Parse(s)
		if !ok {
			return nil, at.fail(dutiesMsg)
		}
		if out == nil {
			out = map[string]duty.Duty{}
		}
		out[m.Key] = d
	}
	return out, nil
}

// checkDuties holds a team's coordinator and duties to each other (OD1, C30).
func checkDuties(coordinator string, duties map[string]duty.Duty, f fields) error {
	if p := coordinatorProblem(coordinator); coordinator != "" && p != "" {
		return f.at(keyCoordinator).fail(p)
	}
	for _, h := range slices.Sorted(maps.Keys(duties)) {
		if p := dutyProblem(coordinator, h, duties[h]); p != "" {
			return f.at(keyDuties).key(h).fail(p)
		}
	}
	return nil
}

// DutyOf is handle's duty in the team: its `duties` entry, else the default (duty.Of).
func (t Team) DutyOf(handle string) duty.Duty {
	return duty.Of(handle, t.Coordinator, t.Duties)
}

// dutiesValue is duties as a JSON object, keys in order.
func dutiesValue(duties map[string]duty.Duty) proto.Value {
	members := make([]proto.Member, 0, len(duties))
	for _, h := range slices.Sorted(maps.Keys(duties)) {
		members = append(members, proto.M(h, proto.Str(string(duties[h]))))
	}
	return proto.Obj(members...)
}

// dutyEntries are duties as AG3 prints them, `<handle>=<duty>`, sorted.
func dutyEntries(duties map[string]duty.Duty) []string {
	out := make([]string, 0, len(duties))
	for _, h := range slices.Sorted(maps.Keys(duties)) {
		out = append(out, h+dutySep+string(duties[h]))
	}
	return out
}

// changeDuties adds an entry for a handle with none, removes the entry given, or sets the duty of
// a handle that has another (AG6), by the rules of a declared entry.
func changeDuties(cur map[string]duty.Duty, coordinator string, ch Change) (map[string]duty.Duty, error) {
	handle, text, cut := strings.Cut(ch.Value, dutySep)
	d, ok := duty.Parse(text)
	if !cut || !ok || dutyProblem(coordinator, handle, d) != "" {
		return nil, badValue(ch.Field)
	}
	was, held := cur[handle]
	out := maps.Clone(cur)
	if out == nil {
		out = map[string]duty.Duty{}
	}
	switch {
	case ch.Op == OpAdd && !held, ch.Op == OpSet && held && was != d:
		out[handle] = d
	case ch.Op == OpRemove && held && was == d:
		delete(out, handle)
	case ch.Op != OpAdd && ch.Op != OpRemove && ch.Op != OpSet:
		return nil, badOp(ch.Field, "--add, --remove or --set")
	default:
		return nil, noChange(ch.Field)
	}
	if len(out) == 0 {
		out = nil
	}
	return out, nil
}
