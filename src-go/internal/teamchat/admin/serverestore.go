package admin

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// The restore ops' args (RC1-RC8).
const (
	keyPort         = "port"
	keyToken        = "token"
	keyRooms        = "rooms"
	keyRoster       = "roster"
	keyRetire       = "retire"
	keyRevoke       = "revoke"
	keyRevokeHumans = "revoke_humans"
	keyLock         = "lock"
	keyBackup       = "backup"
)

// restoreArgKeys are the exact args of each restore op (RC1-RC8).
var restoreArgKeys = map[string][]string{
	OpRestoreSecure: {keyTeamFile, keyPort, keyToken, keyRooms, keyRoster, keyRetire, keyRevoke, keyRevokeHumans, keyLock},
	OpRestoreCheck:  {keyTeamFile, keyPort, keyToken},
	OpRestoreSwap:   {keyTeamFile, keyToken, keyRooms, keyRoster},
	OpRestoreExport: {keyTeamFile, keyBackup, keySinceMS},
	OpRestoreFinish: {keyTeamFile},
	OpRestoreRevert: {keyTeamFile},
}

// The export's cap (n2g-D15), which --lost also reads under (RS14), and the page it reads (RC3).
const (
	maxLost    = 64 << 20
	exportPage = 100
)

// restoreAdmin is the part of the admin client the restore ops call; *matrix.Admin is one.
type restoreAdmin interface {
	Account(ctx context.Context, userID string) (matrix.Account, bool, error)
	SetDeactivated(ctx context.Context, userID string, deactivated bool) error
	ResetPassword(ctx context.Context, userID, password string) error
	SetLocked(ctx context.Context, userID string, locked bool) error
	Membership(ctx context.Context, roomID, userID string) (string, error)
	Kick(ctx context.Context, roomID, userID, reason string) error
	GetState(ctx context.Context, roomID, eventType, stateKey string) (json.RawMessage, error)
	GetStateEvent(ctx context.Context, roomID, eventType, stateKey string) (json.RawMessage, error)
	PutState(ctx context.Context, roomID, eventType, stateKey string, content json.RawMessage) (string, error)
	messagesReader
}

// messagesReader reads a room's events, as the export does.
type messagesReader interface {
	Messages(ctx context.Context, roomID string, req matrix.MessagesRequest) (matrix.MessagesPage, error)
}

// restoreArgs are a restore op's args, read strictly before anything is opened.
type restoreArgs struct {
	port                               int
	token                              string
	staged                             bool
	rooms                              proto.RoomIDs
	roster                             apply.RosterFile
	retire, revoke, revokeHumans, lock []string
	backup, sinceMS                    int64
}

// serveRestore is one restore op (RC1-RC8) on the team the request's team file names.
func serveRestore(ctx context.Context, open Opener, op string, team declare.Team, args map[string]proto.Value) Answer {
	a, err := readRestoreArgs(op, team.ID, args)
	if err != nil {
		return failure(ClassFailed, "op "+op+" args: "+err.Error())
	}
	o, err := open(ctx, team.ID, team.Port, "")
	if err != nil {
		return openFailure(err)
	}
	if o.Restore == nil || o.Staged == nil {
		return failure(ClassConfig, "the team's admin files cannot serve a restore")
	}
	switch op {
	case OpRestoreSecure:
		return serveSecure(ctx, o, team, a)
	case OpRestoreCheck:
		return serveCheck(ctx, o, team, a)
	case OpRestoreSwap:
		if err := o.Restore.Swap([]byte(a.token), apply.EncodeRooms(team.ID, a.rooms), a.roster.Encode()); err != nil {
			return failure(ClassFailed, "the swap: "+err.Error())
		}
		return Answer{OK: true, Changed: []string{"admin.token, rooms.json and roster.json swapped"}}
	case OpRestoreExport:
		return serveExport(ctx, o, team, a)
	case OpRestoreFinish:
		return previousAnswer(o.Restore.Finish, " previous removed")
	}
	return previousAnswer(o.Restore.Revert, " put back")
}

// previousAnswer is finish's or revert's answer: one line per name done, said as suffix says.
func previousAnswer(op func() ([]string, error), suffix string) Answer {
	names, err := op()
	if err != nil {
		return failure(ClassFailed, "the previous files: "+err.Error())
	}
	changed := []string{}
	for _, name := range names {
		changed = append(changed, strings.Replace(name+suffix, " previous", ".previous", 1))
	}
	return Answer{OK: true, Changed: changed}
}

