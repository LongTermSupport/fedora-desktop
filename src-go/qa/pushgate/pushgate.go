// Package pushgate is the pre-push gate on qa-all's verdict (rule push-without-qa-verdict,
// issue #187): a push of commits the remote does not hold yet goes ahead only when the last
// qa-all run in this checkout passed, judged a commit that every pushed ref contains, and ended
// after each commit pushed on top of it was made (the burst of commits made while it ran shares
// its verdict). It reads the verdict qa-all wrote (qa/internal/qaverdict), never its printed
// text, which a grep reads as readily when it says FAILED.
package pushgate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/prepush"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/qaverdict"
)

// ExitCode is the gate's status: git refuses the push on any but ExitOK.
type ExitCode int

// 0 the push may go, 1 refused, 2 the gate could not judge (bad usage, git failing, a damaged
// verdict).
const (
	ExitOK        ExitCode = 0
	ExitRefused   ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// RuleID is the identifier every refusal carries; CLAUDE/QA.md documents it.
const RuleID = "push-without-qa-verdict"

// ExemptEnv names the variable that lets an owner's tool push without a verdict, with the reason
// as its value; the reason is printed. Agents never set it.
const ExemptEnv = "QA_PUSH_GATE_EXEMPT"

const short = 12

const usageText = `usage: qa-push-gate REMOTE [URL]   (git's pre-push arguments; the ref lines on stdin)

The pre-push gate on qa-all's verdict (rule ` + RuleID + `). A push of commits the remote does not
hold yet goes ahead only when the last qa-all run in this checkout passed, judged a commit every
pushed ref contains, and ended after each commit pushed on top of it was made. The verdict is
qa-all-verdict.json in the git common directory, written by ./scripts/qa-all.bash.

` + ExemptEnv + `=<reason>  an owner's tool pushing without a verdict; the reason is printed.

Exit codes: 0 the push may go, 1 refused, 2 the gate could not judge.
`

// Env is what the gate touches: the hook's stdin, the streams, git, the checkout and the
// environment.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Runner         proc.Runner
	Cwd            string
	Getenv         func(string) string
}

// refusal is a push the gate refuses; anything else that goes wrong is a run that cannot judge.
type refusal struct{ msg string }

func (r *refusal) Error() string { return r.msg }

func refuse(format string, args ...any) error {
	return &refusal{msg: fmt.Sprintf(format, args...)}
}

type gate struct {
	env     Env
	remote  string
	verdict *qaverdict.Record
}

// Main judges one pre-push run: args are git's (REMOTE, then URL), the ref lines on stdin.
func Main(ctx context.Context, args []string, env Env) ExitCode {
	out, errOut := emit.New(env.Stdout), emit.New(env.Stderr)
	code := judge(ctx, args, env, out, errOut)
	return emit.Finish(out, errOut, code, ExitCannotRun)
}

func judge(ctx context.Context, args []string, env Env, out, errOut *emit.Emitter) ExitCode {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		out.Block(usageText)
		return ExitOK
	}
	if len(args) == 0 || args[0] == "" || len(args) > 2 {
		errOut.Line("ERROR: qa-push-gate: expected REMOTE [URL]")
		errOut.Block(usageText)
		return ExitCannotRun
	}
	if reason := env.Getenv(ExemptEnv); reason != "" {
		errOut.Line("⚠ qa-push-gate: exempt (" + ExemptEnv + "): " + reason)
		return ExitOK
	}
	refs, err := prepush.Parse(env.Stdin)
	if err != nil {
		errOut.Line("ERROR: qa-push-gate: " + err.Error())
		return ExitCannotRun
	}
	g := &gate{env: env, remote: args[0]}
	pushed := 0
	for _, ref := range refs {
		n, err := g.judgeRef(ctx, ref)
		if refused, ok := errors.AsType[*refusal](err); ok {
			errOut.Line("✗ qa-push-gate [" + RuleID + "]: " + ref.LocalRef + ": " + refused.msg)
			errOut.Line("  Run ./scripts/qa-all.bash on what you push; push when it passes. See CLAUDE/QA.md#" + RuleID)
			return ExitRefused
		}
		if err != nil {
			errOut.Line("ERROR: qa-push-gate: " + ref.LocalRef + ": " + err.Error())
			return ExitCannotRun
		}
		pushed += n
	}
	if g.verdict == nil {
		out.Line("✓ qa-push-gate: no new commit to judge")
		return ExitOK
	}
	out.Line(fmt.Sprintf("✓ qa-push-gate: qa-all passed at %s; %d new commit(s) pushed", g.verdict.Commit[:short], pushed))
	return ExitOK
}

