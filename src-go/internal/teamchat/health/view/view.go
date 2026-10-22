// Package view is the health tick's read of one team (Plan 00209 SPEC-n2e.md VW1-VW6,
// DESIGN-v2-C-host.md 3.11.2 step 3, DC16), as the `teamchat-ctl` child makes it: read-only, as
// the admin, from `team.json`, the roster file and `rooms.json`. It gives each member's and
// human's membership of each room, the humans' devices, the pool-password watch over every
// active or parked member's account, the drift of every room (the rooms engine run against a
// client that writes nothing), the questions room item by item, and a `pending` operation, each
// a problem with its fix. A problem never carries an error's text, and room state a member can
// write only ever skips an entry.
package view

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/rooms"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// Admin is the part of the matrix admin client the view calls; it calls only its reads.
type Admin interface {
	rooms.Client
	Account(ctx context.Context, userID string) (matrix.Account, bool, error)
	Devices(ctx context.Context, userID string) ([]matrix.Device, error)
}

// Input is what the view reads: the applied team, the roster file and the rooms record.
type Input struct {
	Team   declare.Team
	Roster apply.RosterFile
	Rooms  proto.RoomIDs
}

// maxFile caps each of the three files.
const maxFile = 1 << 20

// ErrOtherTeam is a `team.json` that is not the team's.
var ErrOtherTeam = errors.New("view: team.json is another team's")

// Load reads the three files strictly; a missing roster file or rooms record is empty, as
// `team apply` starts from them.
func Load(team paths.TeamID, teamFile, rosterFile, roomsFile string) (Input, error) {
	var in Input
	raw, found, err := state.ReadFile(teamFile, maxFile)
	if err == nil && !found {
		err = errors.New("view: team.json is missing")
	}
	if err != nil {
		return Input{}, err
	}
	if in.Team, err = declare.DecodeTeamFile(raw); err != nil {
		return Input{}, err
	}
	if in.Team.ID != team {
		return Input{}, ErrOtherTeam
	}
	in.Roster = apply.NewRosterFile(team)
	if raw, found, err = state.ReadFile(rosterFile, maxFile); err == nil && found {
		in.Roster, err = apply.DecodeRosterFile(raw, team)
	}
	if err != nil {
		return Input{}, err
	}
	if raw, found, err = state.ReadFile(roomsFile, maxFile); err == nil && found {
		in.Rooms, err = apply.DecodeRooms(raw, team)
	}
	if err != nil {
		return Input{}, err
	}
	return in, nil
}

// reader is one view being made.
type reader struct {
	a      Admin
	in     Input
	server string
	// rooms is each user's membership by room purpose.
	rooms  map[string]map[string]string
	roster *proto.Roster
	view   health.View
}

// Read is the team's view (VW1-VW6). Only a read the homeserver fails is an error.
func Read(ctx context.Context, a Admin, in Input) (health.View, error) {
	r := &reader{a: a, in: in, server: in.Team.ServerName, rooms: map[string]map[string]string{}}
	steps := []func(context.Context) error{r.memberships, r.humans, r.watch, r.drift, r.questions}
	for _, step := range steps {
		if err := step(ctx); err != nil {
			return health.View{}, err
		}
	}
	if p := in.Roster.Pending; p != nil {
		r.view.Problems = append(r.view.Problems, pendingProblem(in.Team.ID, p))
	}
	return r.view, nil
}

func (r *reader) problem(what, fix string) {
	r.view.Problems = append(r.view.Problems, status.Problem{What: what, Fix: fix})
}

func (r *reader) applyFix() string { return "ccy-teams apply " + r.in.Team.ID.String() }

type known struct{ purpose, id string }

// known are the rooms the record names, in A's order.
func (r *reader) known() []known {
	ids := r.in.Rooms
	all := []known{{proto.PurposeMain, ids.Main}, {proto.PurposeReports, ids.Reports}, {proto.PurposeQuestions, ids.Questions}}
	return slices.DeleteFunc(all, func(k known) bool { return k.id == "" })
}

// memberships reads every known room's m.room.member events (VW1). An event that does not
// read as one of the five memberships is skipped: a member writes its own.
func (r *reader) memberships(ctx context.Context) error {
	for _, room := range r.known() {
		raw, err := r.a.RoomState(ctx, room.id)
		if err != nil {
			return fmt.Errorf("view: the state of room %s: %w", room.purpose, err)
		}
		for _, ev := range raw {
			userID, membership, ok := memberEvent(ev)
			if !ok {
				continue
			}
			if r.rooms[userID] == nil {
				r.rooms[userID] = map[string]string{}
			}
			r.rooms[userID][room.purpose] = membership
		}
	}
	r.view.Members = r.memberList()
	return nil
}

const (
	joined  = "join"
	invited = "invite"
)

