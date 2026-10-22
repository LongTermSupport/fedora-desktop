package link

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const (
	// readLimit bounds the wait for a connection's request line, so a client that connects and
	// says nothing holds nothing.
	readLimit = 5 * time.Second
	// ackWait is how long handed items wait for the client's {"ack"} (B 2.4.4).
	ackWait = 10 * time.Second
	// maxAck bounds the ack line; one is 16 bytes.
	maxAck = 64
)

const (
	msgKit  exits.Template = "kit changed under the session"
	msgSize exits.Template = "the request is larger than 64 KiB"
)

// served is the engine as the socket server uses it.
type served interface {
	Serve(ctx context.Context, req wire.Request, out engine.RecordWriter) wire.Done
	Ack(ctx context.Context, ack string) error
	Return(ctx context.Context, ack string) error
}

// listen binds link.sock in run's directory, mode 0600, after unlinking a stale one: the
// caller holds link.lock, so no other link serves it (a socket is not a lock file).
func listen(root *homefs.Root, run paths.Run) (*net.UnixListener, error) {
	if err := root.Unlink(run.LinkSock()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("link: unlink a stale link.sock: %w", err)
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: run.LinkSock(), Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("link: bind link.sock: %w", err)
	}
	// The run directory is the member's own, mode 0700, so no one else reaches the socket
	// before its mode is set; SO_PEERCRED is checked on every connection besides.
	if err := os.Chmod(run.LinkSock(), 0o600); err != nil {
		return nil, errors.Join(fmt.Errorf("link: link.sock's mode: %w", err), ln.Close())
	}
	ln.SetUnlinkOnClose(false)
	return ln, nil
}

// server is link.sock's (LN2): one request per connection, answered by its records and done.
type server struct {
	ln    *net.UnixListener
	eng   served
	clock clock.Clock
	uid   int
	kit   paths.Digest
	log   io.Writer

	wg     sync.WaitGroup
	mu     sync.Mutex
	conns  map[*net.UnixConn]bool
	closed bool
	// open is how many connections are open, for status's runtime.
	open    atomic.Int64
	logMu   sync.Mutex
	closeMu sync.Once
	closeEr error
}

func newServer(ln *net.UnixListener, eng served, clk clock.Clock, uid int, kit paths.Digest, log io.Writer) *server {
	return &server{ln: ln, eng: eng, clock: clk, uid: uid, kit: kit, log: log, conns: map[*net.UnixConn]bool{}}
}

// run accepts until the listener closes; ctx is the engine's, which outlives the server.
func (s *server) run(ctx context.Context) {
	for {
		c, err := s.ln.AcceptUnix()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				s.logf("link: accept: %v", err)
			}
			return
		}
		if !s.track(c) {
			s.closeConn(c)
			return
		}
		s.wg.Go(func() {
			defer s.untrack(c)
			s.handle(ctx, c)
		})
	}
}

// close stops accepting, closes every connection (a hand-out waiting for its ack is returned)
// and waits for each to end. It is safe to call twice.
func (s *server) close() error {
	s.closeMu.Do(func() {
		s.mu.Lock()
		s.closed = true
		for c := range s.conns {
			s.closeConn(c)
		}
		s.mu.Unlock()
		s.closeEr = s.ln.Close()
		s.wg.Wait()
	})
	return s.closeEr
}

// connections is how many connections are open now.
func (s *server) connections() int64 { return s.open.Load() }

func (s *server) track(c *net.UnixConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[c] = true
	s.open.Add(1)
	return true
}

func (s *server) untrack(c *net.UnixConn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
	s.open.Add(-1)
	s.closeConn(c)
}

func (s *server) closeConn(c *net.UnixConn) {
	if err := c.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		s.logf("link: close a connection: %v", err)
	}
}

func (s *server) logf(format string, args ...any) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	// link.log is the log; a line it cannot take has nowhere else to go.
	if _, err := fmt.Fprintf(s.log, format+"\n", args...); err != nil {
		return
	}
}

