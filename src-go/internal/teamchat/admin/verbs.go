package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// System is what the verbs reach beyond the process, each a seam (CV3-CV5): the host's
// (host.Deps; Bootstrap and LockWait are the verb's), the child, the seat side's bundles and
// the owner group for root; for the child, its Opener.
type System struct {
	Host       host.Deps
	Child      Child
	Bundles    Bundles
	OwnerGroup func() (int, error)
	Open       Opener
}

// Child asks the teamchat-ctl child one request (CC1); its start is the seam's (n2a-D6).
type Child interface {
	Ask(ctx context.Context, req Request) (Answer, error)
}

// Bundles is the seat side's bundle for a credential the child issued (C 3.7.2), with the
// team's main room the child answered beside it (BU1).
type Bundles interface {
	Bundle(ctx context.Context, team declare.Team, handle, mainRoom string, c apply.Credential) (proto.Value, error)
}

// SeatWait is how long a member or role verb waits for the team lock: a launcher's seat take
// (C 3.4). The team verbs are the play's and wait host.PlayWait.
const SeatWait = 60 * time.Second

// The flags beside the args' keys they carry; a bool flag given reads flagTrue.
const (
	flagTeam      = "team"
	flagHumanText = "human-text"
	flagTrue      = "true"
)

// maxTeamFile caps the `team.json` root reads.
const maxTeamFile = 64 << 10

// ChildError is a failure the child answered; its class decides the exit code (CC6).
type ChildError struct {
	Class   ErrorClass
	Message string
}

func (e *ChildError) Error() string { return e.Message }

// refusal is a rule of a verb's own that said no (70).
type refusal struct{ problem string }

func (e *refusal) Error() string { return e.problem }

// usageError is a bad flag or argument (64).
type usageError struct{ problem string }

func (e *usageError) Error() string { return e.problem }

func usagef(format string, args ...any) error { return &usageError{fmt.Sprintf(format, args...)} }

// rootRuns are the verbs this slice builds.
func rootRuns() map[string]func(context.Context, Env, []string) term.ExitCode {
	runs := map[string]func(context.Context, Env, []string) term.ExitCode{
		"team apply": teamApply, "team approve": teamApprove, "team remove": teamRemove,
		"role apply": memberVerb(apply.OpRoleApply),
	}
	for _, op := range []string{apply.OpMemberAdd, apply.OpMemberReturn, apply.OpMemberReissue, apply.OpMemberPark, apply.OpMemberRetire, apply.OpMemberSet} {
		runs["member "+strings.TrimPrefix(op, "member.")] = memberVerb(op)
	}
	return runs
}

// memberFlags are each member and role verb's flags after --team, every one required (C 3.6.3).
var memberFlags = map[string][]string{
	apply.OpMemberAdd:     {keyPrefix, keySeat, keyKind, flagHumanText, keyCredential},
	apply.OpMemberReturn:  {keyHandle, keyCredential},
	apply.OpMemberReissue: {keyHandle, keyCredential},
	apply.OpMemberPark:    {keyHandle},
	apply.OpMemberRetire:  {keyHandle},
	apply.OpMemberSet:     {keyHandle, flagHumanText},
	apply.OpRoleApply:     {},
}

func (e Env) system() (*System, term.ExitCode) {
	if e.Sys == nil {
		return nil, fail(e.Stderr, term.ExitFailure, "this build has its verbs but no host seams: they are not wired yet")
	}
	return e.Sys, term.ExitOK
}

func failWith(e Env, err error) term.ExitCode {
	if u, ok := errors.AsType[*usageError](err); ok {
		return fail(e.Stderr, term.ExitUsage, u.problem)
	}
	return fail(e.Stderr, ExitCodeOf(err), err.Error())
}

// memberVerb is a member or role verb (CV3): its flags, the team lock, `team.json`, the child,
// the status record, and one JSON document on stdout.
func memberVerb(op string) func(context.Context, Env, []string) term.ExitCode {
	return func(ctx context.Context, e Env, args []string) term.ExitCode {
		sys, code := e.system()
		if code != term.ExitOK {
			return code
		}
		team, opArgs, err := memberArgs(op, args)
		if err != nil {
			return failWith(e, err)
		}
		lock, err := teamlock.Take(ctx, sys.Host.LockDir, team, op, SeatWait, sys.Host.Clock)
		if err != nil {
			return failWith(e, err)
		}
		out, err := underLock(ctx, sys, op, team, opArgs)
		err = errors.Join(err, lock.Release())
		if out != nil {
			if code = answer(e, string(out)+"\n"); code != term.ExitOK {
				return code
			}
		}
		if err != nil {
			return failWith(e, err)
		}
		return term.ExitOK
	}
}