var memberships = []string{joined, invited, "leave", "ban", "knock"}

func memberEvent(raw []byte) (string, string, bool) {
	v, err := proto.Parse(raw)
	if err != nil {
		return "", "", false
	}
	key, membership := str(v, "state_key"), str(get(v, "content"), "membership")
	if str(v, "type") != proto.MemberType || key == "" || !slices.Contains(memberships, membership) {
		return "", "", false
	}
	return key, membership, true
}

func get(v proto.Value, name string) proto.Value {
	if v.Kind() == proto.KindObject {
		if f, ok := v.Get(name); ok {
			return f
		}
	}
	return proto.Value{}
}

func str(v proto.Value, name string) string {
	if f := get(v, name); f.Kind() == proto.KindString {
		return f.Str()
	}
	return ""
}

// roomsOf is the local user's memberships, an empty map for none.
func (r *reader) roomsOf(local string) map[string]string {
	if m := r.rooms[proto.UserID(local, r.server)]; m != nil {
		return maps.Clone(m)
	}
	return map[string]string{}
}

func (r *reader) memberList() []status.Member {
	f := r.in.Roster
	out := make([]status.Member, 0, len(f.Members))
	for _, handle := range slices.Sorted(maps.Keys(f.Members)) {
		m := f.Members[handle]
		role := proto.RoleWorker
		if handle == f.Coordinator {
			role = proto.RoleCoordinator
		}
		out = append(out, status.Member{Handle: handle, Kind: m.Kind, Role: role, State: m.State, Rooms: r.roomsOf(handle)})
	}
	return out
}

// humans is each human's rooms, device count and newest sign-in (VW2).
func (r *reader) humans(ctx context.Context) error {
	f := r.in.Roster
	for _, name := range slices.Sorted(maps.Keys(f.Humans)) {
		devices, err := r.a.Devices(ctx, proto.UserID(name, r.server))
		if err != nil {
			return fmt.Errorf("view: the devices of human %s: %w", name, err)
		}
		n, last := signIns(devices)
		r.view.Humans = append(r.view.Humans, status.Human{Name: name, State: f.Humans[name].State, Rooms: r.roomsOf(name), Devices: n, LastSeenMS: last})
	}
	return nil
}

// signIns is the device count and the newest last_seen_ts, 0 for none.
func signIns(devices []matrix.Device) (int64, int64) {
	var last int64
	for _, d := range devices {
		if !d.LastSeen.IsZero() {
			last = max(last, d.LastSeen.UnixMilli())
		}
	}
	return int64(len(devices)), last
}

// watch is the pool-password watch (VW3, DC16) over every active or parked member.
func (r *reader) watch(ctx context.Context) error {
	f := r.in.Roster
	for _, handle := range slices.Sorted(maps.Keys(f.Members)) {
		if s := f.Members[handle].State; s != apply.MemberActive && s != apply.MemberParked {
			continue
		}
		if err := r.account(ctx, handle); err != nil {
			return err
		}
	}
	return nil
}

func (r *reader) account(ctx context.Context, handle string) error {
	id := proto.UserID(handle, r.server)
	acct, found, err := r.a.Account(ctx, id)
	switch {
	case err != nil:
		return fmt.Errorf("view: the account of %s: %w", handle, err)
	case !found:
		r.problem(handle+" has no account", "ccy-teams reissue "+handle)
		return nil
	case acct.Deactivated:
		fix := "teamchat-admin member retire " + handle + " and add a new member"
		if handle == r.in.Roster.Coordinator || handle == r.in.Team.Coordinator {
			fix = "name another coordinator in the declaration, then " + fix
		}
		r.problem(handle+" is deactivated", fix)
		return nil
	}
	devices, err := r.a.Devices(ctx, id)
	if err != nil {
		return fmt.Errorf("view: the devices of %s: %w", handle, err)
	}
	if slices.ContainsFunc(devices, func(d matrix.Device) bool { return d.ID != paths.SeatDevice }) {
		r.problem(handle+" has a device that is not its seat's", "ccy-teams reissue "+handle)
	}
	return nil
}

// errWouldCreate is the dry client's answer to a room the engine would create.
var errWouldCreate = errors.New("the room would be created")

// dry is the rooms engine's client and recorder that writes nothing: every write it is asked
// for is noted, by room purpose.
type dry struct {
	Admin

	purpose map[string]string
	writes  []string
}

func (*dry) CreateRoom(context.Context, matrix.CreateRoomRequest) (string, error) {
	return "", errWouldCreate
}

func (d *dry) PutState(_ context.Context, roomID, eventType, _ string, _ json.RawMessage) (string, error) {
	d.note(roomID, "state "+eventType+" differs")
	return "", nil
}

