package tlsterm

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/leakcheck"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/testconn"
)

// lockedLog is a log the terminator's goroutines write while the test reads it.
type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedLog) lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Split(strings.TrimSpace(l.buf.String()), "\n")
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// closeClosed closes a client the terminator has closed already: the close's alert may find the
// connection gone, which is no failure of the test's.
func closeClosed(t *testing.T, c net.Conn) {
	t.Helper()
	if err := c.Close(); err != nil {
		t.Logf("closing a connection the terminator closed: %v", err)
	}
}

// clientFrom is client from the local address from, a second source on loopback.
func clientFrom(t *testing.T, addr netip.AddrPort, from string, ca []byte) (*tls.Conn, error) {
	t.Helper()
	pool, err := pki.Pool(ca)
	check(t, err)
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(from)}},
		Config: &tls.Config{
			RootCAs: pool, ServerName: addr.Addr().String(), MinVersion: tls.VersionTLS13,
			Time: func() time.Time { return fixedNow },
		},
	}
	conn, err := dialer.DialContext(t.Context(), "tcp", addr.String())
	if err != nil {
		return nil, err
	}
	tc, ok := conn.(*tls.Conn)
	if !ok {
		t.Fatalf("a TLS dial gave a %T", conn)
	}
	return tc, nil
}

// answered is the status and body of the HTTP answer the terminator wrote to c, and whether it
// then closed it.
func answered(t *testing.T, c net.Conn) (int, string, bool) {
	t.Helper()
	testconn.Bound(t, c)
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	check(t, err)
	body, err := io.ReadAll(resp.Body)
	check(t, err)
	check(t, resp.Body.Close())
	return resp.StatusCode, string(body), closed(t, c)
}

// T9 (Plan 00257 SEC-4): one source holds at most MaxPerSource connections, so it cannot fill
// every slot; another source is still served, and the refusal is logged. The source's held
// connection answers the liveness check, so the new one is refused once ProbeLimit is up.
func TestT9OneSourceCannotHoldEverySlot(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	log := &lockedLog{}
	clk := clock.NewFake(0, fixedNow.UnixMilli())
	probed := make(chan int, 4)
	srv := serve(t, k, up.addr, clk, func(c *Config) {
		c.MaxConns, c.MaxPerSource, c.Log = 4, 1, log
		c.Probe = func(conns []net.Conn) { probed <- len(conns) }
	})
	addr := srv.Addrs()[0]
	first, err := client(t, addr, k.ca, nil)
	check(t, err)
	t.Cleanup(func() { check(t, first.Close()) })
	echo(t, first, "held")
	// The second's handshake waits on the check, so it is dialled beside the test.
	type dialed struct {
		conn *tls.Conn
		err  error
	}
	got := make(chan dialed, 1)
	go func() {
		c, derr := client(t, addr, k.ca, nil)
		got <- dialed{c, derr}
	}()
	if n := <-probed; n != 1 {
		t.Fatalf("the liveness check was given %d connections, want the source's 1", n)
	}
	check(t, clk.AwaitDueFrom(t.Context(), ProbeLimit, "(*Server).reclaim"))
	clk.Advance(ProbeLimit)
	if second := <-got; second.err == nil {
		if !closed(t, second.conn) {
			t.Fatal("a second connection from the same source was served beyond MaxPerSource")
		}
		closeClosed(t, second.conn)
	}
	other, err := clientFrom(t, addr, "127.0.0.2", k.ca)
	check(t, err)
	t.Cleanup(func() { check(t, other.Close()) })
	echo(t, other, "another source")
	leakcheck.Poll(t, func() bool { return strings.Contains(log.String(), "from 127.0.0.1") })
	if !strings.Contains(log.String(), "1 connections are held from it") {
		t.Errorf("the refusal's log: %q", log.String())
	}
}

