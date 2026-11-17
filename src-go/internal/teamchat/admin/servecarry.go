package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
)

// The child's carry operations (SC-C1 to SC-C3).
const (
	OpCarryTail  = "carry.tail"
	OpCarryPlace = "carry.place"
	OpCarryCheck = "carry.check"
)

// AdminTokenName is the team's admin token's name in ctl/<team>/ (OP2), which carry.place
// writes, the child's own files read and the export carries; carry holds the one spelling.
const AdminTokenName = carry.AdminTokenName

// The carry ops' own args and answer keys.
const (
	keyTail  = "tail"
	keyFound = "found"
	keyGuest = "guest"
	keyLost  = "lost"
)

// carryArgKeys are the exact args of carry.tail and carry.check, which the Opener serves;
// carry.place's are placeArgKeys, served by the Placer.
var (
	carryArgKeys = map[string][]string{
		OpCarryTail:  {keyTeamFile},
		OpCarryCheck: {keyTeamFile, keyPort, keyTail},
	}
	placeArgKeys = []string{keyTeamFile, keyRoster, keyRooms, keyToken, keyGuest, keyLost}
)

// Placer writes a carried team's files into ctl/<team>/ (SC-C2): it refuses when any of them
// is already there, then writes each with the atomic writer, answering the names written. It
// is the child's second seam beside the Opener, because the Opener needs the admin token the
// place writes.
type Placer func(team paths.TeamID, files []PlacedFile) ([]string, error)

// PlacedFile is one file carry.place writes: its name under ctl/<team>/ (a plain name, or
// `guest/<handle>.token`) and its content.
type PlacedFile struct {
	Name string
	Data []byte
}

// carryAdmin is the part of the admin client carry.tail and carry.check call; *matrix.Admin
// is one.
type carryAdmin interface {
	GetStateEvent(ctx context.Context, roomID, eventType, stateKey string) (json.RawMessage, error)
	GetEvent(ctx context.Context, roomID, eventID string) (json.RawMessage, error)
	messagesReader
}

// room is one of the team's rooms: its purpose in rooms.json and its ID.
type room struct{ purpose, id string }

// roomList is the team's rooms in rooms.json's order, those it names.
func roomList(ids proto.RoomIDs) []room {
	var out []room
	for _, r := range []room{{proto.PurposeMain, ids.Main}, {proto.PurposeReports, ids.Reports}, {proto.PurposeQuestions, ids.Questions}} {
		if r.id != "" {
			out = append(out, r)
		}
	}
	return out
}

// serveCarry is carry.tail or carry.check on the team the request's team file names: tail on
// the team's port, check on the port given.
func serveCarry(ctx context.Context, open Opener, op string, team declare.Team, args map[string]proto.Value) Answer {
	port, tail := team.Port, []carry.TailEntry(nil)
	if op == OpCarryCheck {
		var err error
		if port, tail, err = readCheckArgs(args); err != nil {
			return failure(ClassFailed, "op "+op+" args: "+err.Error())
		}
	}
	o, err := open(ctx, team.ID, port, "")
	if err != nil {
		return openFailure(err)
	}
	c, ok := o.Admin.(carryAdmin)
	if !ok {
		return failure(ClassConfig, "the team's admin client cannot read events")
	}
	ids, err := roomsOf(team, o.Deps)
	if err != nil {
		return failure(ClassConfig, "the team's files: "+err.Error())
	}
	if op == OpCarryCheck {
		return serveCarryCheck(ctx, c, team, ids, tail)
	}
	return serveCarryTail(ctx, c, team, ids)
}

func readCheckArgs(args map[string]proto.Value) (int, []carry.TailEntry, error) {
	p := args[keyPort]
	if p.Kind() != proto.KindInt || p.Int() < 1 || p.Int() > 65535 {
		return 0, nil, errors.New("port is not a port from 1 to 65535")
	}
	tail, err := carry.DecodeTail(args[keyTail])
	return int(p.Int()), tail, err
}

