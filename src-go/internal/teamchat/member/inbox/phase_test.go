package inbox

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
)

// pipeEnd is net.Pipe's end as a stream: it has no buffer, so a write blocks until the far end
// reads, which is how a held session's full socket buffer looks to the link.
type pipeEnd struct{ net.Conn }

func (pipeEnd) CloseWrite() error { return nil }

func pipeDialer(clk clock.Clock) (Dialer, net.Conn) {
	near, far := net.Pipe()
	return &dialer{clk: clk, connect: func(context.Context, string, int) (stream, error) {
		return pipeEnd{near}, nil
	}}, far
}

var target = Target{Socket: "/run/user/1000/cc-socks/9.sock", Token: "pipe-token"}

func readAuth(t *testing.T, far net.Conn) *bufio.Reader {
	t.Helper()
	r := bufio.NewReader(far)
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != `{"type":"auth","token":"pipe-token"}`+"\n" {
		t.Fatalf("auth line %q", line)
	}
	return r
}

func expire(t *testing.T, clk *clock.Fake) {
	t.Helper()
	if err := clk.BlockUntil(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	clk.Advance(PhaseTimeout)
}

func TestAConnectThatOutlivesItsPhaseIsRetry(t *testing.T) {
	clk := clock.NewFake(0, 0)
	d := &dialer{clk: clk, connect: func(ctx context.Context, _ string, _ int) (stream, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	done := make(chan error, 1)
	go func() {
		_, err := d.Dial(t.Context(), target)
		done <- err
	}()
	expire(t, clk)
	err := <-done
	if !errors.Is(err, ErrRetry) || !strings.Contains(err.Error(), "5s") {
		t.Fatalf("got %v, want ErrRetry naming the phase's bound", err)
	}
}

func TestAnAuthLineNobodyReadsIsRetry(t *testing.T) {
	clk := clock.NewFake(0, 0)
	d, far := pipeDialer(clk)
	defer closeConn(t, far)
	done := make(chan error, 1)
	go func() {
		_, err := d.Dial(t.Context(), target)
		done <- err
	}()
	if err := expireUntil(t, clk, done); !errors.Is(err, ErrRetry) {
		t.Fatalf("got %v, want ErrRetry", err)
	}
}

// expireUntil moves clk past each phase bound armed until done answers. The connect phase arms
// a bound of its own just before the auth line's, so the bound one expire sees may be the
// connect's, already stopped, and the auth line's armed after the clock moved.
func expireUntil(t *testing.T, clk *clock.Fake, done <-chan error) error {
	t.Helper()
	for {
		ctx, cancel := context.WithCancel(t.Context())
		armed := make(chan error, 1)
		go func() { armed <- clk.BlockUntil(ctx, 1) }()
		select {
		case err := <-done:
			cancel()
			<-armed
			return err
		case err := <-armed:
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			clk.Advance(PhaseTimeout)
		}
	}
}

func TestAHeldSessionBeforeTheUserLineIsRetry(t *testing.T) {
	clk := clock.NewFake(0, 0)
	d, far := pipeDialer(clk)
	defer closeConn(t, far)
	c := dialPipe(t, d, far)
	done := make(chan error, 1)
	go func() { done <- c.WriteUser("never read") }()
	expire(t, clk)
	err := <-done
	if !errors.Is(err, ErrRetry) || errors.Is(err, ErrPartial) {
		t.Fatalf("got %v, want ErrRetry", err)
	}
	if strings.Contains(err.Error(), "pipe-token") {
		t.Fatalf("the error carries the token: %v", err)
	}
}

func TestAHeldSessionInsideTheUserLineIsPartial(t *testing.T) {
	clk := clock.NewFake(0, 0)
	d, far := pipeDialer(clk)
	defer closeConn(t, far)
	c := dialPipe(t, d, far)
	envelope := strings.Repeat("x", 4096)
	done := make(chan error, 1)
	go func() { done <- c.WriteUser(envelope) }()
	half := make([]byte, len(UserLine(envelope))/2)
	if _, err := io.ReadFull(far, half); err != nil {
		t.Fatal(err)
	}
	expire(t, clk)
	if err := <-done; !errors.Is(err, ErrPartial) || errors.Is(err, ErrRetry) {
		t.Fatalf("got %v, want ErrPartial", err)
	}
}

func TestASocketOfAnotherUserIsGone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "5.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := l.Close(); cerr != nil {
			t.Error(cerr)
		}
	}()
	d := &dialer{clk: clock.NewFake(0, 0), uid: os.Geteuid() + 1, connect: connectUnix}
	_, err = d.Dial(t.Context(), Target{Socket: path, Token: "x"})
	if !errors.Is(err, ErrGone) || !strings.Contains(err.Error(), "owned by uid") {
		t.Fatalf("got %v, want ErrGone for a socket another user owns", err)
	}
}

func TestOtherConnectFailuresAreRetry(t *testing.T) {
	for _, cause := range []error{unix.EAGAIN, unix.EACCES, errors.New("odd")} {
		if err := connectFailure(cause); !errors.Is(err, ErrRetry) {
			t.Fatalf("%v: got %v, want ErrRetry", cause, err)
		}
	}
	for _, cause := range []error{unix.ENOENT, unix.ECONNREFUSED, unix.ENOTSOCK} {
		if err := connectFailure(cause); !errors.Is(err, ErrGone) {
			t.Fatalf("%v: got %v, want ErrGone", cause, err)
		}
	}
}

// brokenEnd is a stream whose half-close and close fail.
type brokenEnd struct{ net.Conn }

func (brokenEnd) CloseWrite() error { return errors.New("half-close refused") }

func (b brokenEnd) Close() error {
	if err := b.Conn.Close(); err != nil {
		return err
	}
	return errors.New("close refused")
}

func TestAFailedHalfCloseAndCloseAreReported(t *testing.T) {
	clk := clock.NewFake(0, 0)
	near, far := net.Pipe()
	defer closeConn(t, far)
	d := &dialer{clk: clk, connect: func(context.Context, string, int) (stream, error) {
		return brokenEnd{near}, nil
	}}
	c := dialPipe(t, d, far)
	if err := c.Finish(); err == nil || !strings.Contains(err.Error(), "half-close refused") {
		t.Fatalf("finish: %v", err)
	}
	if err := c.Close(); err == nil || !strings.Contains(err.Error(), "close refused") {
		t.Fatalf("close: %v", err)
	}
}

func dialPipe(t *testing.T, d Dialer, far net.Conn) Conn {
	t.Helper()
	type dialed struct {
		c   Conn
		err error
	}
	done := make(chan dialed, 1)
	go func() {
		c, err := d.Dial(t.Context(), target)
		done <- dialed{c, err}
	}()
	readAuth(t, far)
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	t.Cleanup(func() {
		if err := got.c.Close(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			t.Error(err)
		}
	})
	return got.c
}

func closeConn(t *testing.T, c net.Conn) {
	t.Helper()
	if err := c.Close(); err != nil {
		t.Error(err)
	}
}