// T9 at the defaults (the coordinator's ruling on the B.5 fixes): with nothing configured, one
// source holds DefaultMaxPerSource connections and no more, and those leave slots for another
// source, which is still served.
func TestT9AtTheDefaultsOneSourceLeavesSlotsForOthers(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	clk := clock.NewFake(0, fixedNow.UnixMilli())
	probed := make(chan int, 1)
	srv := serve(t, k, up.addr, clk, func(c *Config) {
		c.Probe = func(conns []net.Conn) { probed <- len(conns) }
	})
	if srv.cfg.maxPerSource() >= srv.cfg.maxConns() {
		t.Fatalf("by default one source holds %d of %d slots: all of them", srv.cfg.maxPerSource(), srv.cfg.maxConns())
	}
	addr := srv.Addrs()[0]
	for i := range DefaultMaxPerSource {
		c, err := client(t, addr, k.ca, nil)
		check(t, err)
		t.Cleanup(func() { check(t, c.Close()) })
		echo(t, c, "held "+strconv.Itoa(i))
	}
	type dialed struct {
		conn *tls.Conn
		err  error
	}
	got := make(chan dialed, 1)
	go func() {
		c, derr := client(t, addr, k.ca, nil)
		got <- dialed{c, derr}
	}()
	if n := <-probed; n != DefaultMaxPerSource {
		t.Fatalf("the liveness check was given %d connections, want the source's %d", n, DefaultMaxPerSource)
	}
	check(t, clk.AwaitDueFrom(t.Context(), ProbeLimit, "(*Server).reclaim"))
	clk.Advance(ProbeLimit)
	if over := <-got; over.err == nil {
		if !closed(t, over.conn) {
			t.Fatal("a connection past the default per-source cap was served")
		}
		closeClosed(t, over.conn)
	}
	other, err := clientFrom(t, addr, "127.0.0.2", k.ca)
	check(t, err)
	t.Cleanup(func() { check(t, other.Close()) })
	echo(t, other, "another source")
}

// T9a (Plan 00257 B.5 review): several seats behind one address (a NAT) reconnect after their
// host was suspended. Their old connections are dead but not yet timed out; the liveness check
// finds them dead, they are reclaimed, and every new connection is admitted.
func TestT9aSeatsBehindOneAddressReconnectAfterASuspend(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	const seats = 3
	var once sync.Once
	srv := serve(t, k, up.addr, clock.NewFake(0, fixedNow.UnixMilli()), func(c *Config) {
		c.MaxPerSource = seats
		// The suspended host answers no probe: the first check finds every connection it is
		// given dead, and closes each, as a failed keepalive does.
		c.Probe = func(conns []net.Conn) {
			once.Do(func() {
				for _, conn := range conns {
					if err := conn.Close(); err != nil {
						t.Errorf("closing a dead connection: %v", err)
					}
				}
			})
		}
	})
	addr := srv.Addrs()[0]
	old := make([]net.Conn, 0, seats)
	for i := range seats {
		c, err := client(t, addr, k.ca, nil)
		check(t, err)
		echo(t, c, "before the suspend "+strconv.Itoa(i))
		old = append(old, c)
	}
	for i := range seats {
		c, err := client(t, addr, k.ca, nil)
		check(t, err)
		t.Cleanup(func() { check(t, c.Close()) })
		echo(t, c, "after the resume "+strconv.Itoa(i))
	}
	for _, c := range old {
		if !closed(t, c) {
			t.Fatal("a dead connection was not reclaimed")
		}
		closeClosed(t, c)
	}
}