// serveCarryTail is SC-C1: each room's newest event, then the roster event's epoch.
func serveCarryTail(ctx context.Context, c carryAdmin, team declare.Team, ids proto.RoomIDs) Answer {
	var tail []carry.TailEntry
	for _, r := range roomList(ids) {
		e, a := newestEvent(ctx, c, r)
		if a != nil {
			return *a
		}
		tail = append(tail, e)
	}
	epoch, a := rosterEpoch(ctx, c, team, ids.Main)
	if a != nil {
		return *a
	}
	return Answer{OK: true, Tail: tail, Epoch: &epoch}
}

// newestEvent is r's newest event, read backwards one event from the end.
func newestEvent(ctx context.Context, c messagesReader, r room) (carry.TailEntry, *Answer) {
	p, err := c.Messages(ctx, r.id, matrix.MessagesRequest{Dir: matrix.Backward, Limit: 1})
	if err != nil {
		a := failure(homeserverClass(err), "reading room "+r.purpose+": "+err.Error())
		return carry.TailEntry{}, &a
	}
	if len(p.Chunk) == 0 {
		a := failure(ClassFailed, "room "+r.purpose+" has no event")
		return carry.TailEntry{}, &a
	}
	e, err := eventLine(p.Chunk[0])
	var id proto.Value
	if err == nil {
		v, perr := proto.Parse(p.Chunk[0])
		var found bool
		if id, found = v.Get("event_id"); perr != nil || !found || id.Kind() != proto.KindString || id.Str() == "" {
			err = errors.New("an event has no event_id")
		}
	}
	if err != nil {
		a := failure(ClassFailed, "room "+r.purpose+": "+err.Error())
		return carry.TailEntry{}, &a
	}
	return carry.TailEntry{Room: r.purpose, EventID: id.Str(), TsMS: e.ts}, nil
}

// serveCarryCheck is SC-C3: each tail event looked up in its room, found or not, then the
// roster event's epoch.
func serveCarryCheck(ctx context.Context, c carryAdmin, team declare.Team, ids proto.RoomIDs, tail []carry.TailEntry) Answer {
	rooms := roomList(ids)
	found := make([]bool, 0, len(tail))
	for _, e := range tail {
		i := slices.IndexFunc(rooms, func(r room) bool { return r.purpose == e.Room })
		if i < 0 {
			return failure(ClassFailed, "the tail names room "+e.Room+", which rooms.json does not")
		}
		_, err := c.GetEvent(ctx, rooms[i].id, e.EventID)
		if err != nil && !errors.Is(err, matrix.ErrNotFound) {
			return failure(homeserverClass(err), "looking up the tail event of room "+e.Room+": "+err.Error())
		}
		found = append(found, err == nil)
	}
	epoch, a := rosterEpoch(ctx, c, team, ids.Main)
	if a != nil {
		return *a
	}
	return Answer{OK: true, Found: found, Epoch: &epoch}
}

// stateEventReader reads one whole state event.
type stateEventReader interface {
	GetStateEvent(ctx context.Context, roomID, eventType, stateKey string) (json.RawMessage, error)
}

// rosterEpoch is RC8's reading: the main room's roster event by a direct state request,
// decoded by A's rules.
func rosterEpoch(ctx context.Context, c stateEventReader, team declare.Team, main string) (restore.Epoch, *Answer) {
	raw, err := c.GetStateEvent(ctx, main, proto.RosterType, "")
	if err != nil {
		a := failure(homeserverClass(err), "reading the roster event: "+err.Error())
		return restore.Epoch{}, &a
	}
	r, err := decodeRosterEvent(raw, team, main)
	if err != nil {
		a := failure(ClassFailed, "the roster event: "+err.Error())
		return restore.Epoch{}, &a
	}
	return restore.Epoch{RestoreEpoch: r.RestoreEpoch, Rev: r.Rev}, nil
}

