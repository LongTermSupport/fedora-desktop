package admin

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// The child's read-only operations (C 3.11.2, n2f-D10).
const (
	OpTeamView           = "team.view"
	OpMemberDisconnected = "member.disconnected"
)

// The answer's keys for them, and for the main room beside a credential (BU1).
const (
	keyMainRoom = "main_room"
	keyView     = "view"
	keyNotice   = "notice"
)

// answerFits refuses an ok answer the op cannot give (CO3): a view answers team.view and a
// notice member.disconnected, each always and only there. A failure fits any op.
func answerFits(op string, a Answer) error {
	if !a.OK {
		return nil
	}
	if (a.View != nil) != (op == OpTeamView) || (a.Notice != nil) != (op == OpMemberDisconnected) {
		return fmt.Errorf("its answer to %s does not fit the op (a view: %v, a notice: %v)", op, a.View != nil, a.Notice != nil)
	}
	return nil
}

// validateOps is an ok answer's main room, view and notice (BU1, CO3).
func (a Answer) validateOps() error {
	if (a.MainRoom != "") != (a.Credential != nil) {
		return errors.New("an answer carries the main room exactly when it carries a credential")
	}
	if a.MainRoom != "" && !proto.ValidRoomID(a.MainRoom) {
		return errors.New("the main room is not a room ID")
	}
	if a.View == nil && a.Notice == nil {
		return nil
	}
	if a.View != nil && a.Notice != nil || a.Handle != "" || a.Credential != nil || len(a.Notes) > 0 {
		return errors.New("a view or a notice is an answer of its own")
	}
	if a.Notice != nil {
		return validNotice(*a.Notice)
	}
	return validView(*a.View)
}

// validNotice is a notice posted with no why, or not posted with a why of one line.
func validNotice(n health.Notice) error {
	if n.Posted != (n.Why == "") || !n.Posted && !validLine(n.Why) {
		return errors.New("a notice is posted, or not posted with one line of why")
	}
	return nil
}

// validView is SR1's value rules: the view must make a status record status.Encode accepts,
// with each LXC member's online (root's, from the guest probe) not yet known.
func validView(v health.View) error {
	team, err := paths.ParseTeamID("view")
	if err != nil {
		return err
	}
	members := slices.Clone(v.Members)
	for i := range members {
		if members[i].Online != nil {
			return errors.New("a view's member carries online, which is root's to know")
		}
		if members[i].Kind == status.KindLXC {
			members[i].Online = new(bool)
		}
	}
	rec := status.Record{
		V: 1, Team: team, Name: "view", Origin: status.OriginV2, ExpiresMS: status.Lifetime.Milliseconds(),
		Homeserver: status.Homeserver{Unit: "active", Probe: status.ProbeOK}, Backup: status.Backup{LastRun: status.RunNone},
		Members: members, Humans: v.Humans, Problems: v.Problems,
	}
	if _, err := rec.Encode(); err != nil {
		return fmt.Errorf("the view: %w", err)
	}
	return nil
}

// encodeOps is the answer's main room, view and notice on the wire.
func (a Answer) encodeOps() []proto.Member {
	var out []proto.Member
	if a.MainRoom != "" {
		out = append(out, proto.M(keyMainRoom, proto.Str(a.MainRoom)))
	}
	if a.View != nil {
		out = append(out, proto.M(keyView, encodeView(*a.View)))
	}
	if n := a.Notice; n != nil {
		out = append(out, proto.M(keyNotice, proto.Obj(proto.M("posted", proto.Bool(n.Posted)), proto.M("why", proto.Str(n.Why)))))
	}
	return out
}

// encodeView is the view in the status record's own encoding (C 3.11.3), online left out.
func encodeView(v health.View) proto.Value {
	members := make([]proto.Value, 0, len(v.Members))
	for _, m := range v.Members {
		members = append(members, proto.Obj(
			proto.M("handle", proto.Str(m.Handle)), proto.M("kind", proto.Str(m.Kind)), proto.M("role", proto.Str(m.Role)),
			proto.M("state", proto.Str(m.State)), proto.M("rooms", encodeRooms(m.Rooms)),
		))
	}
	humans := make([]proto.Value, 0, len(v.Humans))
	for _, h := range v.Humans {
		humans = append(humans, proto.Obj(
			proto.M("name", proto.Str(h.Name)), proto.M("state", proto.Str(h.State)), proto.M("rooms", encodeRooms(h.Rooms)),
			proto.M("devices", proto.Int(h.Devices)), proto.M("last_seen_ms", proto.Int(h.LastSeenMS)),
		))
	}
	problems := make([]proto.Value, 0, len(v.Problems))
	for _, p := range v.Problems {
		problems = append(problems, proto.Obj(proto.M("what", proto.Str(p.What)), proto.M("fix", proto.Str(p.Fix))))
	}
	return proto.Obj(proto.M("members", proto.Arr(members...)), proto.M("humans", proto.Arr(humans...)), proto.M("problems", proto.Arr(problems...)))
}

