package admin

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health/view"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/notice"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// maxChildFile caps each of the child's own files it reads for a view or a notice, as the view
// caps them.
const maxChildFile = 1 << 20

// whyNoCopy is a notice that cannot be posted because the member has no guest token copy.
const whyNoCopy = "the member has no token copy"

// serveView is team.view (CO1): the team's roster file and rooms record read and viewed as the
// admin, read-only.
func serveView(ctx context.Context, open Opener, team declare.Team) Answer {
	o, err := open(ctx, team.ID, team.Port, "")
	if err != nil {
		return openFailure(err)
	}
	admin, ok := o.Admin.(view.Admin)
	if !ok {
		return failure(ClassConfig, "the team's admin client cannot read a view")
	}
	in, err := viewInput(team, o.Deps)
	if err != nil {
		return failure(ClassConfig, "the team's files: "+err.Error())
	}
	v, err := view.Read(ctx, admin, in)
	if err != nil {
		return failure(homeserverClass(err), "the view: "+err.Error())
	}
	return Answer{OK: true, View: &v}
}

// viewInput is view.Load's input with the team the request carried: the child never reads
// root's team.json. A missing roster file or rooms record is empty, as view.Load reads them.
func viewInput(team declare.Team, d apply.Deps) (view.Input, error) {
	in := view.Input{Team: team, Roster: apply.NewRosterFile(team.ID)}
	raw, found, err := state.ReadFile(d.RosterPath, maxChildFile)
	if err == nil && found {
		in.Roster, err = apply.DecodeRosterFile(raw, team.ID)
	}
	if err != nil {
		return view.Input{}, err
	}
	if raw, found, err = state.ReadFile(d.RoomsPath, maxChildFile); err == nil && found {
		in.Rooms, err = apply.DecodeRooms(raw, team.ID)
	}
	return in, err
}

// homeserverClass is a homeserver failure's class: unreachable when it did not answer usably or
// kept rate-limiting, else failed.
func homeserverClass(err error) ErrorClass {
	if errors.Is(err, matrix.ErrUnreachable) || errors.Is(err, matrix.ErrRateLimited) {
		return ClassUnreachable
	}
	return ClassFailed
}

// disconnectArgs are member.disconnected's args beside the team file.
type disconnectArgs struct {
	handle  string
	run     paths.RunID
	sinceMS int64
}

func readDisconnectArgs(args map[string]proto.Value) (disconnectArgs, error) {
	h, r, s := args[keyHandle], args[keyRun], args[keySinceMS]
	if h.Kind() != proto.KindString || !validHandle(h.Str()) {
		return disconnectArgs{}, errors.New("handle is not a member handle")
	}
	if r.Kind() != proto.KindString {
		return disconnectArgs{}, errors.New("run is not a string")
	}
	run, err := paths.ParseRunID(r.Str())
	if err != nil {
		return disconnectArgs{}, fmt.Errorf("run: %w", err)
	}
	if s.Kind() != proto.KindInt || s.Int() < 0 {
		return disconnectArgs{}, errors.New("since_ms is not a time in milliseconds")
	}
	return disconnectArgs{handle: h.Str(), run: run, sinceMS: s.Int()}, nil
}

// serveDisconnected is member.disconnected (CO2): the member's `disconnected` notice posted as
// the member with its guest token copy, into the main room of the child's rooms record.
func serveDisconnected(ctx context.Context, open Opener, team declare.Team, args map[string]proto.Value) Answer {
	da, err := readDisconnectArgs(args)
	if err != nil {
		return failure(ClassFailed, "op "+OpMemberDisconnected+" args: "+err.Error())
	}
	o, err := open(ctx, team.ID, team.Port, "")
	if err != nil {
		return openFailure(err)
	}
	token, found, err := guestToken(o.GuestDir, da.handle)
	if err != nil {
		return failure(ClassConfig, err.Error())
	}
	if !found {
		return Answer{OK: true, Notice: &health.Notice{Why: whyNoCopy}}
	}
	main, err := mainRoom(team.ID, o.RoomsPath)
	if err != nil {
		return failure(ClassConfig, err.Error())
	}
	if o.Member == nil {
		return failure(ClassConfig, "the child has no member client")
	}
	c, err := o.Member(token)
	if err != nil {
		return failure(ClassConfig, "the member client: "+oneLine(strings.ReplaceAll(err.Error(), token, "<token>")))
	}
	defer c.Close()
	out, err := notice.Disconnected(ctx, c, notice.Notice{
		Server: team.ServerName, Team: team.ID.String(), MainRoom: main, Handle: da.handle, Token: token, Run: da.run, SinceMS: da.sinceMS,
	})
	if err != nil {
		return failure(ClassUnreachable, "the disconnected notice: "+matrix.Class(err))
	}
	return Answer{OK: true, Notice: &health.Notice{Posted: out.Posted, Why: out.Why}}
}

// guestToken is the member's guest token copy, read by the trust rule; no error quotes it.
func guestToken(dir, handle string) (string, bool, error) {
	if dir == "" {
		return "", false, errors.New("the child has no directory of guest token copies")
	}
	raw, found, err := state.ReadFile(filepath.Join(dir, handle+".token"), maxSecret)
	if err != nil || !found {
		return "", false, err
	}
	token := string(raw)
	if validCredential(apply.Credential{Kind: apply.CredToken, Secret: token}) != nil {
		return "", false, fmt.Errorf("the token copy of %s is not 1 to %d printable ASCII bytes without a space", handle, maxSecret)
	}
	return token, true, nil
}

// mainRoom is the main room of the child's rooms record.
func mainRoom(team paths.TeamID, path string) (string, error) {
	raw, found, err := state.ReadFile(path, maxChildFile)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("the team has no rooms record: run team apply")
	}
	ids, err := apply.DecodeRooms(raw, team)
	if err != nil {
		return "", err
	}
	if !proto.ValidRoomID(ids.Main) {
		return "", errors.New("the rooms record has no main room: run team apply")
	}
	return ids.Main, nil
}