// ServePlace runs carry.place in the child (SC-C2): its args read strictly, then the files
// handed to place, and the names written answered with no credential.
func ServePlace(place Placer, req Request) Answer {
	if req.Op != OpCarryPlace {
		return failure(ClassFailed, "op "+req.Op+" is not "+OpCarryPlace)
	}
	args, err := exactKeys(req.Args, placeArgKeys...)
	if err != nil {
		return failure(ClassFailed, "op "+req.Op+" args: "+err.Error())
	}
	team, err := teamFile(args[keyTeamFile], req.Team)
	if err != nil {
		return failure(ClassFailed, err.Error())
	}
	files, err := placedFiles(team.ID, args)
	if err != nil {
		return failure(ClassFailed, "op "+req.Op+" args: "+err.Error())
	}
	if place == nil {
		return failure(ClassConfig, "the child cannot place a carried team's files")
	}
	names, err := place(team.ID, files)
	if err != nil {
		return failure(ClassFailed, "placing the carried files: "+err.Error())
	}
	changed := make([]string, 0, len(names))
	for _, name := range names {
		changed = append(changed, name+" written")
	}
	return Answer{OK: true, Changed: changed}
}

// placedFiles are carry.place's files in SI9's order: roster.json, rooms.json and the admin
// token, each guest token, each lost export; the roster and rooms in their own encodings.
func placedFiles(team paths.TeamID, args map[string]proto.Value) ([]PlacedFile, error) {
	r := &argReader{}
	rel := func(p string) string { return strings.TrimPrefix(p, paths.CtlState(team)+"/") }
	roster, err := decodeArgFile(args[keyRoster], func(raw []byte) (apply.RosterFile, error) { return apply.DecodeRosterFile(raw, team) })
	if err != nil {
		r.failf("roster: %w", err)
	}
	ids, err := decodeArgFile(args[keyRooms], func(raw []byte) (proto.RoomIDs, error) { return apply.DecodeRooms(raw, team) })
	if err == nil && ids.Main == "" {
		err = errors.New("names no main room")
	}
	if err != nil {
		r.failf("rooms: %w", err)
	}
	token := args[keyToken]
	if token.Kind() != proto.KindString || !printableWord(token.Str(), maxSecret) {
		r.failf("token is not 1 to %d printable ASCII bytes without a space", maxSecret)
	}
	guests, lost := readGuests(r, args[keyGuest]), readLost(r, args[keyLost])
	out := make([]PlacedFile, 0, 3+len(guests)+len(lost))
	out = append(out,
		PlacedFile{rel(paths.RosterFile(team)), roster.Encode()}, PlacedFile{rel(paths.RoomsFile(team)), apply.EncodeRooms(team, ids)},
		PlacedFile{AdminTokenName, []byte(token.Str())})
	for _, g := range guests {
		out = append(out, PlacedFile{rel(paths.GuestTokenDir(team)) + "/" + g.Handle + ".token", []byte(g.Token)})
	}
	for _, l := range lost {
		out = append(out, PlacedFile{rel(paths.RestoreLostFile(team, l.N)), l.Data})
	}
	return out, errors.Join(r.errs...)
}

// readGuests is the guest list: each a handle once, with a token of the credential grammar.
func readGuests(r *argReader, v proto.Value) []carry.GuestToken {
	var out []carry.GuestToken
	for _, item := range listOf(r, v, keyGuest) {
		f, err := exactKeys(item, "name", "data")
		name, data := f["name"], f["data"]
		switch {
		case err != nil:
			r.failf("guest: %w", err)
		case name.Kind() != proto.KindString || !validHandle(name.Str()):
			r.failf("guest: a name is not a member handle")
		case data.Kind() != proto.KindString || !printableWord(data.Str(), maxSecret):
			r.failf("guest: a token is not 1 to %d printable ASCII bytes without a space", maxSecret)
		case slices.ContainsFunc(out, func(g carry.GuestToken) bool { return g.Handle == name.Str() }):
			r.failf("guest: a handle is named twice")
		default:
			out = append(out, carry.GuestToken{Handle: name.Str(), Token: data.Str()})
		}
	}
	return out
}