func memberArgs(op string, args []string) (paths.TeamID, []proto.Member, error) {
	names := memberFlags[op]
	flags, err := parseFlags(args, append([]string{flagTeam}, names...))
	if err != nil {
		return paths.TeamID{}, nil, err
	}
	team, err := teamFlag(flags)
	if err != nil {
		return paths.TeamID{}, nil, err
	}
	var members []proto.Member
	for _, name := range names {
		v, given := flags[name]
		if !given {
			return paths.TeamID{}, nil, usagef("--%s is required", name)
		}
		arg, err := flagArg(name, v)
		if err != nil {
			return paths.TeamID{}, nil, err
		}
		members = append(members, proto.M(strings.ReplaceAll(name, "-", "_"), arg))
	}
	return team, members, nil
}

func teamFlag(flags map[string]string) (paths.TeamID, error) {
	v, given := flags[flagTeam]
	if !given {
		return paths.TeamID{}, usagef("--team is required")
	}
	team, err := paths.ParseTeamID(v)
	if err != nil {
		return paths.TeamID{}, usagef("--team: %v", err)
	}
	return team, nil
}

// flagArg is a flag's value as the child's arg: --seat a number from 1 or `next` (0),
// --human-text true or false, --credential password or token.
func flagArg(name, v string) (proto.Value, error) {
	switch name {
	case keySeat:
		if v == "next" {
			return proto.Int(0), nil
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxSeat || strconv.Itoa(n) != v {
			return proto.Value{}, usagef("--seat is a number from 1 to %d, or next", maxSeat)
		}
		return proto.Int(int64(n)), nil
	case flagHumanText:
		if v != flagTrue && v != "false" {
			return proto.Value{}, usagef("--human-text is true or false")
		}
		return proto.Bool(v == flagTrue), nil
	case keyCredential:
		if v != apply.CredPassword && v != apply.CredToken {
			return proto.Value{}, usagef("--credential is password or token")
		}
	}
	return proto.Str(v), nil
}

// parseFlags reads `--name value` for values and a bare `--name` for bools, each at most once,
// and nothing else.
func parseFlags(args, values []string, bools ...string) (map[string]string, error) {
	out := map[string]string{}
	for i := 0; i < len(args); i++ {
		name, isFlag := strings.CutPrefix(args[i], "--")
		if !isFlag {
			return nil, usagef("unexpected argument %q", args[i])
		}
		if _, seen := out[name]; seen {
			return nil, usagef("--%s is given twice", name)
		}
		switch {
		case slices.Contains(bools, name):
			out[name] = flagTrue
		case slices.Contains(values, name):
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return nil, usagef("--%s needs a value", name)
			}
			i++
			out[name] = args[i]
		default:
			return nil, usagef("unknown flag %q", args[i])
		}
	}
	return out, nil
}

// underLock is a member or role verb's work under the team lock: the answer document, and
// every failure (the child's, the status record's, the bundle's) joined.
func underLock(ctx context.Context, sys *System, op string, team paths.TeamID, args []proto.Member) ([]byte, error) {
	decl, tf, err := readTeamFile(sys.Host.Root, team)
	if err != nil {
		return nil, err
	}
	a, err := ask(ctx, sys, op, team, proto.Obj(append([]proto.Member{proto.M(keyTeamFile, tf)}, args...)...))
	if a == nil {
		return nil, err
	}
	if a.OK {
		if lie := asked(op, args, *a); lie != nil {
			return nil, lie
		}
	}
	if serr := sys.Host.Status.Refresh(ctx, team, false); serr != nil {
		err = errors.Join(err, fmt.Errorf("the status record: %w", serr))
	}
	members := []proto.Member{proto.M("v", proto.Int(wireMajor)), proto.M("changed", lineArray(a.Changed))}
	if a.Handle != "" {
		members = append(members, proto.M(keyHandle, proto.Str(a.Handle)))
	}
	if len(a.Notes) > 0 {
		members = append(members, proto.M("notes", lineArray(a.Notes)))
	}
	if a.Credential != nil {
		b, berr := sys.Bundles.Bundle(ctx, decl, a.Handle, a.MainRoom, *a.Credential)
		if berr != nil {
			err = errors.Join(err, fmt.Errorf("the bundle of %s: %w; member reissue issues a new credential", a.Handle, berr))
		} else {
			members = append(members, proto.M("bundle", b))
		}
	}
	return proto.Obj(members...).Compact(), err
}

