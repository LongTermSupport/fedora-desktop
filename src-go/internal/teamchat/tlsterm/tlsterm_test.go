package tlsterm

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/leakcheck"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
)

// fixedNow is the instant the fake clock reads, and the client's view of the time: a leaf made
// at it is valid at it, whatever the real date is.
var fixedNow = time.Date(2026, time.November, 3, 12, 0, 0, 0, time.UTC)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// keys are a team's CA certificate and a leaf signed by it for the loopback addresses.
type keys struct{ ca, cert, key []byte }

func newKeys(t *testing.T) keys {
	t.Helper()
	maker := pki.Maker{Clock: clock.NewFake(0, fixedNow.UnixMilli())}
	ca, err := maker.NewCA("team-a")
	check(t, err)
	leaf, err := maker.NewLeaf(ca, pki.Names{IPs: []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("127.0.0.2")}})
	check(t, err)
	return keys{ca: ca.CertPEM(), cert: leaf.CertPEM(), key: leaf.KeyPEM()}
}

// upstream is a loopback echo server standing for the homeserver, and the number of connections
// it was given.
type upstream struct {
	addr     netip.AddrPort
	accepted chan struct{}
}

func newUpstream(t *testing.T) *upstream {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	check(t, err)
	u := &upstream{addr: netip.MustParseAddrPort(ln.Addr().String()), accepted: make(chan struct{}, 64)}
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			u.accepted <- struct{}{}
			wg.Go(func() {
				if _, cerr := io.Copy(c, c); cerr != nil {
					t.Logf("the echo ended: %v", cerr)
				}
				if cerr := c.Close(); cerr != nil && !errors.Is(cerr, net.ErrClosed) {
					t.Errorf("closing the echo: %v", cerr)
				}
			})
		}
	})
	t.Cleanup(func() {
		check(t, ln.Close())
		wg.Wait()
	})
	return u
}

// deadUpstream is an address nothing listens on.
func deadUpstream(t *testing.T) netip.AddrPort {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	check(t, err)
	addr := netip.MustParseAddrPort(ln.Addr().String())
	check(t, ln.Close())
	return addr
}