// readLost is the lost exports: each a backup number of 1 or more, once, with its text.
func readLost(r *argReader, v proto.Value) []carry.LostExport {
	var out []carry.LostExport
	for _, item := range listOf(r, v, keyLost) {
		f, err := exactKeys(item, "n", "data")
		n, data := f["n"], f["data"]
		switch {
		case err != nil:
			r.failf("lost: %w", err)
		case n.Kind() != proto.KindInt || n.Int() < 1:
			r.failf("lost: n is not a backup number of 1 or more")
		case data.Kind() != proto.KindString:
			r.failf("lost: data is not a string")
		case slices.ContainsFunc(out, func(l carry.LostExport) bool { return l.N == n.Int() }):
			r.failf("lost: a backup number is named twice")
		default:
			out = append(out, carry.LostExport{N: n.Int(), Data: []byte(data.Str())})
		}
	}
	return out
}

// listOf is v's items when v is an array.
func listOf(r *argReader, v proto.Value, key string) []proto.Value {
	if v.Kind() != proto.KindArray {
		r.failf("%s is not an array", key)
		return nil
	}
	return v.Items()
}

// validateCarry is a tail's or a found list's answer (SC-C1, SC-C3): of its own, beside the
// epoch, the tail each of its rooms' purposes once, the found list not empty.
func (a Answer) validateCarry() error {
	alone := a.Export == nil && a.Handle == "" && a.Credential == nil && len(a.Notes) == 0 && a.View == nil && a.Notice == nil
	if !alone || (a.Tail != nil) == (a.Found != nil) || a.Epoch == nil {
		return errors.New("a tail or a found list is an answer of its own, beside an epoch")
	}
	if a.Epoch.RestoreEpoch < 0 || a.Epoch.Rev < 1 {
		return errors.New("an epoch is not a restore epoch of 0 or more and a rev of 1 or more")
	}
	if a.Found != nil && len(a.Found) == 0 {
		return errors.New("a found list is empty")
	}
	return validTail(a.Tail)
}

// restoreOrCarry is an answer that holds a restore op's or a carry op's own fields.
func (a Answer) restoreOrCarry() bool {
	return a.Export != nil || a.Epoch != nil || a.Tail != nil || a.Found != nil
}

// validateRestoreOrCarry is a carry answer when it holds a tail or a found list, else a
// restore answer.
func (a Answer) validateRestoreOrCarry() error {
	if a.Tail != nil || a.Found != nil {
		return a.validateCarry()
	}
	return a.validateRestore()
}

// validTail is a tail answer's rooms: the records' grammar, each one of the team's purposes.
func validTail(tail []carry.TailEntry) error {
	if tail == nil {
		return nil
	}
	if _, err := carry.DecodeTail(carry.EncodeTail(tail)); err != nil {
		return err
	}
	for _, e := range tail {
		if e.Room != proto.PurposeMain && e.Room != proto.PurposeReports && e.Room != proto.PurposeQuestions {
			return fmt.Errorf("a tail room is not one of %s, %s and %s", proto.PurposeMain, proto.PurposeReports, proto.PurposeQuestions)
		}
	}
	return nil
}

// encodeCarry is the answer's tail and found list on the wire.
func (a Answer) encodeCarry() []proto.Member {
	var out []proto.Member
	if a.Tail != nil {
		out = append(out, proto.M(keyTail, carry.EncodeTail(a.Tail)))
	}
	if a.Found != nil {
		items := make([]proto.Value, 0, len(a.Found))
		for _, f := range a.Found {
			items = append(items, proto.Bool(f))
		}
		out = append(out, proto.M(keyFound, proto.Arr(items...)))
	}
	return out
}

// decodeCarry reads into a the tail and found list an ok answer's object holds.
func decodeCarry(a *Answer, obj map[string]proto.Value) error {
	if v, given := obj[keyTail]; given {
		tail, err := carry.DecodeTail(v)
		if err != nil {
			return err
		}
		a.Tail = tail
	}
	if v, given := obj[keyFound]; given {
		if v.Kind() != proto.KindArray {
			return errors.New("found is not an array")
		}
		a.Found = []bool{}
		for _, item := range v.Items() {
			if item.Kind() != proto.KindBool {
				return errors.New("found holds a value that is not a boolean")
			}
			a.Found = append(a.Found, item.Bool())
		}
	}
	return nil
}