func readRestoreArgs(op string, team paths.TeamID, args map[string]proto.Value) (restoreArgs, error) {
	var a restoreArgs
	r := &argReader{}
	a.readEndpoint(r, op, args)
	a.readFiles(r, team, args)
	a.readLists(r, args)
	a.readNumbers(r, args)
	return a, errors.Join(r.errs...)
}

// argReader gathers every arg's problem.
type argReader struct{ errs []error }

func (r *argReader) failf(format string, v ...any) { r.errs = append(r.errs, fmt.Errorf(format, v...)) }

// readEndpoint is port and token; restore.check takes both null for the live server.
func (a *restoreArgs) readEndpoint(r *argReader, op string, args map[string]proto.Value) {
	nullOK := op == OpRestoreCheck
	if v, given := args[keyPort]; given {
		switch {
		case nullOK && v.Kind() == proto.KindNull:
		case v.Kind() != proto.KindInt || v.Int() < 1 || v.Int() > 65535:
			r.failf("port is not a port from 1 to 65535")
		default:
			a.port, a.staged = int(v.Int()), true
		}
	}
	if v, given := args[keyToken]; given {
		a.readToken(r, v, nullOK)
	}
}

// readToken is the token, which must be null exactly when a check's port is.
func (a *restoreArgs) readToken(r *argReader, v proto.Value, nullOK bool) {
	null := nullOK && v.Kind() == proto.KindNull
	switch {
	case nullOK && null == a.staged:
		r.failf("token and port are not both null or both given")
	case null:
	case v.Kind() != proto.KindString || validCredential(apply.Credential{Kind: apply.CredToken, Secret: v.Str()}) != nil:
		r.failf("token is not 1 to %d printable ASCII bytes without a space", maxSecret)
	default:
		a.token = v.Str()
	}
}

// readFiles is rooms and roster, each by its file's own reader.
func (a *restoreArgs) readFiles(r *argReader, team paths.TeamID, args map[string]proto.Value) {
	fail := r.failf
	if v, given := args[keyRooms]; given {
		ids, err := decodeArgFile(v, func(raw []byte) (proto.RoomIDs, error) { return apply.DecodeRooms(raw, team) })
		if err == nil && ids.Main == "" {
			err = errors.New("names no main room")
		}
		if err != nil {
			fail("rooms: %w", err)
		}
		a.rooms = ids
	}
	if v, given := args[keyRoster]; given {
		f, err := decodeArgFile(v, func(raw []byte) (apply.RosterFile, error) { return apply.DecodeRosterFile(raw, team) })
		if err != nil {
			fail("roster: %w", err)
		}
		a.roster = f
	}
}

// readLists is the handles and humans to retire, revoke and lock, each of its grammar.
func (a *restoreArgs) readLists(r *argReader, args map[string]proto.Value) {
	for key, list := range map[string]*[]string{keyRetire: &a.retire, keyRevoke: &a.revoke, keyRevokeHumans: &a.revokeHumans, keyLock: &a.lock} {
		v, given := args[key]
		if !given {
			continue
		}
		valid := validHandle
		if key == keyRevokeHumans || key == keyLock {
			valid = proto.ValidHumanName
		}
		got, err := stringList(v, key)
		if err == nil && slices.ContainsFunc(got, func(s string) bool { return !valid(s) }) {
			err = fmt.Errorf("%s holds a name that is not of its grammar", key)
		}
		if err != nil {
			r.errs = append(r.errs, err)
		}
		*list = got
	}
}

// readNumbers is the backup number (1 or more) and since_ms (0 or more).
func (a *restoreArgs) readNumbers(r *argReader, args map[string]proto.Value) {
	for key, at := range map[string]*int64{keyBackup: &a.backup, keySinceMS: &a.sinceMS} {
		v, given := args[key]
		if !given {
			continue
		}
		if v.Kind() != proto.KindInt || v.Int() < 0 || key == keyBackup && v.Int() < 1 {
			r.failf("%s is not a number of 1 or more (backup) or 0 or more (since_ms)", key)
		}
		*at = v.Int()
	}
}

