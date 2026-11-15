package admin

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// keyName is a human verb's --name flag and its child arg (Plan 00209 SPEC-n2h section 5).
const keyName = "name"

// HumanAnswer is a human op's own answer (HU13): `human.password`'s new password, or
// `human.devices`' devices. It is not a Credential, which is a seat's and needs a main room
// beside it (CH3).
type HumanAnswer struct {
	Password string
	Devices  []matrix.Device
}

// humanClearPrompt follows a new password on the alternate screen.
const humanClearPrompt = "\nThis is the human's new password, shown once: give it to them now.\nPress Enter to clear it from the screen.\n"

var humanPasswordForm = regexp.MustCompile(`^[A-Za-z0-9]{32}$`)

// humanVerb is `human password`, `human logout` or `human devices` (HV5): the flags, decided
// before any seam (HV4), the team lock (HV6), `team.json`, the child, the status record, and
// one JSON document on stdout (HV3), a password on the secret screen at a terminal.
func humanVerb(op string) func(context.Context, Env, []string) term.ExitCode {
	return func(ctx context.Context, e Env, args []string) term.ExitCode {
		team, name, err := humanFlags(args)
		if err != nil {
			return failWith(e, err)
		}
		sys, code := e.system()
		if code != term.ExitOK {
			return code
		}
		lock, err := teamlock.Take(ctx, sys.Host.LockDir, team, op, SeatWait, sys.Host.Clock, sys.Host.Guard)
		if err != nil {
			return failWith(e, err)
		}
		doc, secret, err := humanWork(ctx, sys, op, team, name)
		err = errors.Join(err, lock.Release())
		if doc != nil {
			if code = answerOnScreen(e, doc, secret, humanClearPrompt); code != term.ExitOK {
				return code
			}
		}
		if err != nil {
			return failWith(e, err)
		}
		return term.ExitOK
	}
}

func humanFlags(args []string) (paths.TeamID, string, error) {
	flags, err := parseFlags(args, []string{flagTeam, keyName})
	if err != nil {
		return paths.TeamID{}, "", err
	}
	team, err := teamFlag(flags)
	if err != nil {
		return paths.TeamID{}, "", err
	}
	name, given := flags[keyName]
	switch {
	case !given:
		return paths.TeamID{}, "", usagef("--name is required")
	case !proto.ValidHumanName(name):
		return paths.TeamID{}, "", usagef("--name %q is not a human's name", name)
	}
	return team, name, nil
}

// humanWork is a human verb's work under the team lock: the answer document (nil when there is
// none to print), whether it holds a password, and every failure joined. The status record is
// refreshed after the child, whatever it answered (HU10, HV8).
func humanWork(ctx context.Context, sys *System, op string, team paths.TeamID, name string) ([]byte, bool, error) {
	_, tf, err := readTeamFile(sys.Host.Root, team)
	if err != nil {
		return nil, false, err
	}
	a, err := ask(ctx, sys, op, team, proto.Obj(proto.M(keyTeamFile, tf), proto.M(keyName, proto.Str(name))))
	if a == nil {
		return nil, false, err
	}
	if a.OK {
		if lie := humanAsked(op, *a); lie != nil {
			return nil, false, lie
		}
	}
	if serr := sys.Host.Status.Refresh(ctx, team, false, nil); serr != nil {
		err = errors.Join(err, fmt.Errorf("the status record: %w", serr))
	}
	if !a.OK && len(a.Changed) == 0 {
		return nil, false, err
	}
	members := []proto.Member{proto.M("v", proto.Int(wireMajor)), proto.M("team", proto.Str(team.String())), proto.M(keyName, proto.Str(name))}
	if op == apply.OpHumanDevices && a.OK {
		return proto.Obj(append(members, proto.M("devices", encodeDevices(a.Human.Devices)))...).Compact(), false, err
	}
	members = append(members, proto.M("changed", lineArray(a.Changed)))
	secret := op == apply.OpHumanPassword && a.OK
	if secret {
		members = append(members, proto.M("password", proto.Str(a.Human.Password)))
	}
	return proto.Obj(members...).Compact(), secret, err
}

// humanAsked checks a human op's answer before root prints it (HV7, HU2): no handle,
// credential, main room or notes, a human answer only where the op gives one, and a password of
// the one form HU2 makes. No error quotes the password.
func humanAsked(op string, a Answer) error {
	lie := func(what string) error { return childLie(what, "nothing is printed") }
	if a.Handle != "" || a.Credential != nil || a.MainRoom != "" || len(a.Notes) > 0 {
		return lie("a handle, a credential, a main room or notes")
	}
	if err := answerFits(op, a); err != nil {
		return lie("an answer of another op's shape")
	}
	if op == apply.OpHumanPassword && !humanPasswordForm.MatchString(a.Human.Password) {
		return lie("a password that is not 32 characters of [A-Za-z0-9]")
	}
	return nil
}

// serveHuman runs a human op in the child (CH1).
func serveHuman(ctx context.Context, open Opener, op string, id paths.TeamID, team declare.Team, args map[string]proto.Value) Answer {
	n := args[keyName]
	if n.Kind() != proto.KindString || !proto.ValidHumanName(n.Str()) {
		return failure(ClassFailed, "op "+op+" args: name is not a human's name")
	}
	o, err := open(ctx, id, team.Port, "")
	if err != nil {
		return openFailure(err)
	}
	res, err := apply.RunHuman(ctx, o.Deps, team, apply.HumanRequest{Op: op, Name: n.Str()})
	a := answerOf(res, err)
	if err != nil {
		return a
	}
	switch op {
	case apply.OpHumanPassword:
		a.Human = &HumanAnswer{Password: res.Password.Reveal()}
	case apply.OpHumanDevices:
		a.Human = &HumanAnswer{Devices: res.Devices}
	}
	return a
}
