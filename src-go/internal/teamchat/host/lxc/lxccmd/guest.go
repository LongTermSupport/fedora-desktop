package lxccmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/guest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// GuestWait bounds one guest op, the attach included.
const GuestWait = 60 * time.Second

// MaxGuestOutput caps a guest op's stdout and its stderr, each.
const MaxGuestOutput = 64 << 10

// setVar is lxc-attach's flag for one variable of the op's cleared environment.
const setVar = "--set-var"

// maxChangedLine is the longest `CHANGED` line a guest op may print (TB2.4).
const maxChangedLine = 256

// Attach is who a guest op runs as, on which seat, and with which kit (C 3.9.1). Key is the
// seat's, which a seat op (guest.Op.Keyed) is given as `--key`; guest check takes none.
type Attach struct {
	Container string
	User      paths.GuestUser
	Key       paths.SeatKey
	UID, GID  int
	Home      string
	// Kit is HostKitDir(digest), resolved once by the host: never current.
	Kit paths.Kit
}

// GuestResult is a guest op's output as TB2.4 reads it.
type GuestResult struct {
	Changed []guest.What
}

// GuestError is a guest op that failed with its error object as stderr's last line: its exit
// and the object's code. Undecodable is an object the op may not give: an exit outside 1, 64,
// 78 and (from an op that takes a seat lock) 75, or an object whose exit is not the process's;
// Code is then empty.
type GuestError struct {
	Op          string
	Container   string
	Exit        term.ExitCode
	Code        string
	Undecodable bool
}

func (e *GuestError) Error() string {
	if e.Undecodable {
		return fmt.Sprintf("%s in container %s: exit %d with an error object it may not give", e.Op, e.Container, e.Exit)
	}
	return fmt.Sprintf("%s in container %s: exit %d, %s", e.Op, e.Container, e.Exit, e.Code)
}

// AttachError is a guest op that ended with no error object: lxc-attach itself failed, could
// not start, or the op was killed before it wrote one (n3b-D1). Whether the container is still
// running is the caller's question to State.
type AttachError struct {
	Op        string
	Container string
	// Exit is the process's exit, -1 when a signal ended it or it never started.
	Exit   int
	Signal syscall.Signal
	// Err is why lxc-attach could not start, when it did not.
	Err error
}

func (e *AttachError) Error() string {
	switch {
	case e.Err != nil:
		return fmt.Sprintf("lxc-attach for %s in container %s: %v", e.Op, e.Container, e.Err)
	case e.Signal != 0:
		return fmt.Sprintf("lxc-attach for %s in container %s: ended by %v with no error object", e.Op, e.Container, e.Signal)
	}
	return fmt.Sprintf("lxc-attach for %s in container %s: exit %d with no error object", e.Op, e.Container, e.Exit)
}

func (e *AttachError) Unwrap() error { return e.Err }

// Guest runs `lxc-attach -n C -u UID -g GID --clear-env --set-var HOME=<home> --set-var
// USER=<u> --set-var LOGNAME=<u> --set-var PATH=/usr/local/bin:/usr/bin:/bin -- <kit>/bin/teamchat
// <op words> --user <u> [--key <key>] [args]` with stdin, the key for a seat op only, bounded by GuestWait, its stdout and stderr each
// capped at MaxGuestOutput. A uid or gid of 0, or the desktop's uid, is refused before anything
// runs (TB3). A token reaches the op only on stdin, and no error quotes stdin or a guest byte.
func (l LXC) Guest(ctx context.Context, a Attach, op guest.Op, args []string, stdin []byte) (GuestResult, error) {
	op, err := l.checkGuest(a, op, args)
	if err != nil {
		return GuestResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, GuestWait)
	defer cancel()
	user := a.User.String()
	words := append(strings.Fields(op.Verb), "--user", user)
	if op.Keyed {
		words = append(words, "--key", a.Key.String())
	}
	stdout, stderr := &capped{}, &capped{}
	_, err = l.Runner.Run(ctx, execx.Cmd{
		Name: "lxc-attach",
		Args: append([]string{
			"-n", a.Container, "-u", strconv.Itoa(a.UID), "-g", strconv.Itoa(a.GID), "--clear-env",
			setVar, "HOME=" + a.Home, setVar, "USER=" + user, setVar, "LOGNAME=" + user,
			setVar, "PATH=" + strings.Join(guest.SystemDirs, ":"),
			"--", a.Kit.Binary(),
		}, append(words, args...)...),
		Stdin:  bytes.NewReader(stdin),
		Stdout: stdout,
		Stderr: stderr,
	})
	if stdout.over || stderr.over {
		return GuestResult{}, fmt.Errorf("%w: %s in container %s: output over %d bytes", ErrOutput, op.Verb, a.Container, MaxGuestOutput)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return GuestResult{}, fmt.Errorf("%s in container %s: no answer within %s: %w", op.Verb, a.Container, GuestWait, ctxErr)
	}
	if err != nil {
		return GuestResult{}, failed(op, a.Container, stderr.Bytes(), err)
	}
	changed, err := parseChanged(op, stdout.Bytes())
	if err != nil {
		return GuestResult{}, fmt.Errorf("%w: %s in container %s: %s", ErrOutput, op.Verb, a.Container, err.Error())
	}
	return GuestResult{Changed: changed}, nil
}

