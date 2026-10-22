package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// Opener is the child's own reach to one team (CV1, OP1): the admin client over the team's
// admin token on loopback at port, and its files under `ctl/<team>/`. regKey is the
// registration secret `team.apply` carries for bootstrap's first admin (OP3), "" for every
// other op.
type Opener func(ctx context.Context, team paths.TeamID, port int, regKey string) (Opened, error)

// Opened is one team as the child reaches it: apply's dependencies, and a client of the same
// homeserver as a member with its own token (CO2).
type Opened struct {
	apply.Deps

	Member func(token string) (*matrix.Client, error)
}

// argKeys are the exact args of each op the child serves (CV1, n2d-D7).
var argKeys = map[string][]string{
	apply.OpMemberAdd:     {keyTeamFile, keyPrefix, keySeat, keyKind, keyHumanText, keyCredential},
	apply.OpMemberReturn:  {keyTeamFile, keyHandle, keyCredential},
	apply.OpMemberReissue: {keyTeamFile, keyHandle, keyCredential},
	apply.OpMemberPark:    {keyTeamFile, keyHandle},
	apply.OpMemberRetire:  {keyTeamFile, keyHandle},
	apply.OpMemberSet:     {keyTeamFile, keyHandle, keyHumanText},
	apply.OpRoleApply:     {keyTeamFile},
	apply.OpTeamApply:     {"input", "approved", "reg_key"},
}

// The args' keys; each flag of a member verb is the key of the same name (--human-text is
// human_text).
const (
	keyTeamFile   = "team_file"
	keyHandle     = "handle"
	keyPrefix     = "prefix"
	keySeat       = "seat"
	keyKind       = "kind"
	keyHumanText  = "human_text"
	keyCredential = "credential"
)

// Serve runs one request in the child and answers it (CV1, CV2).
func Serve(ctx context.Context, open Opener, req Request) Answer {
	want, served := argKeys[req.Op]
	if !served {
		return failure(ClassFailed, "op "+req.Op+" is not served by this build")
	}
	args, err := exactKeys(req.Args, want...)
	if err != nil {
		return failure(ClassFailed, "op "+req.Op+" args: "+err.Error())
	}
	if req.Op == apply.OpTeamApply {
		return serveTeamApply(ctx, open, req.Team, args)
	}
	team, err := teamFile(args[keyTeamFile], req.Team)
	if err != nil {
		return failure(ClassFailed, err.Error())
	}
	var mr apply.MemberRequest
	if req.Op != apply.OpRoleApply {
		if mr, err = memberRequest(req.Op, args); err != nil {
			return failure(ClassFailed, "op "+req.Op+" args: "+err.Error())
		}
	}
	o, err := open(ctx, req.Team, team.Port, "")
	if err != nil {
		return failure(ClassConfig, "the team's admin files: "+err.Error())
	}
	if req.Op == apply.OpRoleApply {
		return answerOf(apply.RoleApply(ctx, o.Deps, team))
	}
	return answerOf(apply.RunMember(ctx, o.Deps, team, mr))
}

func failure(class ErrorClass, message string) Answer {
	return Answer{Error: &AnswerError{Class: class, Message: oneLine(message)}}
}

// answerOf is a run's answer: on a failure its lines and class, and never its credential.
func answerOf(res apply.Result, err error) Answer {
	if err != nil {
		a := failure(errorClass(err), err.Error())
		a.Changed = res.Changed
		return a
	}
	return Answer{OK: true, Changed: res.Changed, Handle: res.Handle, Credential: res.Credential, Notes: res.Notes}
}

// errorClass is the class whose exit code is C 3.3's for err.
func errorClass(err error) ErrorClass {
	code := ExitCodeOf(err)
	if code == ExitRefused {
		return ClassRefused
	}
	if code == ExitUnreachable {
		return ClassUnreachable
	}
	return ClassFailed
}

// teamFile is the args' `team.json`, which must be the request's team.
func teamFile(v proto.Value, id paths.TeamID) (declare.Team, error) {
	team, err := declare.DecodeTeamFile(v.Compact())
	if err != nil {
		return declare.Team{}, err
	}
	if team.ID != id {
		return declare.Team{}, fmt.Errorf("the team file is team %s, not %s", team.ID, id)
	}
	return team, nil
}

func memberRequest(op string, args map[string]proto.Value) (apply.MemberRequest, error) {
	mr := apply.MemberRequest{Op: op}
	var errs []error
	str := func(key string, into *string) {
		if v, given := args[key]; given {
			if v.Kind() != proto.KindString {
				errs = append(errs, fmt.Errorf("%s is not a string", key))
			}
			*into = v.Str()
		}
	}
	str(keyHandle, &mr.Handle)
	str(keyPrefix, &mr.Prefix)
	str(keyKind, &mr.Kind)
	str(keyCredential, &mr.Credential)
	if v, given := args[keyHumanText]; given {
		if v.Kind() != proto.KindBool {
			errs = append(errs, errors.New("human_text is not a boolean"))
		}
		mr.HumanText = v.Kind() == proto.KindBool && v.Bool()
	}
	if v, given := args[keySeat]; given {
		if v.Kind() != proto.KindInt || v.Int() < 0 || v.Int() > maxSeat {
			errs = append(errs, fmt.Errorf("seat is not a number from 0 (next) to %d", maxSeat))
		} else {
			mr.Seat = int(v.Int())
		}
	}
	return mr, errors.Join(errs...)
}

// maxSeat is the highest seat number a request may name.
const maxSeat = 9999

func serveTeamApply(ctx context.Context, open Opener, id paths.TeamID, args map[string]proto.Value) Answer {
	in, err := declare.DecodeInput(args["input"].Compact())
	if err != nil {
		return failure(ClassFailed, "op team.apply input: "+err.Error())
	}
	if in.ID() != id {
		return failure(ClassFailed, fmt.Sprintf("op team.apply input is team %s, not %s", in.ID(), id))
	}
	g, err := declare.DecodeApproved(args["approved"].Compact(), id)
	if err != nil {
		return failure(ClassFailed, "op team.apply approved: "+err.Error())
	}
	key := args["reg_key"]
	if key.Kind() != proto.KindString {
		return failure(ClassFailed, "op team.apply reg_key is not a string")
	}
	o, err := open(ctx, id, in.Team.Port, key.Str())
	if err != nil {
		return failure(ClassConfig, "the team's admin files: "+err.Error())
	}
	d := o.Deps
	d.Approvals = carried{g}
	return answerOf(apply.Run(ctx, d, in))
}

// carried is the approval root read and carried in the request (n2d-D7).
type carried struct{ g declare.Gated }

func (c carried) Load(paths.TeamID) (*declare.Gated, error) { return &c.g, nil }