func serve(t *testing.T, k keys, up netip.AddrPort, clk Clock, mutate func(*Config)) *Server {
	t.Helper()
	cfg := Config{
		Listen:   []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0")},
		Upstream: up, CertPEM: k.cert, KeyPEM: k.key, Clock: clk,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	srv, err := Start(t.Context(), cfg)
	check(t, err)
	t.Cleanup(func() { check(t, srv.Close()) })
	return srv
}

// client is a member's side: the team's CA is its only root, TLS 1.3 its least version.
func client(t *testing.T, addr netip.AddrPort, ca []byte, mutate func(*tls.Config)) (*tls.Conn, error) {
	t.Helper()
	pool, err := pki.Pool(ca)
	check(t, err)
	cfg := &tls.Config{
		RootCAs: pool, ServerName: addr.Addr().String(), MinVersion: tls.VersionTLS13,
		Time: func() time.Time { return fixedNow },
	}
	if mutate != nil {
		mutate(cfg)
	}
	conn, err := (&tls.Dialer{Config: cfg}).DialContext(t.Context(), "tcp", addr.String())
	if err != nil {
		return nil, err
	}
	tc, ok := conn.(*tls.Conn)
	if !ok {
		t.Fatalf("a TLS dial gave a %T", conn)
	}
	return tc, nil
}

func echo(t *testing.T, c net.Conn, text string) {
	t.Helper()
	_, err := c.Write([]byte(text))
	check(t, err)
	got := make([]byte, len(text))
	_, err = io.ReadFull(c, got)
	check(t, err)
	if string(got) != text {
		t.Fatalf("echoed %q, sent %q", got, text)
	}
}

// closed is whether the next read of c ends in EOF or a reset: the server closed it.
func closed(c net.Conn) bool {
	_, err := c.Read(make([]byte, 1))
	return errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || err != nil
}

// T1: TLS 1.3 on the listen address, the leaf presented, the bytes relayed both ways.
func TestT1RelaysBothWaysOverTLS13(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	srv := serve(t, k, up.addr, clock.NewFake(0, fixedNow.UnixMilli()), nil)
	c, err := client(t, srv.Addrs()[0], k.ca, nil)
	check(t, err)
	t.Cleanup(func() { check(t, c.Close()) })
	if v := c.ConnectionState().Version; v != tls.VersionTLS13 {
		t.Fatalf("version %#x, want TLS 1.3", v)
	}
	echo(t, c, "hello")
	blob := bytes.Repeat([]byte("0123456789abcdef"), 1<<16)
	errc := make(chan error, 1)
	go func() {
		_, werr := c.Write(blob)
		errc <- werr
	}()
	got := make([]byte, len(blob))
	_, err = io.ReadFull(c, got)
	check(t, err)
	check(t, <-errc)
	if !bytes.Equal(got, blob) {
		t.Fatal("a megabyte came back changed")
	}
}

// T1: every listen address serves, each on its own port.
func TestT1EveryListenAddressServes(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	srv := serve(t, k, up.addr, clock.NewFake(0, fixedNow.UnixMilli()), func(c *Config) {
		c.Listen = []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0"), netip.MustParseAddrPort("127.0.0.2:0")}
	})
	if got := len(srv.Addrs()); got != 2 {
		t.Fatalf("%d bound addresses, want 2", got)
	}
	for _, addr := range srv.Addrs() {
		c, err := client(t, addr, k.ca, nil)
		check(t, err)
		echo(t, c, "to "+addr.String())
		check(t, c.Close())
	}
}

// T1: a half-close is relayed: the client ends its writing, the upstream sees the end and ends
// its own, and the client reads to the end.
func TestT1RelaysAHalfClose(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	srv := serve(t, k, up.addr, clock.NewFake(0, fixedNow.UnixMilli()), nil)
	c, err := client(t, srv.Addrs()[0], k.ca, nil)
	check(t, err)
	t.Cleanup(func() { check(t, c.Close()) })
	_, err = c.Write([]byte("last words"))
	check(t, err)
	check(t, c.CloseWrite())
	all, err := io.ReadAll(c)
	check(t, err)
	if string(all) != "last words" {
		t.Fatalf("read %q after the half-close", all)
	}
}

// T2: a client that is not TLS 1.3, one that is not TLS, and one that trusts another CA never
// reach the upstream.
func TestT2WhatIsNotTLS13NeverReachesTheUpstream(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	srv := serve(t, k, up.addr, clock.NewFake(0, fixedNow.UnixMilli()), nil)
	addr := srv.Addrs()[0]

	if _, err := client(t, addr, k.ca, func(c *tls.Config) { c.MinVersion, c.MaxVersion = tls.VersionTLS12, tls.VersionTLS12 }); err == nil {
		t.Error("a TLS 1.2 client was served")
	}
	plain, err := net.Dial("tcp", addr.String())
	check(t, err)
	_, err = plain.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	check(t, err)
	if !closed(plain) {
		t.Error("plain bytes were not answered with a close")
	}
	check(t, plain.Close())
	other := newKeys(t)
	if _, err := client(t, addr, other.ca, nil); err == nil {
		t.Error("a client of another CA trusted the leaf")
	}
	select {
	case <-up.accepted:
		t.Fatal("the upstream was dialled for a client that never completed a handshake")
	default:
	}
}

// T2: the upstream is dialled after the handshake, once for each connection.
func TestT2TheUpstreamIsDialledAfterTheHandshake(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	srv := serve(t, k, up.addr, clock.NewFake(0, fixedNow.UnixMilli()), nil)
	for range 2 {
		c, err := client(t, srv.Addrs()[0], k.ca, nil)
		check(t, err)
		echo(t, c, "x")
		<-up.accepted
		check(t, c.Close())
	}
}

// T3: a client that does not finish the handshake is closed when HandshakeLimit is up.
func TestT3AHandshakeThatNeverEndsIsClosed(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	clk := clock.NewFake(0, fixedNow.UnixMilli())
	srv := serve(t, k, up.addr, clk, nil)
	conn, err := net.Dial("tcp", srv.Addrs()[0].String())
	check(t, err)
	t.Cleanup(func() { check(t, conn.Close()) })
	check(t, clk.AwaitDue(t.Context(), HandshakeLimit))
	clk.Advance(HandshakeLimit)
	if !closed(conn) {
		t.Fatal("the silent client was not closed")
	}
}

// T4: an upstream that does not answer closes the client's connection and holds nothing.
func TestT4AnUpstreamThatIsDownClosesTheClient(t *testing.T) {
	leakcheck.Check(t)
	k := newKeys(t)
	var logged bytes.Buffer
	srv := serve(t, k, deadUpstream(t), clock.NewFake(0, fixedNow.UnixMilli()), func(c *Config) { c.Log = &logged })
	c, err := client(t, srv.Addrs()[0], k.ca, nil)
	check(t, err)
	t.Cleanup(func() { check(t, c.Close()) })
	if !closed(c) {
		t.Fatal("the client was left open with no upstream")
	}
	check(t, srv.Close())
	if logged.Len() == 0 {
		t.Error("the failed upstream dial was not logged")
	}
}

// T5: no more than MaxConns are relayed at once; the next is closed, and is served again when a
// slot is free.
func TestT5MaxConnsBoundsTheRelays(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	srv := serve(t, k, up.addr, clock.NewFake(0, fixedNow.UnixMilli()), func(c *Config) { c.MaxConns = 1 })
	first, err := client(t, srv.Addrs()[0], k.ca, nil)
	check(t, err)
	echo(t, first, "one")
	second, err := client(t, srv.Addrs()[0], k.ca, nil)
	if err == nil {
		if !closed(second) {
			t.Fatal("a second relay was served beyond MaxConns")
		}
		check(t, second.Close())
	}
	check(t, first.Close())
	<-up.accepted
	var third *tls.Conn
	for {
		third, err = client(t, srv.Addrs()[0], k.ca, nil)
		if err != nil {
			continue
		}
		if _, werr := third.Write([]byte("again")); werr == nil {
			got := make([]byte, len("again"))
			if _, rerr := io.ReadFull(third, got); rerr == nil {
				break
			}
		}
		check(t, third.Close())
	}
	check(t, third.Close())
}

// T6: Close ends the listeners, every relay and every goroutine, and the ports are free again.
func TestT6CloseEndsEverything(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	srv, err := Start(t.Context(), Config{
		Listen:   []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0")},
		Upstream: up.addr, CertPEM: k.cert, KeyPEM: k.key, Clock: clock.NewFake(0, fixedNow.UnixMilli()),
	})
	check(t, err)
	addr := srv.Addrs()[0]
	c, err := client(t, addr, k.ca, nil)
	check(t, err)
	echo(t, c, "up")
	check(t, srv.Close())
	if !closed(c) {
		t.Error("a relay outlived Close")
	}
	check(t, c.Close())
	if conn, derr := net.Dial("tcp", addr.String()); derr == nil {
		check(t, conn.Close())
		t.Error("the listener outlived Close")
	}
	if err := srv.Close(); err != nil {
		t.Errorf("a second Close: %v", err)
	}
}

// T8: a relay that moves no byte either way for IdleLimit is closed, and its slot is free again:
// with MaxConns 1, the next client is served once the idle one is closed.
func TestT8AnIdleRelayIsClosedAndItsSlotFreed(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	clk := clock.NewFake(0, fixedNow.UnixMilli())
	srv := serve(t, k, up.addr, clk, func(c *Config) { c.MaxConns = 1 })
	idle, err := client(t, srv.Addrs()[0], k.ca, nil)
	check(t, err)
	t.Cleanup(func() { check(t, idle.Close()) })
	echo(t, idle, "once")
	<-up.accepted
	check(t, clk.AwaitDue(t.Context(), IdleLimit))
	clk.Advance(IdleLimit)
	if !closed(idle) {
		t.Fatal("a relay idle for IdleLimit was left open")
	}
	leakcheck.Poll(t, func() bool { return len(srv.slots) == 0 })
	next, err := client(t, srv.Addrs()[0], k.ca, nil)
	check(t, err)
	t.Cleanup(func() { check(t, next.Close()) })
	echo(t, next, "served")
}

// T8: a relay that moves bytes both ways within each IdleLimit is never closed: every read and
// write puts the deadline IdleLimit ahead again, here for four times IdleLimit in all.
func TestT8AnActiveRelayIsKeptOpen(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	clk := clock.NewFake(0, fixedNow.UnixMilli())
	srv := serve(t, k, up.addr, clk, nil)
	c, err := client(t, srv.Addrs()[0], k.ca, nil)
	check(t, err)
	t.Cleanup(func() { check(t, c.Close()) })
	for range 5 {
		echo(t, c, "tick")
		check(t, clk.AwaitDue(t.Context(), IdleLimit))
		clk.Advance(IdleLimit - time.Second)
	}
	echo(t, c, "still here")
}

// T8: IdleLimit is above the member's matrix client's own waits, so a healthy connection is
// never cut by it: a sync long-poll answers within 30 s and the client drops a pooled
// connection idle for a minute itself.
func TestT8IdleLimitIsAboveTheClientsOwnWaits(t *testing.T) {
	const clientSyncWait, clientPoolIdle = 30 * time.Second, time.Minute
	if IdleLimit <= clientSyncWait || IdleLimit <= clientPoolIdle {
		t.Fatalf("IdleLimit %v would cut a healthy connection (sync %v, pool idle %v)", IdleLimit, clientSyncWait, clientPoolIdle)
	}
}

// T8: the idle timer's own rules: a touch after the end arms nothing, a firing the next touch
// overtook does nothing, and the end disarms what is armed.
func TestT8TheIdleTimerHonoursItsEndAndItsGenerations(t *testing.T) {
	clk := clock.NewFake(0, fixedNow.UnixMilli())
	expired := 0
	idle := &idleTimer{clock: clk, expire: func() { expired++ }}
	idle.touch()
	staleGen := idle.gen
	idle.touch()
	idle.fire(staleGen)
	if expired != 0 {
		t.Fatal("a firing of an overtaken timer closed the relay")
	}
	idle.end()
	if armed, err := clk.Armed(IdleLimit); err != nil || armed {
		t.Fatalf("the end left the timer armed (%v, %v)", armed, err)
	}
	idle.touch()
	clk.Advance(IdleLimit)
	idle.fire(idle.gen)
	if expired != 0 {
		t.Fatal("a relay that had ended was closed by its idle timer")
	}
}

// T6: Run serves until its context ends and then answers nil, holding nothing.
func TestT6RunEndsWithItsContext(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{
			Listen:   []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0")},
			Upstream: up.addr, CertPEM: k.cert, KeyPEM: k.key, Clock: clock.NewFake(0, fixedNow.UnixMilli()),
		})
	}()
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run answered %v for the end of its context", err)
	}
}

