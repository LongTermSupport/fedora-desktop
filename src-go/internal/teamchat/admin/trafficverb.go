package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/traffic"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// OpTeamTraffic is the child's traffic read (Plan 00268 Task 1.3): read-only, counts only.
const OpTeamTraffic = "team.traffic"

// The traffic verb's flags and keys beyond the shared ones.
const (
	keyUntilMS  = "until_ms"
	keyTraffic  = "traffic"
	flagSince   = "since"
	flagUntil   = "until"
	flagJSON    = "json"
	verbTraffic = "team traffic"
)

// teamTraffic is `team traffic --team T --since <RFC 3339> --until <RFC 3339> [--json]`: the
// team's event counts over the window, read by the child as the team's admin, printed as a
// table, or as one JSON document with --json. It writes nothing and takes no team lock: a long
// read must not hold up a seat's start, and nothing it reads is changed by one.
func teamTraffic(ctx context.Context, e Env, args []string) term.ExitCode {
	team, w, asJSON, err := trafficFlags(args)
	if err != nil {
		return failWith(e, err)
	}
	sys, code := e.system()
	if code != term.ExitOK {
		return code
	}
	_, tf, err := readTeamFile(sys.Host.Root, team)
	if err != nil {
		return failWith(e, err)
	}
	a, err := ask(ctx, sys, OpTeamTraffic, team, proto.Obj(
		proto.M(keyTeamFile, tf), proto.M(keySinceMS, proto.Int(w.SinceMS)), proto.M(keyUntilMS, proto.Int(w.UntilMS)),
	))
	if err != nil {
		return failWith(e, err)
	}
	if lie := trafficAsked(*a, w); lie != nil {
		return failWith(e, lie)
	}
	if asJSON {
		doc, err := traffic.JSON(team.String(), *a.Traffic)
		if err != nil {
			return fail(e.Stderr, term.ExitFailure, "cannot encode the counts: "+err.Error())
		}
		return answer(e, string(doc)+"\n")
	}
	if err := traffic.WriteTable(e.Stdout, team.String(), *a.Traffic); err != nil {
		return fail(e.Stderr, term.ExitFailure, "cannot write the answer: "+err.Error())
	}
	return term.ExitOK
}

func trafficFlags(args []string) (paths.TeamID, traffic.Window, bool, error) {
	flags, err := parseFlags(args, []string{flagTeam, flagSince, flagUntil}, flagJSON)
	if err != nil {
		return paths.TeamID{}, traffic.Window{}, false, err
	}
	team, err := teamFlag(flags)
	if err != nil {
		return paths.TeamID{}, traffic.Window{}, false, err
	}
	var times [2]time.Time
	for i, name := range []string{flagSince, flagUntil} {
		v, given := flags[name]
		if !given {
			return paths.TeamID{}, traffic.Window{}, false, usagef("--%s is required", name)
		}
		if times[i], err = time.Parse(time.RFC3339, v); err != nil {
			return paths.TeamID{}, traffic.Window{}, false, usagef("--%s %q is not an RFC 3339 time, such as 2026-11-25T17:00:00Z", name, v)
		}
	}
	w, err := traffic.NewWindow(times[0], times[1])
	if err != nil {
		return paths.TeamID{}, traffic.Window{}, false, usagef("--since and --until: %v", err)
	}
	return team, w, flags[flagJSON] == flagTrue, nil
}

// trafficAsked checks the child's answer before root prints it: a traffic answer, of the
// window asked, and nothing else.
func trafficAsked(a Answer, w traffic.Window) error {
	lie := func(what string) error { return childLie(what, "nothing is printed") }
	if a.Handle != "" || a.Credential != nil || a.MainRoom != "" || len(a.Notes) > 0 || len(a.Changed) > 0 {
		return lie("a handle, a credential, a main room, notes or changes")
	}
	if err := answerFits(OpTeamTraffic, a); err != nil {
		return lie("an answer of another op's shape")
	}
	if a.Traffic.Window != w {
		return lie("the counts of another window")
	}
	return nil
}

// serveTraffic is team.traffic in the child: the window's counts of the team's rooms, read
// through the admin client; no content leaves the child.
func serveTraffic(ctx context.Context, open Opener, team declare.Team, args map[string]proto.Value) Answer {
	since, until := args[keySinceMS], args[keyUntilMS]
	if since.Kind() != proto.KindInt || until.Kind() != proto.KindInt {
		return failure(ClassFailed, "op "+OpTeamTraffic+" args: since_ms and until_ms are not integers")
	}
	w := traffic.Window{SinceMS: since.Int(), UntilMS: until.Int()}
	if err := w.Valid(); err != nil {
		return failure(ClassFailed, "op "+OpTeamTraffic+" args: "+err.Error())
	}
	o, err := open(ctx, team.ID, team.Port, "")
	if err != nil {
		return openFailure(err)
	}
	c, ok := o.Admin.(messagesReader)
	if !ok {
		return failure(ClassConfig, "the team's admin client cannot read events")
	}
	ids, err := roomsOf(team, o.Deps)
	if err != nil {
		return failure(ClassConfig, "the team's files: "+err.Error())
	}
	var rooms []traffic.Room
	for _, r := range roomList(ids) {
		rooms = append(rooms, traffic.Room{Purpose: r.purpose, ID: r.id})
	}
	counts, err := traffic.Read(ctx, c, rooms, w, traffic.Options{})
	if err != nil {
		return failure(homeserverClass(err), fmt.Sprintf("the traffic read: %v", err))
	}
	return Answer{OK: true, Traffic: &counts}
}

// validateTraffic is a traffic answer: of its own, and counts Validate accepts.
func (a Answer) validateTraffic() error {
	if a.Handle != "" || a.Credential != nil || len(a.Notes) > 0 || a.View != nil || a.Notice != nil || a.restoreOrCarry() || a.Human != nil {
		return fmt.Errorf("a traffic answer is an answer of its own")
	}
	return a.Traffic.Validate()
}