// asked checks a child's answer against the request before root acts on it (C 3.3: the answer
// is the one thing root parses that a less-privileged process wrote): the handle asked for (for
// an add, the prefix's, numbered by the seat unless it is next), and a credential of the kind
// asked for exactly when the verb issues one.
func asked(op string, args []proto.Member, a Answer) error {
	byKey := map[string]proto.Value{}
	for _, m := range args {
		byKey[m.Key] = m.Val
	}
	req, err := memberRequest(op, byKey)
	if err != nil {
		return err
	}
	lie := func(what string) error {
		return fmt.Errorf("the %s child answered %s, not what it was asked: no bundle is made", paths.CtlUser, what)
	}
	if !answeredHandle(req, a.Handle) {
		return lie("handle " + strconv.Quote(a.Handle))
	}
	if err := answerFits(op, a); err != nil {
		return lie("a view or a notice")
	}
	issues := op == apply.OpMemberAdd || op == apply.OpMemberReturn || op == apply.OpMemberReissue
	switch {
	case issues && (a.Credential == nil || a.MainRoom == ""):
		return lie("no credential or no main room")
	case !issues && (a.Credential != nil || a.MainRoom != ""):
		return lie("a credential or a main room")
	case issues && a.Credential.Kind != req.Credential:
		return lie("a " + a.Credential.Kind + " credential")
	}
	return nil
}

func answeredHandle(req apply.MemberRequest, handle string) bool {
	switch req.Op {
	case apply.OpRoleApply:
		return handle == ""
	case apply.OpMemberAdd:
		h, ok := proto.ParseHandle(handle)
		if !ok || h.Prefix().String() != req.Prefix {
			return false
		}
		n, numbered := h.SeatNumber()
		return numbered && (req.Seat == 0 || n == req.Seat)
	}
	return handle == req.Handle
}

// readTeamFile is the applied `team.json` (DC18): a member verb never takes a declaration.
func readTeamFile(root string, team paths.TeamID) (declare.Team, proto.Value, error) {
	raw, found, err := state.ReadFile(root+paths.TeamDeclaration(team), maxTeamFile)
	if err != nil {
		return declare.Team{}, proto.Value{}, err
	}
	if !found {
		return declare.Team{}, proto.Value{}, &refusal{"team " + team.String() + " is not applied: run team apply first"}
	}
	decl, err := declare.DecodeTeamFile(raw)
	if err != nil {
		return declare.Team{}, proto.Value{}, err
	}
	v, err := proto.Parse(raw)
	return decl, v, err
}

// ask is the child's answer, nil when none came; a failed one is a ChildError beside it.
func ask(ctx context.Context, sys *System, op string, team paths.TeamID, args proto.Value) (*Answer, error) {
	a, err := sys.Child.Ask(ctx, Request{Op: op, Team: team, Args: args})
	if err != nil {
		return nil, fmt.Errorf("the %s child: %w", paths.CtlUser, err)
	}
	if !a.OK {
		return &a, &ChildError{Class: a.Error.Class, Message: a.Error.Message}
	}
	return &a, nil
}

// printChanged prints the play's CHANGED lines, those before a failure included, then the
// failure.
func printChanged(e Env, lines []string, err error) term.ExitCode {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString("CHANGED\t" + line + "\n")
	}
	if code := answer(e, b.String()); code != term.ExitOK {
		return code
	}
	if err != nil {
		return failWith(e, err)
	}
	return term.ExitOK
}

// readInput is the one-team input on stdin (DR7).
func readInput(stdin io.Reader) (declare.Input, error) {
	raw, err := io.ReadAll(io.LimitReader(stdin, maxRequest+1))
	if err != nil {
		return declare.Input{}, fmt.Errorf("cannot read the input: %w", err)
	}
	if len(raw) > maxRequest {
		return declare.Input{}, fmt.Errorf("the input is over %d bytes", maxRequest)
	}
	return declare.DecodeInput(raw)
}

// teamApply is C 3.5.2 (CV5): host.Apply with step 10 the child's team.apply.
func teamApply(ctx context.Context, e Env, args []string) term.ExitCode {
	sys, code := e.system()
	if code != term.ExitOK {
		return code
	}
	if len(args) > 0 {
		return fail(e.Stderr, term.ExitUsage, "team apply takes no arguments: its input is the team's declaration on stdin")
	}
	in, err := readInput(e.Stdin)
	if err != nil {
		return failWith(e, err)
	}
	d := sys.Host
	d.LockWait = host.PlayWait
	d.Bootstrap = func(ctx context.Context, b host.Bootstrap) ([]string, error) { return bootstrap(ctx, sys, b) }
	lines, err := host.Apply(ctx, d, in)
	return printChanged(e, lines, err)
}

