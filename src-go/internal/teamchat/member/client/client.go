// Package client is the member CLI's side of link.sock (Plan 00209 B 2.4.4, SPEC-n3 LN3) and
// the verbs that run outside a link: recv, wait, inbox, status, verify and history frame one
// request each, copy the link's record lines to stdout and its error to stderr, and exit with
// its done; `seat disconnected` (EN5) and `reach` (n3c-D16) talk to the homeserver themselves.
//
// When no link answers (ENOENT, ECONNREFUSED, or a close before done) the client reads
// keeper.json and probes keeper.lock and exits 69 at once: no-link while the link is restarting
// or not yet ready, link-failed when it failed or the keeper has gone, with the member kind's
// recovery (B 2.4.2). Only wait retries, under its one deadline.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	// waitDefaultS and waitMaxS are wait's --timeout in seconds (n3c-D7).
	waitDefaultS = 110
	waitMaxS     = 3600
	// historyDefault and historyMax are history's --limit (n3c-D8).
	historyDefault = 20
	historyMax     = 100
	historyRoom    = "main"
	// grace is how long past its deadline the client waits for a done before it takes the
	// link for gone: the link answers within the deadline, so only a hung link meets it.
	grace = 5 * time.Second
	// maxLine bounds one record line from the link.
	maxLine = 1 << 20
	// retryFirst and retryMax are wait's reconnect backoff.
	retryFirst = time.Second
	retryMax   = 5 * time.Second
)

const (
	msgNoSession exits.Template = "this is not a teamchat session: TEAMCHAT_RUNTIME is not set; tell a human"
	msgTimeout   exits.Template = "wait --timeout is 1 to 3600 seconds"
	msgLimit     exits.Template = "history --limit is 1 to 100"
	msgLink      exits.Template = "the link answered with a line that is not a record or a done; tell a human"
	msgInternal  exits.Template = "the client could not {}; tell a human"
	// msgWaitTimedOut is the engine's wording for the same end, reached with no link up.
	msgWaitTimedOut exits.Template = "nothing arrived before wait's deadline"
)

// deps are what a request waits on and connects through.
type deps struct {
	clock clock.Clock
	dial  func(path string) (*net.UnixConn, error)
}

func dialUnix(path string) (*net.UnixConn, error) {
	return net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
}

// Main is one of recv, wait, inbox, status, verify and history; cmd/teamchat puts it in
// cli.Env.Verbs under the verb's name.
func Main(verb string) cli.RunFunc {
	return func(e cli.Env, inv cli.Invocation) term.ExitCode {
		return run(e, inv, verb, deps{clock: clock.System(), dial: dialUnix})
	}
}

func run(e cli.Env, inv cli.Invocation, op string, d deps) term.ExitCode {
	req, wait, err := request(e, inv, op, d.clock)
	if err != nil {
		return exits.Write(e.Stderr, err)
	}
	r, err := Runtime(e.Getenv)
	if err != nil {
		return exits.Write(e.Stderr, err)
	}
	c := conn{deps: d, run: r, team: inv.Team, stdout: e.Stdout, stderr: e.Stderr}
	return c.serve(req, wait)
}

// request is op's request, its deadline from now on the boot clock, and wait's timeout.
func request(e cli.Env, inv cli.Invocation, op string, clk clock.Clock) (wire.Request, time.Duration, error) {
	var (
		args    any = wire.TeamsArgs{Team: inv.Team}
		timeout time.Duration
	)
	switch op {
	case wire.OpWait:
		s, err := count(inv, "timeout", waitDefaultS, waitMaxS, msgTimeout)
		if err != nil {
			return wire.Request{}, 0, err
		}
		timeout, args = time.Duration(s)*time.Second, wire.WaitArgs{Team: inv.Team, TimeoutS: s}
	case wire.OpVerify:
		args = wire.VerifyArgs{Team: inv.Team, ID: inv.Args[0]}
	case wire.OpHistory:
		limit, err := count(inv, "limit", historyDefault, historyMax, msgLimit)
		if err != nil {
			return wire.Request{}, 0, err
		}
		room, given := inv.Flag("room")
		if !given {
			room = historyRoom
		}
		args = wire.HistoryArgs{Team: inv.Team, Room: room, Limit: int(limit)}
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return wire.Request{}, 0, exits.New(exits.CodeInternal, inv.Team, msgInternal, exits.Words("frame its request"))
	}
	deadline := clk.Boot() + wire.Deadline(op, timeout)
	req := wire.Request{V: wire.Version, Op: op, Kit: e.Digest, DeadlineMS: deadline.Milliseconds(), Args: raw}
	if _, err := req.Encode(); err != nil {
		return wire.Request{}, 0, exits.New(exits.CodeInternal, inv.Team, msgInternal, exits.Words("frame its request"))
	}
	return req, timeout, nil
}