// decodeArgFile is an arg that is one of the child's files, decoded by its own reader.
func decodeArgFile[T any](v proto.Value, decode func([]byte) (T, error)) (T, error) {
	if v.Kind() != proto.KindObject {
		var zero T
		return zero, errors.New("not an object")
	}
	return decode(v.Compact())
}

// serveSecure is RC1 on the staged server: the rev checked, then retire, revoke and lock, and
// the roster event last.
func serveSecure(ctx context.Context, o Opened, team declare.Team, a restoreArgs) Answer {
	content, err := securedRoster(team, a)
	if err != nil {
		return failure(ClassFailed, "the secured roster: "+err.Error())
	}
	c, err := o.Staged(ctx, a.port, a.token)
	if err != nil {
		return failure(homeserverClass(err), "the staging server: "+err.Error())
	}
	s := &securing{c: c, team: team}
	if err := s.checkRev(ctx, a.rooms.Main, a.roster.Rev); err != nil {
		return s.failed(err)
	}
	for _, step := range []func(context.Context, restoreArgs) error{s.retire, s.revokeAll, s.lockAll} {
		if err := step(ctx, a); err != nil {
			return s.failed(err)
		}
	}
	if _, err := c.PutState(ctx, a.rooms.Main, proto.RosterType, "", content.Compact()); err != nil {
		return s.failed(fmt.Errorf("writing the roster event: %w", err))
	}
	s.changed = append(s.changed, "state "+proto.RosterType)
	return Answer{OK: true, Changed: s.changed}
}

// securedRoster is A's roster from the merged roster file, the team and the tar's rooms,
// validated by A's decoder (RC1).
func securedRoster(team declare.Team, a restoreArgs) (proto.Value, error) {
	r, err := apply.DeriveRoster(a.roster, team)
	if err != nil {
		return proto.Value{}, err
	}
	r.Rooms = proto.SideRooms{Reports: a.rooms.Reports, Questions: a.rooms.Questions}
	v := r.Encode()
	if _, err := proto.DecodeRoster(v, team.ServerName, team.ID.String(), a.rooms.Main); err != nil {
		return proto.Value{}, err
	}
	return v, nil
}

// securing is one restore.secure run: its client and the lines it changed.
type securing struct {
	c       *matrix.Admin
	team    declare.Team
	changed []string
}

func (s *securing) failed(err error) Answer {
	class := homeserverClass(err)
	if errors.Is(err, errRevNotBelow) {
		class = ClassRefused
	}
	a := failure(class, err.Error())
	a.Changed = s.changed
	return a
}

func (s *securing) id(localpart string) string { return proto.UserID(localpart, s.team.ServerName) }

var errRevNotBelow = errors.New("the staged roster event's rev is not below the roster's")

// checkRev refuses a roster whose rev is not above the staged event's (RC1).
func (s *securing) checkRev(ctx context.Context, main string, rev int64) error {
	raw, err := s.c.GetState(ctx, main, proto.RosterType, "")
	if err != nil {
		return fmt.Errorf("reading the staged roster event: %w", err)
	}
	v, err := proto.Parse(raw)
	if err != nil {
		return fmt.Errorf("the staged roster event: %w", err)
	}
	staged, found := v.Get("rev")
	if !found || staged.Kind() != proto.KindInt {
		return errors.New("the staged roster event has no rev")
	}
	if staged.Int() >= rev {
		return fmt.Errorf("%w: %d, not below %d", errRevNotBelow, staged.Int(), rev)
	}
	return nil
}

// retire kicks each retired handle from every room it is joined or invited to, then
// deactivates it.
func (s *securing) retire(ctx context.Context, a restoreArgs) error {
	rooms := []struct{ purpose, id string }{{proto.PurposeMain, a.rooms.Main}, {proto.PurposeReports, a.rooms.Reports}, {proto.PurposeQuestions, a.rooms.Questions}}
	for _, h := range a.retire {
		for _, room := range rooms {
			if err := s.kick(ctx, h, room.purpose, room.id); err != nil {
				return err
			}
		}
		acct, found, err := s.c.Account(ctx, s.id(h))
		if err != nil {
			return fmt.Errorf("member %s: %w", h, err)
		}
		if !found || acct.Deactivated {
			continue
		}
		if err := s.c.SetDeactivated(ctx, s.id(h), true); err != nil {
			return fmt.Errorf("deactivating member %s: %w", h, err)
		}
		s.changed = append(s.changed, "member "+h+" deactivated")
	}
	return nil
}