// T6: a terminator that stops on its own (a listener's failure) says why in Err, ends, and Run
// answers that failure.
func TestT6AStopOnItsOwnIsTheFailure(t *testing.T) {
	leakcheck.Check(t)
	k, up := newKeys(t), newUpstream(t)
	srv := serve(t, k, up.addr, clock.NewFake(0, fixedNow.UnixMilli()), nil)
	if err := srv.Err(); err != nil {
		t.Fatalf("a serving terminator has the failure %v", err)
	}
	why := errors.New("tlsterm: accepting: the listener failed")
	srv.stop(why)
	srv.stop(errors.New("a later failure"))
	<-srv.done
	if err := srv.Err(); !errors.Is(err, why) {
		t.Fatalf("Err is %v, want the first failure", err)
	}
}

// refusingLog is a log whose every write fails, after counting it.
type refusingLog struct {
	mu     sync.Mutex
	writes int
}

func (l *refusingLog) Write([]byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.writes++
	return 0, errors.New("the journal is gone")
}

// T4: the log is shared by every connection's goroutine; a log that fails is dropped by one of
// them while the others are still writing, and that is no data race (run under -race), and the
// log is not written again once it has failed.
func TestT4AFailingLogIsDroppedWithoutARace(t *testing.T) {
	log := &refusingLog{}
	s := &Server{cfg: Config{Log: log}}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			<-start
			for range 50 {
				s.logf("upstream %s: %v", "127.0.0.1:8448", "refused")
			}
		})
	}
	close(start)
	wg.Wait()
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.writes != 1 {
		t.Errorf("the failed log was written %d times, want once", log.writes)
	}
}