// checkGuest refuses an attach TB3 forbids and an op that is not a guest op, and is the
// registered op of that verb, so the caller's copy of its set and Busy is not trusted.
func (l LXC) checkGuest(a Attach, op guest.Op, args []string) (guest.Op, error) {
	if err := checkContainer(a.Container); err != nil {
		return guest.Op{}, err
	}
	switch {
	case a.UID <= 0 || a.GID <= 0:
		return guest.Op{}, fmt.Errorf("%w: an attach as uid %d gid %d", ErrRefused, a.UID, a.GID)
	case a.User == paths.GuestUser{}:
		return guest.Op{}, fmt.Errorf("%w: an attach with no user", ErrRefused)
	case !paths.ValidGuestPath(a.Home):
		return guest.Op{}, fmt.Errorf("%w: the home is not a clean absolute path", ErrRefused)
	case a.Kit == paths.HostKitCurrent() || !strings.HasPrefix(a.Kit.String(), paths.HostKitRoot+"/"):
		return guest.Op{}, fmt.Errorf("%w: the kit is not one digest's directory in %s", ErrRefused, paths.HostKitRoot)
	}
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return guest.Op{}, fmt.Errorf("%w: an argument holds a NUL", ErrRefused)
		}
	}
	return registered(a, op)
}

// registered is the registered op of op's verb, refused when it is a seat op and the attach
// names no seat key.
func registered(a Attach, op guest.Op) (guest.Op, error) {
	for _, o := range guest.Ops() {
		if o.Verb != op.Verb {
			continue
		}
		if o.Keyed && a.Key == (paths.SeatKey{}) {
			return guest.Op{}, fmt.Errorf("%w: %s with no seat key", ErrRefused, o.Verb)
		}
		return o, nil
	}
	return guest.Op{}, fmt.Errorf("%w: %q is not a guest op", ErrRefused, op.Verb)
}

// failed is a guest op that did not exit 0: a GuestError when stderr's last line is an error
// object, else an AttachError.
func failed(op guest.Op, c string, stderr []byte, err error) error {
	ee, ok := errors.AsType[*execx.ExitError](err)
	if !ok {
		return &AttachError{Op: op.Verb, Container: c, Exit: -1, Err: err}
	}
	obj, decErr := exits.Decode(lastLine(stderr))
	if decErr != nil || ee.Signal != 0 {
		return &AttachError{Op: op.Verb, Container: c, Exit: ee.Code, Signal: ee.Signal}
	}
	ge := &GuestError{Op: op.Verb, Container: c, Exit: term.ExitCode(ee.Code)}
	if !mayExit(op, ge.Exit) || obj.Exit != ge.Exit {
		ge.Undecodable = true
		return ge
	}
	ge.Code = obj.Code
	return ge
}

func mayExit(op guest.Op, e term.ExitCode) bool {
	if e == exits.ExitBusy {
		return op.Busy
	}
	return e == exits.ExitInternal || e == exits.ExitUsage || e == exits.ExitConfig
}

func lastLine(b []byte) []byte {
	b = bytes.TrimSuffix(b, []byte("\n"))
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		return b[i+1:]
	}
	return b
}

// parseChanged is TB2.4's reading of a guest op's stdout: each line exactly
// `CHANGED<TAB><what>` with what in the op's set, each ended by a newline.
func parseChanged(op guest.Op, out []byte) ([]guest.What, error) {
	if len(out) == 0 {
		return nil, nil
	}
	if !bytes.HasSuffix(out, []byte("\n")) {
		return nil, errors.New("no newline at the end")
	}
	var changed []guest.What
	for line := range strings.SplitSeq(strings.TrimSuffix(string(out), "\n"), "\n") {
		if len(line) > maxChangedLine {
			return nil, errors.New("a line over its length")
		}
		what, ok := strings.CutPrefix(line, "CHANGED\t")
		if !ok || !slices.Contains(op.Changes, guest.What(what)) {
			return nil, errors.New("a line that is not a CHANGED line of its set")
		}
		changed = append(changed, guest.What(what))
	}
	return changed, nil
}

// capped keeps the first MaxGuestOutput bytes written to it and notes any beyond. It never
// fails a write, so the program is not cut off mid-output; the caller refuses an overflow.
type capped struct {
	buf  bytes.Buffer
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	room := MaxGuestOutput - c.buf.Len()
	if len(p) > room {
		c.over = true
		c.buf.Write(p[:max(room, 0)])
		return len(p), nil
	}
	c.buf.Write(p)
	return len(p), nil
}

func (c *capped) Bytes() []byte { return c.buf.Bytes() }
