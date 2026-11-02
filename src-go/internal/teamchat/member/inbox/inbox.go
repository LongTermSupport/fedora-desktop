// Package inbox is the link's client of a session's inbox socket (Plan 00209 SPEC-n3 DV1, B
// 2.7.1): one connection per delivery, two compact ASCII JSON lines (the auth line, then the
// user line carrying the envelope), a half-close, and a read to EOF, each phase bounded by
// PhaseTimeout on the member's clock.
//
// Dial and WriteUser are apart because the caller persists its awaiting entry between them
// (B 2.7.3: nothing is persisted while the link only connects and authenticates). WriteUser's
// three outcomes are what the caller decides on: nil is a full write, which counts as sent
// whatever Finish then says (E11); ErrPartial is a user line cut inside, whose fate only the
// transcript can settle; ErrRetry is nothing of it written. The token is a secret (B11): no
// error, String, GoString, format verb or JSON form of this package carries it.
package inbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// PhaseTimeout bounds each phase: connect, the auth line, the user line, and the finish.
const PhaseTimeout = 5 * time.Second

var (
	// ErrGone is ENOENT, ECONNREFUSED or ENOTSOCK at connect, or a socket another user owns:
	// delivery stops until the session reports a new socket (the keeper sees the session end).
	ErrGone = errors.New("inbox: the session's socket is gone")
	// ErrRetry is any other connect or auth failure, or a phase that ran out before the user
	// line began: nothing reached the session.
	ErrRetry = errors.New("inbox: no connection, retry later")
	// ErrPartial is a user line that began and did not finish.
	ErrPartial = errors.New("inbox: the user line was cut")

	errTimeout = fmt.Errorf("the %s phase ran out", PhaseTimeout)
	errUsed    = errors.New("inbox: the user line was already written on this connection")
)

const redacted = "[redacted]"

// Target is one session's socket and its token.
type Target struct {
	Socket string
	Token  string
}

func (t Target) String() string { return "inbox " + t.Socket + " token " + redacted }

// GoString keeps %#v from printing the token.
func (t Target) GoString() string {
	return "inbox.Target{Socket:" + strconv.Quote(t.Socket) + ", Token:" + redacted + "}"
}

// Format gives every verb the redacted form, so %d or %x cannot print the token either.
func (t Target) Format(f fmt.State, verb rune) {
	text := t.String()
	if verb == 'v' && f.Flag('#') {
		text = t.GoString()
	}
	if _, err := io.WriteString(f, text); err != nil {
		return
	}
}

// MarshalJSON keeps a structured log from carrying the token.
func (t Target) MarshalJSON() ([]byte, error) {
	return []byte(`{"socket":` + proto.EscapeASCII(t.Socket) + `,"token":"` + redacted + `"}`), nil
}

// Dialer connects to a session's socket and writes the auth line.
type Dialer interface {
	Dial(ctx context.Context, t Target) (Conn, error)
}

// Conn is one delivery's connection, after the auth line.
type Conn interface {
	// WriteUser writes the user line once: nil only for a full write; ErrPartial; ErrRetry
	// when nothing was written.
	WriteUser(envelope string) error
	// Finish half-closes and reads to EOF; its error never undoes a full write.
	Finish() error
	Close() error
}

// stream is a connected socket: *net.UnixConn, or a test's pipe.
type stream interface {
	io.ReadWriteCloser
	CloseWrite() error
}

type dialer struct {
	clk     clock.Clock
	uid     int
	connect func(ctx context.Context, path string, uid int) (stream, error)
}

// NewDialer is the Dialer every link uses: each phase bounded by clk.AfterFunc(PhaseTimeout).
func NewDialer(clk clock.Clock) Dialer {
	return &dialer{clk: clk, uid: unix.Geteuid(), connect: connectUnix}
}

// UserLine is the compact ASCII user line with its newline. Its content takes exactly
// proto.WireBytes(envelope) bytes, the measure the 12 KiB cap is taken in.
func UserLine(envelope string) []byte {
	return []byte(`{"type":"user","message":{"role":"user","content":` + proto.EscapeASCII(envelope) + "}}\n")
}

func authLine(token string) []byte {
	return []byte(`{"type":"auth","token":` + proto.EscapeASCII(token) + "}\n")
}

