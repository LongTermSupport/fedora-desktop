package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// maxDeclaration caps `lxc apply`'s stdin, the whole declaration (n3-D21).
const maxDeclaration = 1 << 20

const flagMember = "--member"

// lxcApply is `lxc apply --phase absent|present [--only-teams T,...] [--member C/K]...` (C
// 3.9.2): the declaration on stdin, then lxc.Apply. stdout is the report's lines and
// `LXC-SUMMARY\t...`, printed before any error.
func lxcApply(ctx context.Context, e Env, args []string) term.ExitCode {
	sys, code := e.system()
	if code != term.ExitOK {
		return code
	}
	req, err := lxcApplyArgs(args)
	if err != nil {
		return failWith(e, err)
	}
	if req.Decl, err = readDeclaration(e.Stdin); err != nil {
		return failWith(e, err)
	}
	d, code := lxcDeps(e, sys)
	if code != term.ExitOK {
		return code
	}
	rep, err := lxc.Apply(ctx, d, req)
	var b strings.Builder
	for _, l := range rep.Lines {
		b.WriteString(l.String() + "\n")
	}
	if rep.Summary != "" {
		b.WriteString("LXC-SUMMARY\t" + rep.Summary + "\n")
	}
	if code = answer(e, b.String()); code != term.ExitOK {
		return code
	}
	if err != nil {
		return failWith(e, err)
	}
	return term.ExitOK
}

// lxcApplyArgs reads the flags: --member may be given once per seat, the others once each.
func lxcApplyArgs(args []string) (lxc.Request, error) {
	var req lxc.Request
	members, rest, err := targetFlags(args)
	if err != nil {
		return lxc.Request{}, err
	}
	req.Members = members
	flags, err := parseFlags(rest, []string{"phase", "only-teams"})
	if err != nil {
		return lxc.Request{}, err
	}
	switch p := lxc.Phase(flags["phase"]); p {
	case lxc.PhaseAbsent, lxc.PhasePresent:
		req.Phase = p
	default:
		return lxc.Request{}, usagef("--phase is absent or present")
	}
	if v, given := flags["only-teams"]; given {
		for s := range strings.SplitSeq(v, ",") {
			id, err := paths.ParseTeamID(s)
			if err != nil {
				return lxc.Request{}, usagef("--only-teams: %v", err)
			}
			if slices.Contains(req.OnlyTeams, id) {
				return lxc.Request{}, usagef("--only-teams names %s twice", id)
			}
			req.OnlyTeams = append(req.OnlyTeams, id)
		}
	}
	return req, nil
}

// targetFlags are the --member targets, each seat once, and the arguments beside them.
func targetFlags(args []string) ([]lxc.Target, []string, error) {
	var members []lxc.Target
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] != flagMember {
			rest = append(rest, args[i])
			continue
		}
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			return nil, nil, usagef("%s needs a value", flagMember)
		}
		i++
		t, err := parseTarget(args[i])
		if err != nil {
			return nil, nil, err
		}
		if slices.Contains(members, t) {
			return nil, nil, usagef("%s %s is given twice", flagMember, args[i])
		}
		members = append(members, t)
	}
	return members, rest, nil
}

// parseTarget is `--member C/K`: a container and a seat key (n3b-D2).
func parseTarget(s string) (lxc.Target, error) {
	c, k, found := strings.Cut(s, "/")
	if !found {
		return lxc.Target{}, usagef("%s is <container>/<seat key>, not %q", flagMember, s)
	}
	box, err := paths.ParseContainerName(c)
	if err != nil {
		return lxc.Target{}, usagef("%s: %v", flagMember, err)
	}
	key, err := paths.ParseSeatKey(k)
	if err != nil {
		return lxc.Target{}, usagef("%s: %v", flagMember, err)
	}
	return lxc.Target{Container: box, Key: key}, nil
}

// readDeclaration is the declaration on stdin, read under maxDeclaration.
func readDeclaration(stdin io.Reader) (declare.Declaration, error) {
	raw, err := io.ReadAll(io.LimitReader(stdin, maxDeclaration+1))
	if err != nil {
		return declare.Declaration{}, fmt.Errorf("cannot read the declaration: %w", err)
	}
	if len(raw) > maxDeclaration {
		return declare.Declaration{}, fmt.Errorf("the declaration is over %d bytes", maxDeclaration)
	}
	return declare.Decode(raw)
}