// kick removes handle h from the room when it is joined or invited there.
func (s *securing) kick(ctx context.Context, h, purpose, room string) error {
	if room == "" {
		return nil
	}
	m, err := s.c.Membership(ctx, room, s.id(h))
	if err != nil {
		return fmt.Errorf("member %s in %s: %w", h, purpose, err)
	}
	if m != "join" && m != "invite" {
		return nil
	}
	if err := s.c.Kick(ctx, room, s.id(h), ""); err != nil {
		return fmt.Errorf("kicking member %s from %s: %w", h, purpose, err)
	}
	s.changed = append(s.changed, "member "+h+" kicked from "+purpose)
	return nil
}

// revokeAll signs every revoked handle and human out with a password never stored.
func (s *securing) revokeAll(ctx context.Context, a restoreArgs) error {
	for _, who := range []struct {
		kind  string
		names []string
	}{{"member", a.revoke}, {"human", a.revokeHumans}} {
		for _, name := range who.names {
			if err := s.c.ResetPassword(ctx, s.id(name), rand.Text()); err != nil {
				return fmt.Errorf("signing %s %s out: %w", who.kind, name, err)
			}
			s.changed = append(s.changed, who.kind+" "+name+" signed out")
		}
	}
	return nil
}

func (s *securing) lockAll(ctx context.Context, a restoreArgs) error {
	for _, name := range a.lock {
		if err := s.c.SetLocked(ctx, s.id(name), true); err != nil {
			return fmt.Errorf("locking human %s: %w", name, err)
		}
		s.changed = append(s.changed, "human "+name+" locked")
	}
	return nil
}

// serveCheck is RC8: the main room's roster event read by a direct state request, from the
// staged server or the live one, decoded by A's rules.
func serveCheck(ctx context.Context, o Opened, team declare.Team, a restoreArgs) Answer {
	var c interface {
		GetStateEvent(ctx context.Context, roomID, eventType, stateKey string) (json.RawMessage, error)
	}
	if a.staged {
		staged, err := o.Staged(ctx, a.port, a.token)
		if err != nil {
			return failure(homeserverClass(err), "the staging server: "+err.Error())
		}
		c = staged
	} else if live, ok := o.Admin.(restoreAdmin); ok {
		c = live
	} else {
		return failure(ClassConfig, "the team's admin client cannot read a roster event")
	}
	ids, err := roomsOf(team, o.Deps)
	if err != nil {
		return failure(ClassConfig, "the team's files: "+err.Error())
	}
	raw, err := c.GetStateEvent(ctx, ids.Main, proto.RosterType, "")
	if err != nil {
		return failure(homeserverClass(err), "reading the roster event: "+err.Error())
	}
	r, err := decodeRosterEvent(raw, team, ids.Main)
	if err != nil {
		return failure(ClassFailed, "the roster event: "+err.Error())
	}
	return Answer{OK: true, Epoch: &restore.Epoch{RestoreEpoch: r.RestoreEpoch, Rev: r.Rev}}
}

// decodeRosterEvent is a whole roster event the admin sent, its content by A's decoder.
func decodeRosterEvent(raw []byte, team declare.Team, main string) (proto.Roster, error) {
	v, err := proto.Parse(raw)
	if err != nil {
		return proto.Roster{}, err
	}
	sender, sok := v.Get("sender")
	content, cok := v.Get("content")
	if !sok || !cok || sender.Kind() != proto.KindString {
		return proto.Roster{}, errors.New("it is not an event with a sender and a content")
	}
	if admin := proto.AdminID(team.ServerName); sender.Str() != admin {
		return proto.Roster{}, fmt.Errorf("its sender is %s, not %s", sender.Str(), admin)
	}
	return proto.DecodeRoster(content, team.ServerName, team.ID.String(), main)
}

// roomsOf is the team's rooms record, which must name the main room.
func roomsOf(team declare.Team, d apply.Deps) (proto.RoomIDs, error) {
	raw, found, err := state.ReadFile(d.RoomsPath, maxChildFile)
	if err == nil && !found {
		err = errors.New("there is no rooms record")
	}
	if err != nil {
		return proto.RoomIDs{}, err
	}
	ids, err := apply.DecodeRooms(raw, team.ID)
	if err == nil && ids.Main == "" {
		err = errors.New("the rooms record names no main room")
	}
	return ids, err
}

