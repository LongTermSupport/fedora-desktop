package link

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const kitHex = "0123456789abcdef"

// fakeServed is the engine as the server sees it: it answers each request with the records
// and done the test set, and records the acks and returns.
type fakeServed struct {
	mu      sync.Mutex
	recs    []records.Record
	done    wire.Done
	served  []wire.Request
	acked   []string
	back    []string
	settled chan struct{}
	block   bool
	// cancelled, if set, runs when a blocked request's context ends.
	cancelled func()
}

func (f *fakeServed) Serve(ctx context.Context, req wire.Request, out engine.RecordWriter) wire.Done {
	f.mu.Lock()
	f.served = append(f.served, req)
	recs, done, block, cancelled := f.recs, f.done, f.block, f.cancelled
	f.mu.Unlock()
	if block {
		<-ctx.Done()
		if cancelled != nil {
			cancelled()
		}
		return wire.Done{}
	}
	for _, r := range recs {
		if err := out.Record(r); err != nil {
			return wire.Done{Exit: 1}
		}
	}
	if err := out.Flush(); err != nil {
		return wire.Done{Exit: 1}
	}
	return done
}

func (f *fakeServed) Ack(_ context.Context, ack string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acked = append(f.acked, ack)
	close(f.settled)
	return nil
}

func (f *fakeServed) Return(_ context.Context, ack string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.back = append(f.back, ack)
	close(f.settled)
	return errors.New("the return could not be written")
}

func (f *fakeServed) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.served)
}

type serverWorld struct {
	*home

	eng  *fakeServed
	srv  *server
	sock string
}

func newServerWorld(t *testing.T) *serverWorld {
	t.Helper()
	return newServerWorldAs(t, os.Getuid())
}

// newServerWorldAs is a server that serves uid alone.
func newServerWorldAs(t *testing.T, uid int) *serverWorld {
	t.Helper()
	w := &serverWorld{home: newHome(t), eng: &fakeServed{settled: make(chan struct{})}}
	kit, err := paths.ParseDigest(kitHex)
	check(t, err)
	w.sock = w.layout.Run(w.run).LinkSock()
	check(t, w.root.Create(w.sock, []byte("stale"), 0o600))
	ln, err := listen(w.root, w.layout.Run(w.run))
	check(t, err)
	info, err := os.Lstat(w.sock)
	check(t, err)
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("link.sock is %v", info.Mode())
	}
	w.srv = newServer(ln, w.eng, w.clk, uid, kit, w.log)
	go w.srv.run(context.Background())
	t.Cleanup(func() { check(t, w.srv.close()) })
	return w
}

func (w *serverWorld) dial(t *testing.T) *net.UnixConn {
	t.Helper()
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: w.sock, Net: "unix"})
	check(t, err)
	t.Cleanup(func() {
		if err := c.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Error(err)
		}
	})
	return c
}

// request is a request line for op, due deadline from the fake's boot reading.
func (w *serverWorld) request(t *testing.T, op string, deadline time.Duration) []byte {
	t.Helper()
	line, err := wire.Request{
		V: 1, Op: op, Kit: kitHex, DeadlineMS: (w.clk.Boot() + deadline).Milliseconds(),
		Args: json.RawMessage(`{"team":null}`),
	}.Encode()
	check(t, err)
	return append(line, '\n')
}

// exchange sends line and reads every line until the server closes.
func exchange(t *testing.T, c *net.UnixConn, line []byte) []string {
	t.Helper()
	_, err := c.Write(line)
	check(t, err)
	return readAll(t, c)
}