func encodeRooms(rooms map[string]string) proto.Value {
	members := make([]proto.Member, 0, len(rooms))
	for _, purpose := range slices.Sorted(maps.Keys(rooms)) {
		members = append(members, proto.M(purpose, proto.Str(rooms[purpose])))
	}
	return proto.Obj(members...)
}

// decodeOps reads into a the main room, view and notice an ok answer's object holds.
func decodeOps(a *Answer, obj map[string]proto.Value) error {
	if v, given := obj[keyMainRoom]; given {
		if v.Kind() != proto.KindString {
			return errors.New("main_room is not a string")
		}
		a.MainRoom = v.Str()
	}
	if v, given := obj[keyView]; given {
		view, err := decodeView(v)
		if err != nil {
			return fmt.Errorf("view: %w", err)
		}
		a.View = &view
	}
	if v, given := obj[keyNotice]; given {
		n, err := exactKeys(v, "posted", "why")
		if err != nil {
			return fmt.Errorf("notice: %w", err)
		}
		if n["posted"].Kind() != proto.KindBool || n["why"].Kind() != proto.KindString {
			return errors.New("notice: posted is not a boolean or why not a string")
		}
		a.Notice = &health.Notice{Posted: n["posted"].Bool(), Why: n["why"].Str()}
	}
	return nil
}

func decodeView(v proto.Value) (health.View, error) {
	obj, err := exactKeys(v, "members", "humans", "problems")
	if err != nil {
		return health.View{}, err
	}
	members, merr := items(obj["members"], decodeMember)
	humans, herr := items(obj["humans"], decodeHuman)
	problems, perr := items(obj["problems"], decodeProblem)
	return health.View{Members: members, Humans: humans, Problems: problems}, errors.Join(merr, herr, perr)
}

// items is v's array, each item read by one.
func items[T any](v proto.Value, one func(proto.Value) (T, error)) ([]T, error) {
	if v.Kind() != proto.KindArray {
		return nil, errors.New("not an array")
	}
	out := make([]T, 0, len(v.Items()))
	for _, item := range v.Items() {
		got, err := one(item)
		if err != nil {
			return nil, err
		}
		out = append(out, got)
	}
	return out, nil
}

// fields is an object's exact keys, each a string or an integer as kinds says.
type fields struct {
	obj  map[string]proto.Value
	errs []error
}

func newFields(v proto.Value, want ...string) (*fields, error) {
	obj, err := exactKeys(v, want...)
	return &fields{obj: obj}, err
}

func (f *fields) str(key string) string {
	if v := f.obj[key]; v.Kind() == proto.KindString {
		return v.Str()
	}
	f.errs = append(f.errs, fmt.Errorf("%s is not a string", key))
	return ""
}

func (f *fields) int(key string) int64 {
	if v := f.obj[key]; v.Kind() == proto.KindInt {
		return v.Int()
	}
	f.errs = append(f.errs, fmt.Errorf("%s is not an integer", key))
	return 0
}

func (f *fields) rooms() map[string]string {
	v := f.obj["rooms"]
	if v.Kind() != proto.KindObject {
		f.errs = append(f.errs, errors.New("rooms is not an object"))
		return nil
	}
	out := make(map[string]string, len(v.Members()))
	for _, m := range v.Members() {
		if m.Val.Kind() != proto.KindString {
			f.errs = append(f.errs, errors.New("a room membership is not a string"))
		}
		out[m.Key] = m.Val.Str()
	}
	return out
}

func (f *fields) err() error { return errors.Join(f.errs...) }

func decodeMember(v proto.Value) (status.Member, error) {
	f, err := newFields(v, "handle", "kind", "role", "state", "rooms")
	if err != nil {
		return status.Member{}, fmt.Errorf("a member: %w", err)
	}
	m := status.Member{Handle: f.str("handle"), Kind: f.str("kind"), Role: f.str("role"), State: f.str("state"), Rooms: f.rooms()}
	return m, f.err()
}

func decodeHuman(v proto.Value) (status.Human, error) {
	f, err := newFields(v, "name", "state", "rooms", "devices", "last_seen_ms")
	if err != nil {
		return status.Human{}, fmt.Errorf("a human: %w", err)
	}
	h := status.Human{Name: f.str("name"), State: f.str("state"), Rooms: f.rooms(), Devices: f.int("devices"), LastSeenMS: f.int("last_seen_ms")}
	return h, f.err()
}

func decodeProblem(v proto.Value) (status.Problem, error) {
	f, err := newFields(v, "what", "fix")
	if err != nil {
		return status.Problem{}, fmt.Errorf("a problem: %w", err)
	}
	p := status.Problem{What: f.str("what"), Fix: f.str("fix")}
	return p, f.err()
}