// serveExport is RC3: the live server's events after since_ms, written to the lost file.
func serveExport(ctx context.Context, o Opened, team declare.Team, a restoreArgs) Answer {
	c, ok := o.Admin.(restoreAdmin)
	if !ok {
		return failure(ClassConfig, "the team's admin client cannot read events")
	}
	ids, err := roomsOf(team, o.Deps)
	if err != nil {
		return failure(ClassConfig, "the team's files: "+err.Error())
	}
	ex, data, err := exportEvents(ctx, c, ids, a.sinceMS, maxLost, exportPage)
	if err != nil {
		return failure(homeserverClass(err), "the export: "+err.Error())
	}
	ex.EndMS = max(ex.EndMS, o.Now().UnixMilli())
	if err := o.Restore.WriteLost(a.backup, data); err != nil {
		return failure(ClassFailed, "the export: "+err.Error())
	}
	name := filepath.Base(paths.RestoreLostFile(team.ID, a.backup))
	return Answer{OK: true, Changed: []string{fmt.Sprintf("%s written: %d events", name, ex.Count)}, Export: &ex}
}

// timed is one exported event: its server time and its line.
type timed struct {
	ts   int64
	line []byte
}

// exportEvents is every event of the rooms after since, in time order, one per line, kept to
// limit bytes from the newest; EndMS is the newest kept event's time.
func exportEvents(ctx context.Context, c messagesReader, ids proto.RoomIDs, since int64, limit, page int) (restore.Export, []byte, error) {
	var all []timed
	var ex restore.Export
	for _, room := range []string{ids.Main, ids.Reports, ids.Questions} {
		if room == "" {
			continue
		}
		got, cut, err := roomEvents(ctx, c, room, since, limit, page)
		if err != nil {
			return restore.Export{}, nil, err
		}
		all = append(all, got...)
		ex.Truncated = ex.Truncated || cut
	}
	slices.SortStableFunc(all, func(a, b timed) int { return cmp.Compare(a.ts, b.ts) })
	start, size := len(all), 0
	for start > 0 && size+len(all[start-1].line)+1 <= limit {
		start--
		size += len(all[start].line) + 1
	}
	ex.Truncated = ex.Truncated || start > 0
	var buf bytes.Buffer
	for _, e := range all[start:] {
		buf.Write(e.line)
		buf.WriteByte('\n')
	}
	if kept := all[start:]; len(kept) > 0 {
		ex.Count, ex.FirstMS, ex.EndMS = int64(len(kept)), kept[0].ts, kept[len(kept)-1].ts
	}
	return ex, buf.Bytes(), nil
}

// roomEvents reads room backwards from its end until an event at or before since, oldest
// first; cut is set when limit stopped it first.
func roomEvents(ctx context.Context, c messagesReader, room string, since int64, limit, page int) ([]timed, bool, error) {
	var out []timed
	size, from := 0, ""
	for {
		p, err := c.Messages(ctx, room, matrix.MessagesRequest{From: from, Dir: matrix.Backward, Limit: page})
		if err != nil {
			return nil, false, err
		}
		older := false
		for _, raw := range p.Chunk {
			e, err := eventLine(raw)
			if err != nil {
				return nil, false, err
			}
			if e.ts <= since {
				older = true
				continue
			}
			if size+len(e.line)+1 > limit {
				slices.Reverse(out)
				return out, true, nil
			}
			out, size = append(out, e), size+len(e.line)+1
		}
		if older || p.End == "" || len(p.Chunk) == 0 {
			slices.Reverse(out)
			return out, false, nil
		}
		from = p.End
	}
}

func eventLine(raw []byte) (timed, error) {
	v, err := proto.Parse(raw)
	if err != nil {
		return timed{}, fmt.Errorf("an event: %w", err)
	}
	ts, found := v.Get("origin_server_ts")
	if !found || ts.Kind() != proto.KindInt || ts.Int() < 0 {
		return timed{}, errors.New("an event has no origin_server_ts")
	}
	return timed{ts: ts.Int(), line: v.Compact()}, nil
}
