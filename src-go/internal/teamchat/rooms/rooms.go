// Package rooms is the host's rooms engine (Plan 00209, DESIGN-v2-C-host.md 3.6.2 step 4;
// SPEC-n2a.md RE1-RE10): it converges a team's rooms on the homeserver to the room specs of
// area A (`proto.Desired`, seam 13) through the matrix admin client. It finds, adopts or creates
// each room in A 5.2's order, refuses drift it must not repair, then kicks in every room, then
// writes the state that differs (the roster by seam 17's `rev` rule), then invites. The engine
// knows nothing of what a room is for; a second run over a converged team writes nothing.
package rooms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// Client is the part of the matrix admin client the engine calls.
type Client interface {
	JoinedRooms(ctx context.Context) ([]string, error)
	RoomState(ctx context.Context, roomID string) ([]json.RawMessage, error)
	CreateRoom(ctx context.Context, req matrix.CreateRoomRequest) (string, error)
	PutState(ctx context.Context, roomID, eventType, stateKey string, content json.RawMessage) (string, error)
	Invite(ctx context.Context, roomID, userID string) error
	Kick(ctx context.Context, roomID, userID, reason string) error
}

// Recorder keeps the team's room IDs (C's `rooms.json`, and the roster's `pending` for the
// intent); the engine calls Intent before it creates a room and Record once it has its ID.
type Recorder interface {
	Intent(purpose string) error
	Record(purpose, roomID string) error
}

// Team is what the rooms are built from: the server name, the team id, A's roster as the host
// derived it (its Rev is the roster file's `rev`; its Rooms are ignored) and the description.
type Team struct {
	Server, Team, Description string
	Roster                    proto.Roster
}

// Result is the converged rooms: their IDs, the CHANGED lines, and the roster `rev` written,
// 0 when the roster event was left as it was.
type Result struct {
	Rooms     proto.RoomIDs
	Changed   []string
	RosterRev int64
}

// The steps a failure names (RE9).
const (
	StepFind   = "find"
	StepAdopt  = "adopt"
	StepCreate = "create"
	StepRecord = "record"
	StepRead   = "read"
	StepKick   = "kick"
	StepState  = "state"
	StepInvite = "invite"
)

// Error is a failed convergence: the room, the step, and whether it is drift the engine must
// not repair (RE9).
type Error struct {
	Purpose, Step string
	Drift         bool
	Err           error
}