// T9a: the production liveness check is a short keepalive, which fails a dead connection
// within ProbeLimit; the check leaves a live connection serving, its keepalive given back.
func TestT9aTheLivenessCheckIsAShortKeepalive(t *testing.T) {
	failsAfter := probeKeepAlive.Idle + probeKeepAlive.Interval*time.Duration(probeKeepAlive.Count)
	if failsAfter >= ProbeLimit {
		t.Fatalf("a dead connection fails after %v of probes, not within ProbeLimit %v", failsAfter, ProbeLimit)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	check(t, err)
	t.Cleanup(func() { check(t, ln.Close()) })
	testconn.Bound(t, ln)
	dialed, err := net.Dial("tcp", ln.Addr().String())
	check(t, err)
	t.Cleanup(func() { check(t, dialed.Close()) })
	accepted, err := ln.Accept()
	check(t, err)
	t.Cleanup(func() { check(t, accepted.Close()) })
	s := &Server{cfg: Config{Clock: clock.NewFake(0, 0)}}
	s.keepaliveProbe([]net.Conn{accepted})
	s.restoreKeepalive([]net.Conn{accepted})
	got := make([]byte, 1)
	_, err = dialed.Write([]byte("x"))
	check(t, err)
	testconn.Bound(t, accepted)
	_, err = io.ReadFull(accepted, got)
	check(t, err)
}

// T9: the slots full is a refusal logged too, never a silent close.
func TestT9FullSlotsAreLogged(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	log := &lockedLog{}
	srv := serve(t, k, up.addr, clock.NewFake(0, fixedNow.UnixMilli()), func(c *Config) { c.MaxConns, c.Log = 1, log })
	first, err := client(t, srv.Addrs()[0], k.ca, nil)
	check(t, err)
	t.Cleanup(func() { check(t, first.Close()) })
	echo(t, first, "held")
	if second, cerr := client(t, srv.Addrs()[0], k.ca, nil); cerr == nil {
		closed(t, second)
		closeClosed(t, second)
	}
	leakcheck.Poll(t, func() bool { return strings.Contains(log.String(), "all 1 connections are held") })
}

// T9: the first refusal of a window is logged at once and the rest are counted, then said in
// one line when the window ends, so a flood cannot fill the journal.
func TestT9RefusalsAreLoggedOnceAWindow(t *testing.T) {
	leakcheck.Check(t)
	clk := clock.NewFake(0, fixedNow.UnixMilli())
	log := &lockedLog{}
	s := &Server{cfg: Config{Clock: clk, Log: log}}
	for range 5 {
		s.refused("a connection from %s refused: %s", "192.0.2.7", "all 1 connections are held")
	}
	if got := log.lines(); len(got) != 1 || !strings.Contains(got[0], "192.0.2.7") {
		t.Fatalf("within the window: %q", got)
	}
	check(t, clk.AwaitDue(t.Context(), RefusalWindow))
	clk.Advance(RefusalWindow)
	if got := log.lines(); len(got) != 2 || !strings.Contains(got[1], "4 more refused") {
		t.Fatalf("after the window: %q", got)
	}
	s.refused("a connection from %s refused: %s", "192.0.2.8", "x")
	if got := log.lines(); len(got) != 3 {
		t.Fatalf("a new window: %q", got)
	}
	s.shutdown()
}

// T10 (Plan 00257 SEC-5): a first request outside the Matrix API is answered 403 and closed, and
// the homeserver is never dialled for it.
func TestT10TheAdminAPIIsRefusedAndNeverReachesTheUpstream(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	log := &lockedLog{}
	srv := serve(t, k, up.addr, clock.NewFake(0, fixedNow.UnixMilli()), func(c *Config) { c.Log = log })
	for _, target := range []string{
		"/_synapse/admin/v1/register",
		"/_synapse/admin/v2/users/@a:ops.example",
		"/_tuwunel/server_version",
		"/",
		"/_matrix/../_synapse/admin/v1/register",
		"/_matrix/%2e%2E/_synapse/admin/v1/register",
		"/_matrix/client/./x",
		"http://192.0.2.10/_matrix/client/versions",
	} {
		c, err := client(t, srv.Addrs()[0], k.ca, nil)
		check(t, err)
		_, err = c.Write([]byte("GET " + target + " HTTP/1.1\r\nHost: x\r\n\r\n"))
		check(t, err)
		status, body, shut := answered(t, c)
		if status != http.StatusForbidden || !strings.Contains(body, "M_FORBIDDEN") || !shut {
			t.Errorf("%s: %d %s, closed %v", target, status, body, shut)
		}
		closeClosed(t, c)
	}
	select {
	case <-up.accepted:
		t.Fatal("the upstream was dialled for a refused request")
	default:
	}
	if strings.Contains(log.String(), "@a:ops.example") {
		t.Errorf("the log names more of a path than its first segments: %q", log.String())
	}
}

// T10: the Matrix API, the well-known discovery and the probe's versions are relayed.
func TestT10TheMatrixAPIIsRelayed(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	srv := serve(t, k, up.addr, clock.NewFake(0, fixedNow.UnixMilli()), nil)
	c, err := client(t, srv.Addrs()[0], k.ca, nil)
	check(t, err)
	t.Cleanup(func() { check(t, c.Close()) })
	for _, req := range []string{
		"GET /_matrix/client/versions HTTP/1.1\r\nHost: x\r\n\r\n",
		"GET /.well-known/matrix/client HTTP/1.1\r\nHost: x\r\n\r\n",
		"GET /_matrix/media/v3/download/ops.example/abc?allow_redirect=true HTTP/1.1\r\nHost: x\r\n\r\n",
		"PUT /_matrix/client/v3/rooms/%21r%3Aops.example/send/m.room.message/1 HTTP/1.1\r\nHost: x\r\n" +
			"Transfer-Encoding: chunked\r\n\r\n5;ext=1\r\nhello\r\n0\r\nX-Trailer: t\r\n\r\n",
	} {
		_, err := c.Write([]byte(req))
		check(t, err)
		got := make([]byte, len(req))
		testconn.Bound(t, c)
		_, err = io.ReadFull(c, got)
		check(t, err)
		if string(got) != req {
			t.Fatalf("relayed %q, sent %q", got, req)
		}
	}
}

// T10: a later request on a connection is judged as the first is: none of the bytes of one outside
// the Matrix API reach the homeserver, the answers to the requests before it still reach the
// client, and the connection then ends.
func TestT10ALaterRequestIsJudgedToo(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	srv := serve(t, k, up.addr, clock.NewFake(0, fixedNow.UnixMilli()), nil)
	c, err := client(t, srv.Addrs()[0], k.ca, nil)
	check(t, err)
	t.Cleanup(func() { closeClosed(t, c) })
	allowed := request("first")
	_, err = c.Write([]byte(allowed + "GET /_synapse/admin/v1/users HTTP/1.1\r\nHost: x\r\n\r\n"))
	check(t, err)
	testconn.Bound(t, c)
	all, err := io.ReadAll(c)
	if err != nil && !strings.Contains(err.Error(), "reset") {
		check(t, err)
	}
	if string(all) != allowed {
		t.Errorf("the homeserver saw %q, want only the allowed request", all)
	}
}

// T10: an admin request after one with a body, framed by its length or chunked, is judged at
// the body's end: none of its bytes reach the homeserver, and the request before it does whole.
func TestT10AnAdminRequestAfterABodyIsRefused(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	srv := serve(t, k, up.addr, clock.NewFake(0, fixedNow.UnixMilli()), nil)
	for _, allowed := range []string{
		request("a body that holds GET /_synapse/admin/v1/users HTTP/1.1"),
		"PUT /_matrix/client/v3/x HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: chunked\r\n\r\n" +
			"5\r\nhello\r\n0\r\n\r\n",
	} {
		c, err := client(t, srv.Addrs()[0], k.ca, nil)
		check(t, err)
		_, err = c.Write([]byte(allowed + "GET /_synapse/admin/v1/users HTTP/1.1\r\nHost: x\r\n\r\n"))
		check(t, err)
		testconn.Bound(t, c)
		all, err := io.ReadAll(c)
		if err != nil && !strings.Contains(err.Error(), "reset") {
			check(t, err)
		}
		if string(all) != allowed {
			t.Errorf("the homeserver saw %q, want only the allowed request", all)
		}
		closeClosed(t, c)
	}
}

// T10: a chunk size is hexadecimal digits alone (RFC 9112 7.1): a sign or a space around it is
// refused, and its line never reaches the homeserver.
func TestT10AChunkSizeIsHexDigitsAlone(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	srv := serve(t, k, up.addr, clock.NewFake(0, fixedNow.UnixMilli()), nil)
	head := "PUT /_matrix/client/v3/x HTTP/1.1\r\nHost: x\r\nTransfer-Encoding: chunked\r\n\r\n"
	for _, size := range []string{"+5", " 5", "5 ", "5 ;x=1", "\t5", "0x5", ""} {
		c, err := client(t, srv.Addrs()[0], k.ca, nil)
		check(t, err)
		_, err = c.Write([]byte(head + size + "\r\nhello\r\n0\r\n\r\n"))
		check(t, err)
		testconn.Bound(t, c)
		all, err := io.ReadAll(c)
		if err != nil && !strings.Contains(err.Error(), "reset") {
			check(t, err)
		}
		if string(all) != head {
			t.Errorf("size %q: the homeserver saw %q, want the head alone", size, all)
		}
		closeClosed(t, c)
	}
}

// T10: framing two readers could read two ways is refused before it reaches the homeserver.
func TestT10AmbiguousFramingIsRefused(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	srv := serve(t, k, up.addr, clock.NewFake(0, fixedNow.UnixMilli()), nil)
	for _, c := range []struct{ name, req string }{
		{"length and chunked", "POST /_matrix/client/v3/x HTTP/1.1\r\nContent-Length: 3\r\nTransfer-Encoding: chunked\r\n\r\n"},
		{"two lengths", "POST /_matrix/client/v3/x HTTP/1.1\r\nContent-Length: 3\r\nContent-Length: 4\r\n\r\n"},
		{"gzip encoding", "POST /_matrix/client/v3/x HTTP/1.1\r\nTransfer-Encoding: gzip, chunked\r\n\r\n"},
		{"a signed length", "POST /_matrix/client/v3/x HTTP/1.1\r\nContent-Length: +3\r\n\r\n"},
		{"a folded header", "GET /_matrix/client/versions HTTP/1.1\r\nX-A: 1\r\n folded\r\n\r\n"},
		{"an upgrade", "GET /_matrix/client/versions HTTP/1.1\r\nUpgrade: h2c\r\n\r\n"},
		{"a bare LF", "GET /_matrix/client/versions HTTP/1.1\nHost: x\n\n"},
		{"HTTP/2 preface", "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"},
		{"CONNECT", "CONNECT /_matrix/client/v3 HTTP/1.1\r\n\r\n"},
		{"HTTP/1.0 chunked", "POST /_matrix/client/v3/x HTTP/1.0\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n"},
	} {
		name, req := c.name, c.req
		c, err := client(t, srv.Addrs()[0], k.ca, nil)
		check(t, err)
		_, err = c.Write([]byte(req))
		check(t, err)
		status, _, shut := answered(t, c)
		if status < 400 || status == http.StatusForbidden || !shut {
			t.Errorf("%s: %d, closed %v", name, status, shut)
		}
		closeClosed(t, c)
	}
	select {
	case <-up.accepted:
		t.Fatal("the upstream was dialled for a refused request")
	default:
	}
}

// T10: a client that sends no request within RequestLimit of its handshake is closed, and the
// homeserver is never dialled for it.
func TestT10ASilentClientIsClosedAtRequestLimit(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	clk := clock.NewFake(0, fixedNow.UnixMilli())
	srv := serve(t, k, up.addr, clk, nil)
	c, err := client(t, srv.Addrs()[0], k.ca, nil)
	check(t, err)
	t.Cleanup(func() { closeClosed(t, c) })
	check(t, clk.AwaitDueFrom(t.Context(), RequestLimit, "(*Server).firstRequest"))
	clk.Advance(RequestLimit)
	if !closed(t, c) {
		t.Fatal("a client that sent no request was left open")
	}
	select {
	case <-up.accepted:
		t.Fatal("the upstream was dialled for a client that sent no request")
	default:
	}
}

// T10: RequestLimit is shorter than IdleLimit: a connection that has sent no request holds its
// slot for less time than a relay may sit idle.
func TestT10RequestLimitIsBelowIdleLimit(t *testing.T) {
	if RequestLimit >= IdleLimit {
		t.Fatalf("RequestLimit %v is not below IdleLimit %v", RequestLimit, IdleLimit)
	}
}