func readAll(t *testing.T, c *net.UnixConn) []string {
	t.Helper()
	data, err := io.ReadAll(c)
	check(t, err)
	if len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func doneOf(t *testing.T, line string) wire.Done {
	t.Helper()
	d, err := wire.DecodeDone([]byte(line))
	check(t, err)
	return d
}

func TestTheServerAnswersRecordsThenDone(t *testing.T) {
	w := newServerWorld(t)
	w.eng.recs = []records.Record{records.More{Pending: 3}}
	lines := exchange(t, w.dial(t), w.request(t, wire.OpStatus, 2*time.Second))
	if len(lines) != 2 || !strings.Contains(lines[0], `"kind":"more"`) || doneOf(t, lines[1]).Exit != 0 {
		t.Fatalf("lines %q", lines)
	}
}

func TestTheServerRefusesWhatIsNotARequest(t *testing.T) {
	w := newServerWorld(t)
	for name, c := range map[string]struct {
		line []byte
		code string
	}{
		"oversized": {append([]byte(strings.Repeat("x", wire.MaxRequest+1)), '\n'), exits.CodeSize},
		"no ending": {[]byte(strings.Repeat("x", wire.MaxRequest+1)), exits.CodeSize},
		"not json":  {[]byte("{\n"), exits.CodeSchema},
		"other kit": {[]byte(strings.Replace(string(w.request(t, wire.OpStatus, time.Second)), kitHex, "fedcba9876543210", 1)), exits.CodeRuntime},
	} {
		lines := exchange(t, w.dial(t), c.line)
		if len(lines) != 1 {
			t.Fatalf("%s: lines %q", name, lines)
		}
		if d := doneOf(t, lines[0]); d.Error == nil || d.Error.Code != c.code {
			t.Fatalf("%s: done %+v", name, d)
		}
	}
	if w.eng.requests() != 0 {
		t.Fatal("a refused request was served")
	}
}

func TestAPastDeadlineIsDiscardedUnacted(t *testing.T) {
	w := newServerWorld(t)
	if lines := exchange(t, w.dial(t), w.request(t, wire.OpStatus, -time.Millisecond)); lines != nil {
		t.Fatalf("lines %q", lines)
	}
	if lines := exchange(t, w.dial(t), w.request(t, wire.OpStatus, 0)); lines != nil {
		t.Fatalf("lines %q", lines)
	}
	if w.eng.requests() != 0 {
		t.Fatal("a late request was served")
	}
}

func TestTheDeadlineEndsTheRequest(t *testing.T) {
	w := newServerWorld(t)
	w.eng.block = true
	c := w.dial(t)
	_, err := c.Write(w.request(t, wire.OpWait, 5*time.Second))
	check(t, err)
	until(t, func() bool { return w.eng.requests() == 1 })
	check(t, w.clk.BlockUntil(t.Context(), 1))
	w.clk.Advance(5 * time.Second)
	if lines := readAll(t, c); len(lines) != 1 || doneOf(t, lines[0]).Exit != 0 {
		t.Fatalf("lines %q", lines)
	}
}

func TestCloseEndsABlockedWaitAtOnce(t *testing.T) {
	w := newServerWorld(t)
	w.eng.block = true
	c := w.dial(t)
	_, err := c.Write(w.request(t, wire.OpWait, time.Hour))
	check(t, err)
	until(t, func() bool { return w.eng.requests() == 1 })
	closed := make(chan error, 1)
	go func() { closed <- w.srv.close() }()
	select {
	case err := <-closed:
		check(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("close waited on a blocked wait")
	}
}

// N3: close cancels a request only once its connection is closed, so the client sees the link
// gone (a wait retries across the restart), never the cancel's answer (exit 4 before its
// deadline).
func TestCloseClosesTheConnectionBeforeTheCancel(t *testing.T) {
	w := newServerWorld(t)
	cancelled := make(chan struct{})
	w.eng.block = true
	w.eng.cancelled = func() { close(cancelled) }
	c := w.dial(t)
	_, err := c.Write(w.request(t, wire.OpWait, time.Hour))
	check(t, err)
	until(t, func() bool { return w.eng.requests() == 1 })
	// Holding the server's lock holds close before it closes the connections: no cancel may
	// come while it is held.
	w.srv.mu.Lock()
	closed := make(chan error, 1)
	go func() { closed <- w.srv.close() }()
	select {
	case <-cancelled:
		w.srv.mu.Unlock()
		t.Fatal("the request was cancelled before its connection was closed")
	case <-time.After(100 * time.Millisecond):
	}
	w.srv.mu.Unlock()
	check(t, <-closed)
	if lines := readAll(t, c); lines != nil {
		t.Fatalf("the client was answered %q", lines)
	}
}

func TestASilentClientIsClosed(t *testing.T) {
	w := newServerWorld(t)
	c := w.dial(t)
	check(t, w.clk.BlockUntil(t.Context(), 1))
	w.clk.Advance(readLimit)
	if lines := readAll(t, c); lines != nil {
		t.Fatalf("lines %q", lines)
	}
	half := w.dial(t)
	_, err := half.Write([]byte(`{"v":1`))
	check(t, err)
	check(t, half.CloseWrite())
	if lines := readAll(t, half); lines != nil {
		t.Fatalf("lines %q", lines)
	}
}

func TestAnotherUIDIsClosed(t *testing.T) {
	w := newServerWorldAs(t, os.Getuid()+1)
	if lines := readAll(t, w.dial(t)); lines != nil {
		t.Fatalf("lines %q", lines)
	}
}

// handOut is a recv that handed items, answered with an ack.
func (w *serverWorld) handOut(t *testing.T) *net.UnixConn {
	t.Helper()
	w.eng.done = wire.Done{Ack: "0badcafe"}
	c := w.dial(t)
	_, err := c.Write(w.request(t, wire.OpRecv, 2*time.Second))
	check(t, err)
	line, err := bufio.NewReader(c).ReadString('\n')
	check(t, err)
	if d := doneOf(t, strings.TrimSuffix(line, "\n")); d.Ack != "0badcafe" {
		t.Fatalf("done %+v", d)
	}
	return c
}

// settled waits for the hand-out's ack or return, and is how many of each there were.
func (w *serverWorld) settled(t *testing.T) (int, int) {
	t.Helper()
	select {
	case <-w.eng.settled:
	case <-t.Context().Done():
		t.Fatal("never settled")
	}
	w.eng.mu.Lock()
	defer w.eng.mu.Unlock()
	return len(w.eng.acked), len(w.eng.back)
}

func TestAnAckDelivers(t *testing.T) {
	w := newServerWorld(t)
	c := w.handOut(t)
	_, err := c.Write(append(wire.Ack{Ack: "0badcafe"}.Encode(), '\n'))
	check(t, err)
	if acked, back := w.settled(t); acked != 1 || back != 0 {
		t.Fatalf("acked %d back %d", acked, back)
	}
}

func TestAWrongAckReturns(t *testing.T) {
	w := newServerWorld(t)
	c := w.handOut(t)
	_, err := c.Write(append(wire.Ack{Ack: "00000000"}.Encode(), '\n'))
	check(t, err)
	if acked, back := w.settled(t); acked != 0 || back != 1 {
		t.Fatalf("acked %d back %d", acked, back)
	}
	check(t, w.srv.close())
	if !strings.Contains(w.log.String(), "return") {
		t.Fatalf("log %q", w.log.String())
	}
}

func TestAClosedConnectionReturns(t *testing.T) {
	w := newServerWorld(t)
	check(t, w.handOut(t).Close())
	if acked, back := w.settled(t); acked != 0 || back != 1 {
		t.Fatalf("acked %d back %d", acked, back)
	}
}

func TestALateAckReturns(t *testing.T) {
	w := newServerWorld(t)
	c := w.handOut(t)
	check(t, w.clk.BlockUntil(t.Context(), 1))
	w.clk.Advance(ackWait)
	if acked, back := w.settled(t); acked != 0 || back != 1 {
		t.Fatalf("acked %d back %d", acked, back)
	}
	// The ack that comes after the 10 s delivers nothing.
	if _, err := c.Write(append(wire.Ack{Ack: "0badcafe"}.Encode(), '\n')); err == nil {
		if lines := readAll(t, c); lines != nil {
			t.Fatalf("lines %q", lines)
		}
	}
	check(t, w.srv.close())
	if acked, back := w.settled(t); acked != 0 || back != 1 {
		t.Fatalf("acked %d back %d after the late ack", acked, back)
	}
}

func TestListenRefusesARunWithNoDirectory(t *testing.T) {
	h := newHome(t)
	other, err := paths.ParseRunID("fedcba9876543210")
	check(t, err)
	if _, err := listen(h.root, h.layout.Run(other)); err == nil {
		t.Fatal("a socket was bound in no directory")
	}
}
