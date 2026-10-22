package admin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// ErrChild is a child that broke the pipe's rules (CS4): a signal, an exit code other than its
// answer's, an answer that does not decode or is not the request's. The verbs report it as 1.
var ErrChild = errors.New("the " + paths.CtlUser + " child broke the pipe's rules")

// configError is a failure of the install's own setup (78).
type configError struct{ problem string }

func (e *configError) Error() string { return e.problem }

// maxChildStderr caps the child's stderr, which is only ever shown as an explanation.
const maxChildStderr = maxLine

// ChildWait bounds each child op to the play's wait for the team lock, so a child that never
// answers holds the lock for a bounded time. A verb that asks the child more than once, as team
// apply does, holds the lock across several of these, so a waiter can still time out.
const ChildWait = 10 * time.Minute

// ExecChild is the production Child (CS1-CS5): Self started as `ctl-child` under the
// teamchat-ctl account Lookup finds, through Runner.
type ExecChild struct {
	Runner execx.Runner
	Self   string
	Lookup func(name string) (User, bool, error)

	bound func(context.Context) (context.Context, context.CancelFunc) // a test's bound; nil is ChildWait
}

// Ask implements Child.
func (c ExecChild) Ask(ctx context.Context, req Request) (Answer, error) {
	bound := c.bound
	if bound == nil {
		bound = func(ctx context.Context) (context.Context, context.CancelFunc) {
			return context.WithTimeout(ctx, ChildWait)
		}
	}
	ctx, cancel := bound(ctx)
	defer cancel()
	spec, err := c.spec()
	if err != nil {
		return Answer{}, err
	}
	raw, err := EncodeRequest(req)
	if err != nil {
		return Answer{}, err
	}
	out, errOut := &capped{limit: maxAnswer}, &capped{limit: maxChildStderr}
	_, runErr := c.Runner.Run(ctx, execx.Cmd{
		Name: spec.Path, Args: spec.Args, Dir: spec.Dir, Env: spec.Env, CleanEnv: true,
		Credential: &syscall.Credential{Uid: spec.UID, Gid: spec.GID, Groups: spec.Groups},
		Stdin:      bytes.NewReader(raw), Stdout: out, Stderr: errOut,
	})
	if ctx.Err() != nil {
		return Answer{}, fmt.Errorf("the %s child: %w", paths.CtlUser, ctx.Err())
	}
	a, err := judge(req.Op, runErr, out)
	if err != nil {
		return Answer{}, broke(err, errOut)
	}
	return a, nil
}

// spec is the child's start, its failures the install's (78).
func (c ExecChild) spec() (ChildSpec, error) {
	u, found, err := c.Lookup(paths.CtlUser)
	if err != nil {
		return ChildSpec{}, &configError{fmt.Sprintf("cannot look up the %s account: %v", paths.CtlUser, err)}
	}
	if !found {
		return ChildSpec{}, &configError{"there is no " + paths.CtlUser + " account: the install is incomplete, run " + paths.PlayFile}
	}
	spec, err := NewChildSpec(c.Self, u)
	if err != nil {
		return ChildSpec{}, &configError{err.Error()}
	}
	return spec, nil
}

// judge is the answer when the exit and the answer agree (CS4); the exit code is never used.
func judge(op string, runErr error, out *capped) (Answer, error) {
	code := 0
	if runErr != nil {
		exit, ok := errors.AsType[*execx.ExitError](runErr)
		switch {
		case !ok:
			return Answer{}, fmt.Errorf("it could not start: %w", runErr)
		case exit.Signal != 0:
			return Answer{}, fmt.Errorf("it was killed by signal %d", exit.Signal)
		}
		code = exit.Code
	}
	if code != 0 && code != 1 {
		return Answer{}, fmt.Errorf("it exited %d", code)
	}
	if out.over {
		return Answer{}, fmt.Errorf("its answer is over %d bytes", maxAnswer)
	}
	line, ended := bytes.CutSuffix(out.buf.Bytes(), []byte("\n"))
	if !ended {
		return Answer{}, errors.New("its answer is not one line")
	}
	a, err := DecodeAnswer(line)
	if err != nil {
		return Answer{}, fmt.Errorf("its answer does not decode: %w", err)
	}
	if a.OK != (code == 0) {
		return Answer{}, fmt.Errorf("it exited %d with an answer whose ok is %v", code, a.OK)
	}
	if err := answerFits(op, a); err != nil {
		return Answer{}, err
	}
	return a, nil
}

// broke is ErrChild with what happened and the child's stderr, on one line.
func broke(err error, stderr *capped) error {
	msg := err.Error()
	if s := strings.TrimSpace(stderr.buf.String()); s != "" {
		msg += "; it said: " + s
	}
	return fmt.Errorf("%w: %s", ErrChild, oneLine(msg))
}

// capped holds at most limit bytes of what is written to it and drops the rest, so a child
// cannot make root hold more than the cap; over records that something was dropped.
type capped struct {
	limit int
	buf   bytes.Buffer
	over  bool
}

func (w *capped) Write(p []byte) (int, error) {
	room := w.limit - w.buf.Len()
	if len(p) > room {
		w.over = true
		w.buf.Write(p[:max(room, 0)])
		return len(p), nil
	}
	w.buf.Write(p)
	return len(p), nil
}

// SelfPath is the running binary (CS2): /proc/self/exe resolved, absolute and clean, so the
// child is the same build as root.
func SelfPath() (string, error) { return selfPath(os.Readlink) }

func selfPath(readlink func(string) (string, error)) (string, error) {
	p, err := readlink("/proc/self/exe")
	if err != nil {
		return "", fmt.Errorf("the running binary: %w", err)
	}
	if !filepath.IsAbs(p) || filepath.Clean(p) != p || strings.HasSuffix(p, " (deleted)") {
		return "", fmt.Errorf("the running binary %q is not an absolute, clean path to a file that exists", p)
	}
	return p, nil
}