// judgeRef answers how many commits the ref pushes that the remote lacks, refusing it when the
// verdict does not cover them.
func (g *gate) judgeRef(ctx context.Context, ref prepush.Ref) (int, error) {
	if ref.Deletes() {
		return 0, nil
	}
	list, err := g.git(ctx, append([]string{"rev-list"}, ref.Selection(g.remote)...)...)
	if err != nil {
		return 0, err
	}
	fresh := strings.Fields(list)
	if len(fresh) == 0 {
		return 0, nil
	}
	v, err := g.load(ctx)
	if err != nil {
		return 0, err
	}
	if v.Status != qaverdict.StatusPass {
		return 0, refuse("the last qa-all run FAILED at %s", v.Commit[:short])
	}
	if err := g.contains(ctx, v, ref); err != nil {
		return 0, err
	}
	if err := g.sameBurst(ctx, v, ref); err != nil {
		return 0, err
	}
	return len(fresh), nil
}

// contains refuses a ref whose commit does not have the judged commit in its history.
func (g *gate) contains(ctx context.Context, v qaverdict.Record, ref prepush.Ref) error {
	res, err := g.run(ctx, "merge-base", "--is-ancestor", v.Commit, ref.LocalID)
	if err != nil {
		return err
	}
	switch res.Code {
	case 0:
		return nil
	case 1:
		return refuse("qa-all passed at %s, which is not in what is pushed (%s)", v.Commit[:short], ref.LocalID[:short])
	default:
		return fmt.Errorf("git merge-base: exit %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
}

// sameBurst refuses a commit on top of the judged one that was made after the run ended.
func (g *gate) sameBurst(ctx context.Context, v qaverdict.Record, ref prepush.Ref) error {
	log, err := g.git(ctx, "log", "--format=%H %ct", v.Commit+".."+ref.LocalID)
	if err != nil {
		return err
	}
	for line := range strings.SplitSeq(strings.TrimSpace(log), "\n") {
		if line == "" {
			continue
		}
		id, stamp, ok := strings.Cut(line, " ")
		sec, perr := strconv.ParseInt(stamp, 10, 64)
		if !ok || perr != nil || len(id) < short {
			return fmt.Errorf("git log gave %q, not a commit and its time", line)
		}
		if sec > v.Finished.Unix() {
			return refuse("%s was committed after the qa-all run that passed at %s ended", id[:short], v.Commit[:short])
		}
	}
	return nil
}

// load reads the checkout's verdict once; none is a refusal, a damaged one an error.
func (g *gate) load(ctx context.Context) (qaverdict.Record, error) {
	if g.verdict != nil {
		return *g.verdict, nil
	}
	target, err := qaverdict.Locate(ctx, g.env.Runner, g.env.Cwd)
	if err != nil {
		return qaverdict.Record{}, err
	}
	v, err := qaverdict.Read(target.Path)
	if errors.Is(err, qaverdict.ErrNone) {
		return qaverdict.Record{}, refuse("no qa-all verdict: no qa-all run has finished in this checkout since the last one started")
	}
	if err != nil {
		return qaverdict.Record{}, err
	}
	g.verdict = &v
	return v, nil
}

func (g *gate) run(ctx context.Context, args ...string) (proc.Result, error) {
	res, err := g.env.Runner.Run(ctx, proc.Cmd{Argv: append([]string{"git", "-C", g.env.Cwd}, args...)})
	if err != nil {
		return proc.Result{}, fmt.Errorf("git %s: %w", args[0], err)
	}
	return res, nil
}

func (g *gate) git(ctx context.Context, args ...string) (string, error) {
	res, err := g.run(ctx, args...)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", fmt.Errorf("git %s: exit %d: %s", args[0], res.Code, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}