func (e *Error) Error() string {
	return "room " + e.Purpose + ", step " + e.Step + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// Converge makes the team's rooms what A's specs say, starting from the room IDs known.
func Converge(ctx context.Context, c Client, rec Recorder, t Team, ids proto.RoomIDs) (Result, error) {
	e := &engine{c: c, rec: rec, t: t, ids: ids, admin: proto.AdminID(t.Server)}
	specs, err := e.find(ctx)
	if err != nil {
		return Result{}, err
	}
	rooms, err := e.read(ctx, specs)
	if err != nil {
		return Result{}, err
	}
	rosterWrite, err := e.planRoster(rooms[0])
	if err != nil {
		return Result{}, err
	}
	for _, step := range []func(context.Context, []room, *stateWrite) error{e.kick, e.writeState, e.invite} {
		if err := step(ctx, rooms, rosterWrite); err != nil {
			return Result{}, err
		}
	}
	return Result{Rooms: e.ids, Changed: e.changed, RosterRev: rosterWrite.rev}, nil
}

type engine struct {
	c       Client
	rec     Recorder
	t       Team
	ids     proto.RoomIDs
	admin   string
	changed []string
}

func (e *engine) note(purpose, what string) { e.changed = append(e.changed, "room "+purpose+" "+what) }

func failed(purpose, step string, err error) error {
	return &Error{Purpose: purpose, Step: step, Err: err}
}

func drift(purpose, step, format string, args ...any) error {
	return &Error{Purpose: purpose, Step: step, Drift: true, Err: fmt.Errorf(format, args...)}
}

// find gives every spec a room, adopting or creating the first without one and asking A again
// (RE1).
func (e *engine) find(ctx context.Context) ([]proto.RoomSpec, error) {
	for {
		specs := proto.Desired(e.t.Server, e.t.Roster, e.ids, e.t.Description)
		i := slices.IndexFunc(specs, func(s proto.RoomSpec) bool { return s.RoomID == "" })
		if i < 0 {
			return specs, nil
		}
		if err := e.place(ctx, specs[i]); err != nil {
			return nil, err
		}
	}
}

func (e *engine) place(ctx context.Context, spec proto.RoomSpec) error {
	p := spec.Purpose
	id, found, err := e.adoptable(ctx, p)
	if err != nil {
		return err
	}
	what := "adopted "
	if !found {
		if err = e.rec.Intent(p); err != nil {
			return failed(p, StepCreate, err)
		}
		if id, err = e.c.CreateRoom(ctx, createRequest(spec)); err != nil {
			return failed(p, StepCreate, err)
		}
		what = "created "
	}
	if err := e.rec.Record(p, id); err != nil {
		return failed(p, StepRecord, err)
	}
	e.set(p, id)
	e.note(p, what+id)
	return nil
}

func (e *engine) set(purpose, id string) {
	switch purpose {
	case proto.PurposeMain:
		e.ids.Main = id
	case proto.PurposeReports:
		e.ids.Reports = id
	case proto.PurposeQuestions:
		e.ids.Questions = id
	}
}

// adoptable is the one room the admin has joined that is this purpose's and not yet known
// (RE2): for main one with no link, for a side room one whose admin-sent link names this team
// and purpose.
func (e *engine) adoptable(ctx context.Context, purpose string) (string, bool, error) {
	joined, err := e.c.JoinedRooms(ctx)
	if err != nil {
		return "", false, failed(purpose, StepFind, err)
	}
	var candidates []string
	for _, id := range joined {
		if id == e.ids.Main || id == e.ids.Reports || id == e.ids.Questions {
			continue
		}
		events, err := e.stateOf(ctx, id)
		if err != nil {
			return "", false, failed(purpose, StepFind, err)
		}
		ok, err := e.candidate(purpose, id, events)
		if err != nil {
			return "", false, err
		}
		if ok {
			candidates = append(candidates, id)
		}
	}
	switch len(candidates) {
	case 0:
		return "", false, nil
	case 1:
		return candidates[0], true, nil
	}
	return "", false, drift(purpose, StepAdopt, "more than one room could be it: %s", strings.Join(candidates, ", "))
}

// candidate is whether the room is purpose's: one the admin created, main with no link, a side
// room with an admin-sent link naming this team and purpose (n2a-D7). A side room linking
// another main is drift (n2a-D8).
func (e *engine) candidate(purpose, id string, events map[key]proto.StateEvent) (bool, error) {
	if create, ok := events[key{proto.CreateType, ""}]; !ok || create.Sender != e.admin {
		return false, nil
	}
	link, linked := events[key{proto.LinkType, ""}]
	if purpose == proto.PurposeMain {
		return !linked, nil
	}
	if !linked || link.Sender != e.admin || field(link.Content, "team") != e.t.Team || field(link.Content, "purpose") != purpose {
		return false, nil
	}
	if main := field(link.Content, "main"); main != e.ids.Main {
		return false, drift(purpose, StepAdopt, "room %s links main room %s, not %s", id, main, e.ids.Main)
	}
	return true, nil
}

func field(v proto.Value, name string) string {
	f, ok := v.Get(name)
	if !ok || f.Kind() != proto.KindString {
		return ""
	}
	return f.Str()
}

// createRequest is a private room of the spec's version, its power levels as the override and
// every other state event but the roster as initial state, with no invite (RE3).
func createRequest(spec proto.RoomSpec) matrix.CreateRoomRequest {
	req := matrix.CreateRoomRequest{Preset: "private_chat", RoomVersion: spec.RoomVersion}
	for _, ev := range spec.State {
		switch ev.Type {
		case proto.PowerLevelsType:
			req.PowerLevelOverride = ev.Content.Compact()
		case proto.RosterType:
		default:
			req.InitialState = append(req.InitialState, matrix.StateEvent{Type: ev.Type, StateKey: ev.StateKey, Content: ev.Content.Compact()})
		}
	}
	return req
}

// room is one spec with its room's current state and who is joined or invited.
type room struct {
	spec    proto.RoomSpec
	state   map[key]proto.StateEvent
	members []string
}

// read reads every room and refuses drift it must not repair, before any write (RE4).
func (e *engine) read(ctx context.Context, specs []proto.RoomSpec) ([]room, error) {
	rooms := make([]room, 0, len(specs))
	for _, spec := range specs {
		p := spec.Purpose
		state, err := e.stateOf(ctx, spec.RoomID)
		if err != nil {
			return nil, failed(p, StepRead, err)
		}
		create, ok := state[key{proto.CreateType, ""}]
		switch {
		case !ok:
			return nil, drift(p, StepRead, "room %s has no create event", spec.RoomID)
		case create.Sender != e.admin:
			return nil, drift(p, StepRead, "room %s was created by %s, not the admin", spec.RoomID, create.Sender)
		case field(create.Content, "room_version") != spec.RoomVersion:
			return nil, drift(p, StepRead, "room %s is not room version %s", spec.RoomID, spec.RoomVersion)
		}
		if _, other := create.Content.Get("additional_creators"); other {
			return nil, drift(p, StepRead, "room %s has another creator", spec.RoomID)
		}
		if _, encrypted := state[key{"m.room.encryption", ""}]; encrypted {
			return nil, drift(p, StepRead, "room %s is encrypted", spec.RoomID)
		}
		rooms = append(rooms, room{spec: spec, state: state, members: present(state)})
	}
	return rooms, nil
}

// present are the users joined or invited.
func present(state map[key]proto.StateEvent) []string {
	var out []string
	for k, ev := range state {
		if m := field(ev.Content, "membership"); k.typ == proto.MemberType && (m == "join" || m == "invite") {
			out = append(out, k.stateKey)
		}
	}
	slices.Sort(out)
	return out
}

// kick removes, in every room, whoever is there and must not be (RE5).
func (e *engine) kick(ctx context.Context, rooms []room, _ *stateWrite) error {
	for _, r := range rooms {
		_, kicks := proto.MembershipDiff(r.spec.Members, r.members, e.t.Server)
		for _, user := range kicks {
			if err := e.c.Kick(ctx, r.spec.RoomID, user, ""); err != nil {
				return failed(r.spec.Purpose, StepKick, err)
			}
			e.note(r.spec.Purpose, "kicked "+user)
		}
	}
	return nil
}

// stateWrite is the roster event to write, with its rev; rev 0 is no write.
type stateWrite struct {
	content proto.Value
	rev     int64
}

// planRoster is the roster write main needs, if any, checked by A's decoder before anything is
// written (RE6, seam 17).
func (e *engine) planRoster(main room) (*stateWrite, error) {
	i := slices.IndexFunc(main.spec.State, func(ev proto.StateEvent) bool { return ev.Type == proto.RosterType })
	if i < 0 {
		return &stateWrite{}, nil
	}
	want := main.spec.State[i].Content
	cur, ok := main.state[key{proto.RosterType, ""}]
	if ok && cur.Sender == e.admin && without(cur.Content, "rev").Equal(without(want, "rev")) {
		return &stateWrite{}, nil
	}
	var eventRev int64
	if r, found := cur.Content.Get("rev"); ok && found && r.Kind() == proto.KindInt {
		eventRev = r.Int()
	}
	w := &stateWrite{rev: proto.NextRev(eventRev, e.t.Roster.Rev)}
	w.content = with(want, "rev", proto.Int(w.rev))
	if _, err := proto.DecodeRoster(w.content, e.t.Server, e.t.Team, e.ids.Main); err != nil {
		return nil, &Error{Purpose: proto.PurposeMain, Step: StepState, Drift: true, Err: err}
	}
	return w, nil
}

func without(v proto.Value, name string) proto.Value {
	return proto.Obj(slices.DeleteFunc(slices.Clone(v.Members()), func(m proto.Member) bool { return m.Key == name })...)
}

func with(v proto.Value, name string, val proto.Value) proto.Value {
	members := slices.Clone(v.Members())
	for i := range members {
		if members[i].Key == name {
			members[i].Val = val
		}
	}
	return proto.Obj(members...)
}

// writeState writes, in spec order, every state event the room lacks, holds differently or
// holds from another sender (RE6).
func (e *engine) writeState(ctx context.Context, rooms []room, roster *stateWrite) error {
	for _, r := range rooms {
		for _, ev := range r.spec.State {
			content := ev.Content
			if ev.Type == proto.RosterType {
				if roster.rev == 0 {
					continue
				}
				content = roster.content
			} else if cur, ok := r.state[key{ev.Type, ev.StateKey}]; ok && cur.Sender == e.admin && cur.Content.Equal(content) {
				continue
			}
			if _, err := e.c.PutState(ctx, r.spec.RoomID, ev.Type, ev.StateKey, content.Compact()); err != nil {
				return failed(r.spec.Purpose, StepState, err)
			}
			e.note(r.spec.Purpose, "state "+ev.Type)
		}
	}
	return nil
}

// invite invites whoever the spec invites and is neither joined nor invited (RE7).
func (e *engine) invite(ctx context.Context, rooms []room, _ *stateWrite) error {
	for _, r := range rooms {
		for _, user := range r.spec.Invite {
			if user == e.admin || slices.Contains(r.members, user) {
				continue
			}
			if err := e.c.Invite(ctx, r.spec.RoomID, user); err != nil {
				return failed(r.spec.Purpose, StepInvite, err)
			}
			e.note(r.spec.Purpose, "invited "+user)
		}
	}
	return nil
}

// stateOf is a room's current state, by type and state key.
func (e *engine) stateOf(ctx context.Context, roomID string) (map[key]proto.StateEvent, error) {
	raw, err := e.c.RoomState(ctx, roomID)
	if err != nil {
		return nil, err
	}
	events, err := parseState(raw)
	if err != nil {
		return nil, err
	}
	out := make(map[key]proto.StateEvent, len(events))
	for _, ev := range events {
		k := key{ev.Type, ev.StateKey}
		if _, twice := out[k]; twice {
			return nil, fmt.Errorf("room %s holds %s %q twice", roomID, ev.Type, ev.StateKey)
		}
		out[k] = ev
	}
	return out, nil
}

type key struct{ typ, stateKey string }

var errMalformed = errors.New("a state event is not an object with a type, a state key, a sender, an event id and object content")

// parseState is a room's state events read strictly, as proto's trust checks take them.
func parseState(raw []json.RawMessage) ([]proto.StateEvent, error) {
	out := make([]proto.StateEvent, 0, len(raw))
	for _, r := range raw {
		v, err := proto.Parse(r)
		if err != nil {
			return nil, err
		}
		strs := map[string]string{}
		for _, name := range []string{"type", "state_key", "sender", "event_id"} {
			f, ok := v.Get(name)
			if !ok || f.Kind() != proto.KindString || (name != "state_key" && f.Str() == "") {
				return nil, errMalformed
			}
			strs[name] = f.Str()
		}
		content, ok := v.Get("content")
		if !ok || content.Kind() != proto.KindObject {
			return nil, errMalformed
		}
		out = append(out, proto.StateEvent{
			Type: strs["type"], StateKey: strs["state_key"], Sender: strs["sender"], EventID: strs["event_id"], Content: content,
		})
	}
	return out, nil
}