// handle is one connection: another uid's is closed; a line over 64 KiB is 10 size, one that
// does not decode 10, another kit 78 runtime; one whose deadline has passed is closed unacted;
// otherwise its records and done, and for a hand-out the wait for its ack.
func (s *server) handle(base context.Context, c *net.UnixConn) {
	if uid, err := peerUID(c); err != nil || uid != s.uid {
		return
	}
	r := bufio.NewReaderSize(c, wire.MaxRequest+1)
	stop := s.clock.AfterFunc(readLimit, func() { s.closeConn(c) })
	line, err := r.ReadSlice('\n')
	stop()
	w := bufio.NewWriter(c)
	switch {
	case errors.Is(err, bufio.ErrBufferFull):
		s.refuse(c, w, wire.Done{Error: exits.New(exits.CodeSize, nil, msgSize)})
		return
	case err != nil:
		return
	}
	req, err := wire.DecodeRequest(line[:len(line)-1])
	if xe, ok := errors.AsType[*exits.Error](err); ok {
		s.refuse(c, w, wire.Done{Error: xe})
		return
	}
	if req.Kit != s.kit.String() {
		s.refuse(c, w, wire.Done{Error: exits.New(exits.CodeRuntime, nil, msgKit)})
		return
	}
	left := time.Duration(req.DeadlineMS)*time.Millisecond - s.clock.Boot()
	if left <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(base)
	stopDeadline := s.clock.AfterFunc(left, cancel)
	done := s.eng.Serve(ctx, req, recordWriter{w})
	stopDeadline()
	cancel()
	sent := s.answer(w, done)
	if done.Ack == "" {
		return
	}
	if sent {
		failpoint.Hit(failpoint.LinkWhileHanded)
	}
	s.settle(base, c, r, done.Ack, sent)
}

// refuse answers a refused request and reads what the client still sends, within readLimit:
// a socket closed with unread input resets, and the client would lose the answer.
func (s *server) refuse(c *net.UnixConn, w *bufio.Writer, done wire.Done) {
	if !s.answer(w, done) || c.CloseWrite() != nil {
		return
	}
	stop := s.clock.AfterFunc(readLimit, func() { s.closeConn(c) })
	defer stop()
	if _, err := io.Copy(io.Discard, io.LimitReader(c, 2*wire.MaxRequest)); err != nil {
		return
	}
}

// answer writes done, true when it reached the connection.
func (s *server) answer(w *bufio.Writer, done wire.Done) bool {
	if _, err := w.Write(append(done.Encode(), '\n')); err != nil {
		return false
	}
	return w.Flush() == nil
}

// settle waits ackWait for the client's {"ack"}: the same ack delivers the items, anything
// else (another ack, a close, the wait's end, a done that never reached it) returns them.
func (s *server) settle(ctx context.Context, c *net.UnixConn, r *bufio.Reader, ack string, sent bool) {
	if sent {
		stop := s.clock.AfterFunc(ackWait, func() { s.closeConn(c) })
		got, err := readAck(r)
		stop()
		if err == nil && got == ack {
			if err := s.eng.Ack(ctx, ack); err != nil {
				s.logf("link: deliver hand-out %s: %v", ack, err)
			}
			return
		}
	}
	if err := s.eng.Return(ctx, ack); err != nil {
		s.logf("link: return hand-out %s: %v", ack, err)
	}
}

func readAck(r *bufio.Reader) (string, error) {
	var line []byte
	for len(line) <= maxAck {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == '\n' {
			a, err := wire.DecodeAck(line)
			return a.Ack, err
		}
		line = append(line, b)
	}
	return "", wire.ErrMalformed
}

// peerUID is the connecting process's uid (SO_PEERCRED).
func peerUID(c *net.UnixConn) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if credErr != nil {
		return 0, credErr
	}
	return int(cred.Uid), nil
}

// recordWriter writes a request's records as their lines.
type recordWriter struct{ w *bufio.Writer }

func (r recordWriter) Record(rec records.Record) error {
	line, err := records.Encode(rec)
	if err != nil {
		return err
	}
	_, err = r.w.Write(append(line, '\n'))
	return err
}

func (r recordWriter) Flush() error { return r.w.Flush() }