func (d *dry) Invite(_ context.Context, roomID, userID string) error {
	d.note(roomID, "would invite "+userID)
	return nil
}

func (d *dry) Kick(_ context.Context, roomID, userID, _ string) error {
	d.note(roomID, "would kick "+userID)
	return nil
}

func (*dry) Intent(string) error { return nil }

func (d *dry) Record(purpose, roomID string) error {
	d.purpose[roomID] = purpose
	d.writes = append(d.writes, "room "+purpose+": would adopt "+roomID)
	return nil
}

func (d *dry) note(roomID, what string) {
	d.writes = append(d.writes, "room "+d.purpose[roomID]+": "+what)
}

// drift runs the rooms engine against the dry client (VW4).
func (r *reader) drift(ctx context.Context) error {
	if r.roster = r.derived(); r.roster == nil {
		r.problem("the roster file does not make a valid roster", r.applyFix())
		return nil
	}
	d := &dry{Admin: r.a, purpose: map[string]string{}}
	for _, room := range r.known() {
		d.purpose[room.id] = room.purpose
	}
	team := rooms.Team{Server: r.server, Team: r.in.Team.ID.String(), Description: r.in.Team.Description, Roster: *r.roster}
	_, err := rooms.Converge(ctx, d, d, team, r.in.Rooms)
	for _, w := range d.writes {
		r.problem(w, r.applyFix())
	}
	re, isRooms := errors.AsType[*rooms.Error](err)
	switch {
	case err == nil:
	case isRooms && errors.Is(err, errWouldCreate):
		r.problem("room "+re.Purpose+" does not exist", r.applyFix())
	case isRooms && re.Drift:
		r.problem("room "+re.Purpose+": drift the rooms engine will not repair (step "+re.Step+")", r.applyFix()+" names it")
	default:
		return fmt.Errorf("view: the rooms: %w", err)
	}
	return nil
}

// derived is A's roster from the two files, nil when they do not make one.
func (r *reader) derived() *proto.Roster {
	roster, err := apply.DeriveRoster(r.in.Roster, r.in.Team)
	if err != nil {
		return nil
	}
	return &roster
}

// questions checks the questions room item by item (VW5): no worker joined or invited, the
// open coordinator joined, and the form the roster gives.
func (r *reader) questions(ctx context.Context) error {
	id := r.in.Rooms.Questions
	if id == "" {
		return nil
	}
	for _, m := range r.view.Members {
		if s := m.Rooms[proto.PurposeQuestions]; m.Role == proto.RoleWorker && (s == joined || s == invited) {
			r.problem("the worker "+m.Handle+" is in the questions room", r.applyFix())
		}
	}
	f := r.in.Roster
	c, ok := f.Members[f.Coordinator]
	open := ok && c.State == apply.MemberActive
	if open && r.roomsOf(f.Coordinator)[proto.PurposeQuestions] != joined {
		r.problem("the coordinator "+f.Coordinator+" has not joined the questions room", "ccy-teams restart "+f.Coordinator)
	}
	if r.roster == nil {
		return nil
	}
	return r.form(ctx, id, open)
}

// form compares the questions room's power levels with those A's spec gives the roster.
func (r *reader) form(ctx context.Context, id string, open bool) error {
	var want proto.Value
	for _, spec := range proto.Desired(r.server, *r.roster, r.in.Rooms, r.in.Team.Description) {
		for _, ev := range spec.State {
			if spec.Purpose == proto.PurposeQuestions && ev.Type == proto.PowerLevelsType {
				want = ev.Content
			}
		}
	}
	raw, err := r.a.RoomState(ctx, id)
	if err != nil {
		return fmt.Errorf("view: the state of room questions: %w", err)
	}
	for _, ev := range raw {
		v, err := proto.Parse(ev)
		if err == nil && str(v, "type") == proto.PowerLevelsType && str(v, "state_key") == "" && get(v, "content").Equal(want) {
			return nil
		}
	}
	form := "closed"
	if open {
		form = "open"
	}
	r.problem("the questions room is not "+form+", as the roster says it is", r.applyFix())
	return nil
}

// pendingProblem is a `pending` operation and the verb that rolls it forward (VW6); the view
// never completes it.
func pendingProblem(team paths.TeamID, p *apply.Pending) status.Problem {
	target := team.String()
	if p.Handle != "" {
		target = p.Handle
	}
	fix := "teamchat-admin " + strings.ReplaceAll(p.Op, ".", " ") + " " + target
	if p.Op == apply.OpTeamApply {
		fix = "ccy-teams apply " + team.String()
	}
	prob, err := status.NewProblem(p.Op+" did not finish (step "+p.Step+")", fix)
	if err != nil {
		return status.Problem{What: "an operation did not finish", Fix: "journalctl -u teamchat-health.service"}
	}
	return prob
}
