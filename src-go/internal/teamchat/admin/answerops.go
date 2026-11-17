package admin

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// The child's read-only operations (C 3.11.2, n2f-D10).
const (
	OpTeamView           = "team.view"
	OpMemberDisconnected = "member.disconnected"
)

// The child's restore operations (RC1-RC8).
const (
	OpRestoreSecure = "restore.secure"
	OpRestoreCheck  = "restore.check"
	OpRestoreSwap   = "restore.swap"
	OpRestoreExport = "restore.export"
	OpRestoreFinish = "restore.finish"
	OpRestoreRevert = "restore.revert"
)

// The answer's keys for them, and for the main room beside a credential (BU1).
const (
	keyMainRoom = "main_room"
	keyView     = "view"
	keyNotice   = "notice"
	keyExport   = "export"
	keyEpoch    = "epoch"
	keyHuman    = "human"
)

// The bounds of a human answer's devices (HU13).
const (
	maxDevices  = 256
	maxDeviceID = 255
)

// answerFits refuses an ok answer the op cannot give (CO3, RC6, SC-C1 to SC-C3): a view answers
// team.view, a notice member.disconnected, an export restore.export, an epoch restore.check,
// carry.tail and carry.check, a tail carry.tail and a found list carry.check, each always and
// only there; a restore, human or carry op answers no handle, credential, main room or notes. A
// failure fits any op.
func answerFits(op string, a Answer) error {
	if !a.OK {
		return nil
	}
	got := []bool{a.View != nil, a.Notice != nil, a.Export != nil, a.Epoch != nil, a.Tail != nil, a.Found != nil}
	want := []bool{
		op == OpTeamView, op == OpMemberDisconnected, op == OpRestoreExport,
		slices.Contains([]string{OpRestoreCheck, OpCarryTail, OpCarryCheck}, op), op == OpCarryTail, op == OpCarryCheck,
	}
	if !slices.Equal(got, want) {
		return fmt.Errorf("its answer to %s does not fit the op (a view, a notice, an export, an epoch, a tail, a found list: %v)", op, got)
	}
	if slices.ContainsFunc([]string{"restore.", "human.", "carry."}, func(p string) bool { return strings.HasPrefix(op, p) }) &&
		(a.Handle != "" || a.Credential != nil || a.MainRoom != "" || len(a.Notes) > 0) {
		return fmt.Errorf("its answer to %s carries a handle, a credential, a main room or notes", op)
	}
	return humanFits(op, a)
}