// lxcRestart is `lxc restart --member C/K` (SU3), the target form `lxc apply` takes, given
// once: lxc.Restart, nothing on stdin or stdout.
func lxcRestart(ctx context.Context, e Env, args []string) term.ExitCode {
	sys, code := e.system()
	if code != term.ExitOK {
		return code
	}
	members, rest, err := targetFlags(args)
	if err == nil {
		_, err = parseFlags(rest, nil)
	}
	if err != nil {
		return failWith(e, err)
	}
	if len(members) != 1 {
		return fail(e.Stderr, term.ExitUsage, "lxc restart takes one --member <container>/<seat key>")
	}
	t := members[0]
	d, code := lxcDeps(e, sys)
	if code != term.ExitOK {
		return code
	}
	if err := lxc.Restart(ctx, d, t.Container, t.Key); err != nil {
		return failWith(e, err)
	}
	return term.ExitOK
}

// lxcDeps is System.LXC's Deps with the seams only the verbs have: the child's member work as
// the Roster and the status record as Refresh. None wired is 1, one that fails 78.
func lxcDeps(e Env, sys *System) (lxc.Deps, term.ExitCode) {
	if sys.LXC == nil {
		return lxc.Deps{}, fail(e.Stderr, term.ExitFailure, "the LXC seams are not wired")
	}
	d, err := sys.LXC()
	if err != nil {
		return lxc.Deps{}, fail(e.Stderr, ExitConfig, "the LXC seams: "+err.Error())
	}
	out := *d
	out.Roster = lxcRoster{sys: sys}
	out.Refresh = func(ctx context.Context, team paths.TeamID) error { return sys.Host.Status.Refresh(ctx, team, false) }
	return out, term.ExitOK
}

// lxcRoster is lxc.Roster over the child (gap 20): each call is memberWork, made while
// lxc.Apply holds the team's lock, so asked still checks every answer.
type lxcRoster struct{ sys *System }

// Members is the team's roster states by handle, from the child's team.view.
func (r lxcRoster) Members(ctx context.Context, team paths.TeamID) (map[string]string, error) {
	v, err := ChildViewer{Child: r.sys.Child, Root: r.sys.Host.Root}.View(ctx, team)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(v.Members))
	for _, m := range v.Members {
		out[m.Handle] = m.State
	}
	return out, nil
}

// Join is member add, return or reissue with a token credential, and an add's kind lxc: the
// args memberArgs would build. A retired account is apply.ErrRetired.
func (r lxcRoster) Join(ctx context.Context, team paths.TeamID, j lxc.Join) (lxc.Joined, error) {
	token := proto.M(keyCredential, proto.Str(apply.CredToken))
	var args []proto.Member
	switch j.Op {
	case apply.OpMemberAdd:
		args = []proto.Member{
			proto.M(keyPrefix, proto.Str(j.Prefix.String())), proto.M(keySeat, proto.Int(int64(j.Seat))),
			proto.M(keyKind, proto.Str(status.KindLXC)), proto.M(keyHumanText, proto.Bool(j.HumanText)), token,
		}
	case apply.OpMemberReturn, apply.OpMemberReissue:
		args = []proto.Member{proto.M(keyHandle, proto.Str(j.Handle)), token}
	default:
		return lxc.Joined{}, fmt.Errorf("%s is not a join", j.Op)
	}
	w, err := memberWork(ctx, r.sys, j.Op, team, args)
	if ce, ok := errors.AsType[*ChildError](err); ok && ce.Class == ClassRetired {
		return lxc.Joined{}, fmt.Errorf("%w (%w)", err, apply.ErrRetired)
	}
	if err != nil {
		return lxc.Joined{}, err
	}
	return lxc.Joined{Handle: w.answer.Handle, Bundle: w.bundle.Compact()}, nil
}

// Park is member park.
func (r lxcRoster) Park(ctx context.Context, team paths.TeamID, handle string) error {
	_, err := memberWork(ctx, r.sys, apply.OpMemberPark, team, []proto.Member{proto.M(keyHandle, proto.Str(handle))})
	return err
}

// Reach is guest check's stdin from the applied team.json: its URL and plain-HTTP hosts.
func (r lxcRoster) Reach(_ context.Context, team paths.TeamID) ([]byte, error) {
	decl, _, err := readTeamFile(r.sys.Host.Root, team)
	if err != nil {
		return nil, err
	}
	plain, err := plainHTTPHosts(decl.URL)
	if err != nil {
		return nil, err
	}
	return proto.Obj(
		proto.M("v", proto.Int(1)), proto.M("team", proto.Str(team.String())),
		proto.M("base_url", proto.Str(decl.URL)), proto.M("plain_http_hosts", lineArray(plain)),
	).Compact(), nil
}