// count is a whole-number flag from 1 to most, def when it is not given.
func count(inv cli.Invocation, flag string, def, most int64, msg exits.Template) (int64, error) {
	s, given := inv.Flag(flag)
	if !given {
		return def, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 1 || n > most {
		return 0, exits.New(exits.CodeUsage, inv.Team, msg)
	}
	return n, nil
}

// Runtime is the run directory TEAMCHAT_RUNTIME names: a ccy session's, or the home member's
// under $HOME and $XDG_RUNTIME_DIR; anything else is 69 no-session.
func Runtime(getenv func(string) string) (paths.Run, error) {
	r, _, err := sessionOf(getenv(paths.EnvRuntime), getenv)
	return r, err
}

// sessionOf is Runtime's run directory for dir and the layout it is in.
func sessionOf(dir string, getenv func(string) string) (paths.Run, paths.Layout, error) {
	none := exits.New(exits.CodeNoSession, nil, msgNoSession)
	if dir == "" || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return paths.Run{}, paths.Layout{}, none
	}
	id, err := paths.ParseRunID(filepath.Base(dir))
	if err != nil {
		return paths.Run{}, paths.Layout{}, none
	}
	layouts := []paths.Layout{paths.CCYLayout()}
	if home, err := paths.HomeLayout(getenv("HOME"), getenv("XDG_RUNTIME_DIR")); err == nil {
		layouts = append(layouts, home)
	}
	for _, l := range layouts {
		if r := l.Run(id); r.Dir() == dir {
			return r, l, nil
		}
	}
	return paths.Run{}, paths.Layout{}, none
}

// conn is one verb's talk with the link.
type conn struct {
	deps

	run    paths.Run
	team   *paths.TeamID
	stdout io.Writer
	stderr io.Writer
}

// outcome is one connection's end: the exit, or gone when no link answered (refused, missing,
// or closed before its done); copied is whether a record reached stdout first.
type outcome struct {
	code   term.ExitCode
	copied bool
	gone   bool
}

// serve sends req until a link answers it; wait retries a missing link until its deadline, and
// is timed-out there (n3c-D7): a request sent past it would be discarded unanswered.
func (c conn) serve(req wire.Request, wait time.Duration) term.ExitCode {
	deadline := time.Duration(req.DeadlineMS) * time.Millisecond
	delay := retryFirst
	for {
		o := c.once(req, deadline)
		if !o.gone {
			return o.code
		}
		answer := c.noLink()
		if wait == 0 || o.copied || answer.Code == exits.CodeLinkFailed {
			return exits.Write(c.stderr, answer)
		}
		left := deadline - c.clock.Boot()
		if left > 0 {
			if err := c.clock.Sleep(context.Background(), min(delay, left)); err != nil {
				return exits.Write(c.stderr, err)
			}
			left = deadline - c.clock.Boot()
		}
		if left <= 0 {
			return exits.Write(c.stderr, exits.New(exits.CodeTimedOut, c.team, msgWaitTimedOut))
		}
		delay = min(2*delay, retryMax)
	}
}

// once is one connection: the request, the records copied, the done.
func (c conn) once(req wire.Request, deadline time.Duration) outcome {
	sock, err := c.dial(c.run.LinkSock())
	// EAGAIN is a link whose backlog is full: as good as gone for this try.
	if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EAGAIN) {
		return outcome{gone: true}
	}
	if err != nil {
		return outcome{code: c.internal("connect to the link")}
	}
	stop := c.clock.AfterFunc(deadline-c.clock.Boot()+grace, func() { closeSock(sock) })
	defer stop()
	defer closeSock(sock)
	line, err := req.Encode()
	if err == nil {
		_, err = sock.Write(append(line, '\n'))
	}
	if err != nil {
		return outcome{gone: true}
	}
	return c.copy(sock)
}

// copy copies record lines to stdout until the done; then flushes stdout and, for a hand-out,
// sends the ack, which a failed stdout never does.
func (c conn) copy(sock *net.UnixConn) outcome {
	r := bufio.NewReaderSize(sock, maxLine)
	through := &counter{w: c.stdout}
	out := bufio.NewWriterSize(through, maxLine)
	for {
		line, readErr := r.ReadSlice('\n')
		switch {
		case errors.Is(readErr, bufio.ErrBufferFull):
			return outcome{code: exits.Write(c.stderr, exits.New(exits.CodeInternal, c.team, msgLink)), copied: through.n > 0}
		case readErr != nil:
			// What is still buffered never reaches stdout: unacknowledged, the link returns it.
			return outcome{copied: through.n > 0, gone: true}
		case !bytes.HasPrefix(line, []byte(`{"done":`)):
			if _, err := out.Write(line); err != nil {
				return outcome{code: exits.Write(c.stderr, err), copied: true}
			}
			continue
		}
		done, err := wire.DecodeDone(line[:len(line)-1])
		if err != nil {
			return outcome{code: exits.Write(c.stderr, exits.New(exits.CodeInternal, c.team, msgLink)), copied: through.n > 0}
		}
		return outcome{code: c.finish(sock, out, done), copied: true}
	}
}

func (c conn) finish(sock *net.UnixConn, out *bufio.Writer, done wire.Done) term.ExitCode {
	if err := out.Flush(); err != nil {
		return exits.Write(c.stderr, err)
	}
	if done.Ack != "" {
		if _, err := sock.Write(append(wire.Ack{Ack: done.Ack}.Encode(), '\n')); err != nil {
			return c.internal("acknowledge the records")
		}
	}
	if done.Error != nil {
		return exits.Write(c.stderr, done.Error)
	}
	return term.ExitCode(done.Exit)
}

func (c conn) internal(what string) term.ExitCode {
	return exits.Write(c.stderr, exits.New(exits.CodeInternal, c.team, msgInternal, exits.Words(what)))
}

// closeSock closes the connection, which the deadline's timer may have closed already; a
// close's own failure leaves nothing to undo.
func closeSock(sock *net.UnixConn) {
	if err := sock.Close(); err != nil {
		return
	}
}

// counter counts what reaches the writer.
type counter struct {
	w io.Writer
	n int64
}

func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