// humanFits is HU13's rule: a human answer answers human.password (a password, no devices) and
// human.devices (devices, no password), always and only there.
func humanFits(op string, a Answer) error {
	password, devices := op == apply.OpHumanPassword, op == apply.OpHumanDevices
	if (a.Human != nil) != (password || devices) {
		return fmt.Errorf("its answer to %s does not fit the op (a human answer: %v)", op, a.Human != nil)
	}
	if password && (a.Human.Password == "" || a.Human.Devices != nil) || devices && a.Human.Password != "" {
		return fmt.Errorf("its human answer to %s is not that op's", op)
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
	if a.Human != nil {
		return a.validateHuman()
	}
	if a.restoreOrCarry() {
		return a.validateRestoreOrCarry()
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

// validateHuman is a human answer (HU13): of its own, a password of printable ASCII without a
// space, or at most maxDevices distinct device IDs of 1 to maxDeviceID such bytes. No error
// quotes the password.
func (a Answer) validateHuman() error {
	if a.Handle != "" || a.Credential != nil || len(a.Notes) > 0 || a.View != nil || a.Notice != nil || a.Export != nil || a.Epoch != nil ||
		a.Tail != nil || a.Found != nil {
		return errors.New("a human answer is an answer of its own")
	}
	h := a.Human
	if h.Password != "" && (h.Devices != nil || !printableWord(h.Password, maxSecret)) {
		return fmt.Errorf("a human answer's password is not 1 to %d printable ASCII bytes without a space, alone", maxSecret)
	}
	return validDevices(h.Devices)
}

// validDevices is at most maxDevices distinct IDs of 1 to maxDeviceID printable bytes, each
// seen at a time from 0.
func validDevices(devices []matrix.Device) error {
	if len(devices) > maxDevices {
		return fmt.Errorf("a human answer holds more than %d devices", maxDevices)
	}
	seen := map[string]bool{}
	for _, d := range devices {
		if !printableWord(d.ID, maxDeviceID) || seen[d.ID] || !d.LastSeen.IsZero() && d.LastSeen.UnixMilli() < 0 {
			return fmt.Errorf("a device is not a distinct ID of 1 to %d printable bytes seen at a time from 0", maxDeviceID)
		}
		seen[d.ID] = true
	}
	return nil
}

// printableWord is 1 to most printable ASCII bytes without a space.
func printableWord(s string, most int) bool {
	return s != "" && len(s) <= most && !strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || r > '~' })
}

// encodeDevices is HU5's devices: each ID, and its last sighting in milliseconds or null.
func encodeDevices(devices []matrix.Device) proto.Value {
	items := make([]proto.Value, 0, len(devices))
	for _, d := range devices {
		seen := proto.Null()
		if !d.LastSeen.IsZero() {
			seen = proto.Int(d.LastSeen.UnixMilli())
		}
		items = append(items, proto.Obj(proto.M("id", proto.Str(d.ID)), proto.M("last_seen_ms", seen)))
	}
	return proto.Arr(items...)
}

// decodeHumanAnswer reads a human answer: exactly one of password and devices.
func decodeHumanAnswer(v proto.Value) (*HumanAnswer, error) {
	if pw, given := v.Get("password"); given {
		if _, err := exactKeys(v, "password"); err != nil || pw.Kind() != proto.KindString {
			return nil, errors.New("human: a password answer is one string, alone")
		}
		return &HumanAnswer{Password: pw.Str()}, nil
	}
	obj, err := exactKeys(v, "devices")
	if err != nil || obj["devices"].Kind() != proto.KindArray {
		return nil, errors.New("human: not a password or an array of devices")
	}
	devices := []matrix.Device{}
	for _, item := range obj["devices"].Items() {
		f, err := exactKeys(item, "id", "last_seen_ms")
		if err != nil || f["id"].Kind() != proto.KindString {
			return nil, errors.New("human: a device is not an id and a last_seen_ms")
		}
		d := matrix.Device{ID: f["id"].Str()}
		seen := f["last_seen_ms"]
		if seen.Kind() == proto.KindInt {
			d.LastSeen = time.UnixMilli(seen.Int()).UTC()
		} else if seen.Kind() != proto.KindNull {
			return nil, errors.New("human: a device's last_seen_ms is not a number or null")
		}
		devices = append(devices, d)
	}
	return &HumanAnswer{Devices: devices}, nil
}

// validateRestore is an export's or an epoch's answer: of its own, with sane numbers (RC6).
func (a Answer) validateRestore() error {
	if a.Export != nil && a.Epoch != nil || a.Handle != "" || a.Credential != nil || len(a.Notes) > 0 || a.View != nil || a.Notice != nil {
		return errors.New("an export or an epoch is an answer of its own")
	}
	if e := a.Export; e != nil {
		if e.Count < 0 || e.FirstMS < 0 || e.FirstMS > e.EndMS || (e.Count == 0) != (e.FirstMS == 0) {
			return errors.New("an export's count and times are not a count, the oldest event's time and an end time after it")
		}
		return nil
	}
	if a.Epoch.RestoreEpoch < 0 || a.Epoch.Rev < 1 {
		return errors.New("an epoch is not a restore epoch of 0 or more and a rev of 1 or more")
	}
	return nil
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
		Homeserver: status.Homeserver{Unit: systemd.ActiveStateActive, Probe: status.ProbeOK}, Backup: status.Backup{LastRun: status.RunNone},
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
	if e := a.Export; e != nil {
		out = append(out, proto.M(keyExport, proto.Obj(
			proto.M("count", proto.Int(e.Count)), proto.M("first_ms", proto.Int(e.FirstMS)),
			proto.M("end_ms", proto.Int(e.EndMS)), proto.M("truncated", proto.Bool(e.Truncated)),
		)))
	}
	if e := a.Epoch; e != nil {
		out = append(out, proto.M(keyEpoch, proto.Obj(proto.M("restore_epoch", proto.Int(e.RestoreEpoch)), proto.M("rev", proto.Int(e.Rev)))))
	}
	out = append(out, a.encodeCarry()...)
	if h := a.Human; h != nil {
		if h.Password != "" {
			out = append(out, proto.M(keyHuman, proto.Obj(proto.M("password", proto.Str(h.Password)))))
		} else {
			out = append(out, proto.M(keyHuman, proto.Obj(proto.M("devices", encodeDevices(h.Devices)))))
		}
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
		fields := []proto.Member{proto.M("what", proto.Str(p.What)), proto.M("fix", proto.Str(p.Fix))}
		if p.Member != "" {
			fields = append(fields, proto.M("member", proto.Str(p.Member)))
		}
		problems = append(problems, proto.Obj(fields...))
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
	if v, given := obj[keyHuman]; given {
		h, err := decodeHumanAnswer(v)
		if err != nil {
			return err
		}
		a.Human = h
	}
	if err := decodeCarry(a, obj); err != nil {
		return err
	}
	return decodeRestore(a, obj)
}

// decodeRestore reads into a the export and epoch an ok answer's object holds (RC6).
func decodeRestore(a *Answer, obj map[string]proto.Value) error {
	if v, given := obj[keyExport]; given {
		f, err := newFields(v, "count", "first_ms", "end_ms", "truncated")
		if err != nil {
			return fmt.Errorf("export: %w", err)
		}
		e := restore.Export{Count: f.int("count"), FirstMS: f.int("first_ms"), EndMS: f.int("end_ms"), Truncated: f.bool("truncated")}
		if err := f.err(); err != nil {
			return fmt.Errorf("export: %w", err)
		}
		a.Export = &e
	}
	if v, given := obj[keyEpoch]; given {
		f, err := newFields(v, "restore_epoch", "rev")
		if err != nil {
			return fmt.Errorf("epoch: %w", err)
		}
		e := restore.Epoch{RestoreEpoch: f.int("restore_epoch"), Rev: f.int("rev")}
		if err := f.err(); err != nil {
			return fmt.Errorf("epoch: %w", err)
		}
		a.Epoch = &e
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

func (f *fields) bool(key string) bool {
	if v := f.obj[key]; v.Kind() == proto.KindBool {
		return v.Bool()
	}
	f.errs = append(f.errs, fmt.Errorf("%s is not a boolean", key))
	return false
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

// decodeProblem is one problem; `member`, when there, is a non-empty handle that validView then
// requires among the view's members.
func decodeProblem(v proto.Value) (status.Problem, error) {
	obj, err := keys(v, []string{"what", "fix"}, "member")
	if err != nil {
		return status.Problem{}, fmt.Errorf("a problem: %w", err)
	}
	f := &fields{obj: obj}
	p := status.Problem{What: f.str("what"), Fix: f.str("fix")}
	if _, named := obj["member"]; named {
		if p.Member = f.str("member"); p.Member == "" {
			f.errs = append(f.errs, errors.New("member is empty"))
		}
	}
	return p, f.err()
}