// T7: a configuration that cannot serve is refused, naming why, and holds nothing.
func TestT7StartRefusesWhatCannotServe(t *testing.T) {
	k, up := newKeys(t), newUpstream(t)
	other := newKeys(t)
	clk := clock.NewFake(0, fixedNow.UnixMilli())
	busy, err := net.Listen("tcp", "127.0.0.2:0")
	check(t, err)
	t.Cleanup(func() { check(t, busy.Close()) })
	free := netip.MustParseAddrPort("127.0.0.1:0")
	for name, mutate := range map[string]func(*Config){
		"no listen address":          func(c *Config) { c.Listen = nil },
		"a listen address twice":     func(c *Config) { c.Listen = []netip.AddrPort{free, free} },
		"an unspecified address":     func(c *Config) { c.Listen = []netip.AddrPort{netip.MustParseAddrPort("0.0.0.0:0")} },
		"an upstream off loopback":   func(c *Config) { c.Upstream = netip.MustParseAddrPort("192.0.2.1:8448") },
		"no upstream port":           func(c *Config) { c.Upstream = netip.MustParseAddrPort("127.0.0.1:0") },
		"a key of another leaf":      func(c *Config) { c.KeyPEM = other.key },
		"no certificate":             func(c *Config) { c.CertPEM = nil },
		"text for a key":             func(c *Config) { c.KeyPEM = []byte("not a key") },
		"no clock":                   func(c *Config) { c.Clock = nil },
		"an address already in use":  func(c *Config) { c.Listen = []netip.AddrPort{free, netip.MustParseAddrPort(busy.Addr().String())} },
		"a negative connection cap":  func(c *Config) { c.MaxConns = -1 },
		"a cap past the descriptors": func(c *Config) { c.MaxConns = maxConnsLimit + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			leakcheck.Check(t)
			cfg := Config{Listen: []netip.AddrPort{free}, Upstream: up.addr, CertPEM: k.cert, KeyPEM: k.key, Clock: clk}
			mutate(&cfg)
			srv, err := Start(t.Context(), cfg)
			if err == nil {
				check(t, srv.Close())
				t.Fatal("Start served it")
			}
		})
	}
}