func (d *dialer) Dial(ctx context.Context, t Target) (Conn, error) {
	if !filepath.IsAbs(t.Socket) || t.Token == "" {
		return nil, fmt.Errorf("inbox: %s: a target needs an absolute socket path and a token", t)
	}
	connectCtx, cancel := context.WithCancel(ctx)
	stop := d.clk.AfterFunc(PhaseTimeout, cancel)
	s, err := d.connect(connectCtx, t.Socket, d.uid)
	timedOut := !stop()
	cancel()
	if err != nil {
		if timedOut {
			err = errTimeout
		}
		return nil, fmt.Errorf("inbox: connect %s: %w", t.Socket, connectFailure(err))
	}
	c := &conn{clk: d.clk, s: s, socket: t.Socket}
	if _, err := c.phase(authLine(t.Token)); err != nil {
		return nil, errors.Join(fmt.Errorf("%w: auth line to %s: %w", ErrRetry, t.Socket, err), c.Close())
	}
	return c, nil
}

// connectFailure sorts a connect error: the session's socket gone, or worth a retry.
func connectFailure(err error) error {
	if errors.Is(err, ErrGone) || errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ECONNREFUSED) || errors.Is(err, unix.ENOTSOCK) {
		return fmt.Errorf("%w: %w", ErrGone, err)
	}
	return fmt.Errorf("%w: %w", ErrRetry, err)
}

// connectUnix opens the path TB1's way, without following a symlink, and connects only to a
// socket the link's own user owns, through the descriptor it checked.
func connectUnix(ctx context.Context, path string, uid int) (stream, error) {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	s, err := connectChecked(ctx, fd, uid)
	if cerr := unix.Close(fd); cerr != nil {
		err = errors.Join(err, fmt.Errorf("close the checked descriptor: %w", cerr))
		if s != nil {
			err = errors.Join(err, s.Close())
		}
		return nil, err
	}
	return s, err
}

func connectChecked(ctx context.Context, fd, uid int) (stream, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, fmt.Errorf("fstat: %w", err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFSOCK {
		return nil, fmt.Errorf("not a socket: %w", unix.ENOTSOCK)
	}
	if int(st.Uid) != uid {
		return nil, fmt.Errorf("%w: the socket is owned by uid %d, not %d", ErrGone, st.Uid, uid)
	}
	var nd net.Dialer
	c, err := nd.DialContext(ctx, "unix", "/proc/self/fd/"+strconv.Itoa(fd))
	if err != nil {
		return nil, err
	}
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return nil, errors.Join(fmt.Errorf("a %T, not a unix connection", c), c.Close())
	}
	return uc, nil
}

type conn struct {
	clk    clock.Clock
	s      stream
	socket string

	used      bool
	closeOnce sync.Once
	closeErr  error
	closed    bool
	mu        sync.Mutex
}

func (c *conn) WriteUser(envelope string) error {
	if c.used {
		return errUsed
	}
	c.used = true
	line := UserLine(envelope)
	n, err := c.phase(line)
	switch {
	case err == nil: // io.Writer: a nil error is the whole line
		return nil
	case n == 0:
		return fmt.Errorf("%w: user line to %s: %w", ErrRetry, c.socket, err)
	default:
		return fmt.Errorf("%w: user line to %s: %d of %d bytes: %w", ErrPartial, c.socket, n, len(line), err)
	}
}

func (c *conn) Finish() error {
	if _, err := c.phase(nil); err != nil {
		return fmt.Errorf("inbox: finish %s: %w", c.socket, err)
	}
	return nil
}

// Close closes the connection; a second Close, or one after a phase ran out, is nil.
func (c *conn) Close() error {
	already := c.isClosed()
	c.shut()
	if already {
		return nil
	}
	if c.closeErr != nil {
		return fmt.Errorf("inbox: close %s: %w", c.socket, c.closeErr)
	}
	return nil
}

// phase writes line (or, for nil, half-closes and reads to EOF) with the stream closed at
// PhaseTimeout; it reports the bytes written and the cause of a failure.
func (c *conn) phase(line []byte) (int, error) {
	if c.isClosed() {
		return 0, net.ErrClosed
	}
	stop := c.clk.AfterFunc(PhaseTimeout, c.shut)
	var n int
	var err error
	if line != nil {
		n, err = c.s.Write(line)
	} else {
		err = c.finish()
	}
	if !stop() && err != nil {
		err = errTimeout
	}
	return n, err
}

func (c *conn) finish() error {
	if err := c.s.CloseWrite(); err != nil {
		return fmt.Errorf("half-close: %w", err)
	}
	if _, err := io.Copy(io.Discard, c.s); err != nil {
		return fmt.Errorf("read to EOF: %w", err)
	}
	return nil
}

func (c *conn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// shut closes the stream once; the phase timer and Close both reach it.
func (c *conn) shut() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		c.closeErr = c.s.Close()
	})
}