// bootstrap asks the child for team.apply with what only root can read (n2d-D7): the input,
// the approval and the registration key.
func bootstrap(ctx context.Context, sys *System, b host.Bootstrap) ([]string, error) {
	id := b.Input.ID()
	approved, err := sys.Host.Approvals.Load(id)
	if err != nil {
		return nil, err
	}
	if approved == nil {
		return nil, &refusal{"team " + id.String() + " has no approval"}
	}
	in, err := proto.Parse(b.Input.Encode())
	if err != nil {
		return nil, err
	}
	g, err := proto.Parse(declare.EncodeApproved(id, *approved))
	if err != nil {
		return nil, err
	}
	a, err := ask(ctx, sys, apply.OpTeamApply, id, proto.Obj(proto.M("input", in), proto.M("approved", g), proto.M("reg_key", proto.Str(b.RegKey))))
	if a == nil {
		return nil, err
	}
	return a.Changed, err
}

// teamRemove is C 3.5.3 (CV5).
func teamRemove(ctx context.Context, e Env, args []string) term.ExitCode {
	sys, code := e.system()
	if code != term.ExitOK {
		return code
	}
	flags, err := parseFlags(args, []string{flagTeam}, "purge")
	if err != nil {
		return failWith(e, err)
	}
	team, err := teamFlag(flags)
	if err != nil {
		return failWith(e, err)
	}
	d := sys.Host
	d.LockWait = host.PlayWait
	lines, err := host.Remove(ctx, d, team, flags["purge"] == flagTrue)
	return printChanged(e, lines, err)
}

// changeOps are `team approve`'s one-field changes (AG6).
var changeOps = map[string]declare.Op{"add": declare.OpAdd, "remove": declare.OpRemove, "set": declare.OpSet}

// teamApprove is CV4: the input on stdin approved, whole or by one field's change, into
// `approved.json` with the owner group's gid.
func teamApprove(_ context.Context, e Env, args []string) term.ExitCode {
	sys, code := e.system()
	if code != term.ExitOK {
		return code
	}
	team, ch, err := approveArgs(args)
	if err != nil {
		return failWith(e, err)
	}
	in, err := readInput(e.Stdin)
	if err != nil {
		return failWith(e, err)
	}
	if in.ID() != team {
		return fail(e.Stderr, term.ExitUsage, "the input is team "+in.ID().String()+", not "+team.String())
	}
	gid, err := sys.OwnerGroup()
	if err != nil {
		return fail(e.Stderr, ExitConfig, "the owner group: "+err.Error())
	}
	lines, err := approve(sys.Host.Root, declare.FileApprovals{Path: func(id paths.TeamID) string { return sys.Host.Root + paths.ApprovedFile(id) }, GID: gid}, in, ch)
	return printChanged(e, lines, err)
}

func approveArgs(args []string) (paths.TeamID, *declare.Change, error) {
	flags, err := parseFlags(args, []string{flagTeam, "only", "add", "remove", "set"})
	if err != nil {
		return paths.TeamID{}, nil, err
	}
	team, err := teamFlag(flags)
	if err != nil {
		return paths.TeamID{}, nil, err
	}
	var ch *declare.Change
	for name, op := range changeOps {
		if v, given := flags[name]; given {
			if ch != nil {
				return paths.TeamID{}, nil, usagef("--add, --remove and --set are one change")
			}
			ch = &declare.Change{Field: flags["only"], Op: op, Value: v}
		}
	}
	if _, only := flags["only"]; only != (ch != nil) {
		return paths.TeamID{}, nil, usagef("--only names the field of one change: --add, --remove or --set")
	}
	return team, ch, nil
}

// approve writes what declare.Approve answers when it differs from the approval, and answers
// one line per field that changed.
func approve(root string, fa declare.FileApprovals, in declare.Input, ch *declare.Change) ([]string, error) {
	approved, err := fa.Load(in.ID())
	if err != nil {
		return nil, err
	}
	g, err := declare.Approve(approved, in, ch)
	if err != nil {
		return nil, err
	}
	diffs := declare.Compare(approved, in)
	if len(diffs) == 0 {
		return nil, nil
	}
	if err = host.MkdirStatus(root, in.ID()); err != nil {
		return nil, err
	}
	if err = fa.Save(in.ID(), g); err != nil {
		return nil, err
	}
	var lines []string
	for _, d := range diffs {
		if line := "approved " + d.Field; !slices.Contains(lines, line) {
			lines = append(lines, line)
		}
	}
	return lines, nil
}
