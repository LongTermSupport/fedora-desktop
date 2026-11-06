// Package tlsterm is a team's TLS terminator, the process of the unit teamchat-tls@<team>
// (Plan 00246 SPEC-cross-network X18): it accepts TLS on the team's derived listen addresses,
// presenting the team's leaf certificate, and relays the bytes to the team's homeserver on
// loopback, which stays plain there (X12). It holds the leaf and its key only: the CA key stays
// with root (X13), and a terminator cannot make a certificate.
//
// This package and package matrix are the only places under teamchat that build a tls.Config
// (X19, project_transport_home): the server's side is built here, TLS 1.3 at least, with the one
// certificate and no client authentication, because a member authenticates to the homeserver
// with its Matrix credential inside the connection.
//
// Expectations (each has a test whose name starts with its number):
//
//	T1 It serves TLS 1.3 on every listen address and relays the bytes both ways to the upstream,
//	   a half-close included.
//	T2 A client that is not TLS 1.3, is not TLS, or does not trust the leaf never reaches the
//	   upstream: the upstream is dialled only after the handshake succeeded.
//	T3 A client that does not finish its handshake in HandshakeLimit is closed.
//	T4 An upstream that does not answer closes the client's connection, and the failure is logged.
//	T5 At most MaxConns connections are held at once, handshaking or relayed; the next is closed
//	   unserved.
//	T6 Close ends every listener, relay and goroutine, and is safe to call twice.
//	T7 A configuration that cannot serve is refused naming why, and holds nothing.
package tlsterm

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"
)

const (
	// HandshakeLimit is how long a client has to finish its TLS handshake.
	HandshakeLimit = 10 * time.Second
	// UpstreamLimit is how long the dial of the homeserver may take: it is on loopback.
	UpstreamLimit = 5 * time.Second
	// DefaultMaxConns is the number of connections held at once when none is configured.
	DefaultMaxConns = 256
	// maxConnsLimit is the most a configuration may ask for, well inside a unit's descriptors.
	maxConnsLimit = 4096
)

// Clock is the one reading the terminator takes: a timer, for its bounds.
type Clock interface {
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

// Config is what a terminator serves. Every field but MaxConns and Log is required.
type Config struct {
	// Listen are the addresses to accept on, each once.
	Listen []netip.AddrPort
	// Upstream is the homeserver, on a loopback address and its port.
	Upstream netip.AddrPort
	// CertPEM and KeyPEM are the leaf certificate and its key.
	CertPEM, KeyPEM []byte
	// Clock arms HandshakeLimit and UpstreamLimit.
	Clock Clock
	// MaxConns is the most connections held at once; 0 is DefaultMaxConns.
	MaxConns int
	// Log, when set, gets a line for each failure that is not a client's. It never gets a
	// client's bytes.
	Log io.Writer
}

// Server is a running terminator.
type Server struct {
	cfg    Config
	tls    *tls.Config
	lns    []net.Listener
	addrs  []netip.AddrPort
	slots  chan struct{}
	done   <-chan struct{}
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once

	mu    sync.Mutex
	live  map[net.Conn]struct{}
	fatal error
	logMu sync.Mutex
}

// Start binds every listen address and begins serving until ctx ends or Close, or refuses the
// configuration and holds nothing.
func Start(ctx context.Context, cfg Config) (*Server, error) {
	tlsConfig, err := cfg.check()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Server{
		cfg: cfg, tls: tlsConfig, slots: make(chan struct{}, cfg.maxConns()),
		done: ctx.Done(), cancel: cancel, live: map[net.Conn]struct{}{},
	}
	for _, addr := range cfg.Listen {
		ln, lerr := (&net.ListenConfig{}).Listen(ctx, "tcp", addr.String())
		if lerr != nil {
			return nil, errors.Join(fmt.Errorf("tlsterm: listening on %s: %w", addr, lerr), s.Close())
		}
		s.lns = append(s.lns, ln)
		s.addrs = append(s.addrs, netip.MustParseAddrPort(ln.Addr().String()))
	}
	for _, ln := range s.lns {
		s.wg.Go(func() { s.accept(ctx, ln) })
	}
	context.AfterFunc(ctx, func() { s.shutdown() })
	return s, nil
}

// Run serves until ctx ends or a listener fails, then closes; it answers the failure, nil for the
// end of ctx.
func Run(ctx context.Context, cfg Config) error {
	s, err := Start(ctx, cfg)
	if err != nil {
		return err
	}
	<-s.done
	return errors.Join(s.Err(), s.Close())
}

// Addrs are the addresses the terminator listens on, the ports a port of 0 was given included.
func (s *Server) Addrs() []netip.AddrPort { return append([]netip.AddrPort(nil), s.addrs...) }

// Err is what made the terminator stop on its own, nil while it serves and after a Close.
func (s *Server) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fatal
}

// Close ends every listener, every relay and every goroutine.
func (s *Server) Close() error {
	s.cancel()
	s.shutdown()
	s.wg.Wait()
	return nil
}

// shutdown closes the listeners and every connection held, once.
func (s *Server) shutdown() {
	s.once.Do(func() {
		for _, ln := range s.lns {
			s.closeListener(ln)
		}
		s.mu.Lock()
		held := make([]net.Conn, 0, len(s.live))
		for c := range s.live {
			held = append(held, c)
		}
		s.mu.Unlock()
		for _, c := range held {
			s.closeConn(c)
		}
	})
}

func (c Config) maxConns() int {
	if c.MaxConns == 0 {
		return DefaultMaxConns
	}
	return c.MaxConns
}

// check is the configuration's refusals, and the TLS configuration it serves.
func (c Config) check() (*tls.Config, error) {
	switch {
	case len(c.Listen) == 0:
		return nil, errors.New("tlsterm: no listen address")
	case c.Clock == nil:
		return nil, errors.New("tlsterm: a terminator needs a Clock")
	case c.MaxConns < 0 || c.MaxConns > maxConnsLimit:
		return nil, fmt.Errorf("tlsterm: %d connections is not between 0 and %d", c.MaxConns, maxConnsLimit)
	case !c.Upstream.IsValid() || !c.Upstream.Addr().IsLoopback() || c.Upstream.Port() == 0:
		return nil, fmt.Errorf("tlsterm: the upstream %s is not a loopback address and a port", c.Upstream)
	}
	seen := map[netip.AddrPort]bool{}
	for _, a := range c.Listen {
		if !a.IsValid() || a.Addr().IsUnspecified() || a.Addr().Is4In6() {
			return nil, fmt.Errorf("tlsterm: listen address %s is not one specific address and a port", a)
		}
		if seen[a] {
			return nil, fmt.Errorf("tlsterm: listen address %s is named twice", a)
		}
		seen[a] = true
	}
	cert, err := tls.X509KeyPair(c.CertPEM, c.KeyPEM)
	if err != nil {
		return nil, fmt.Errorf("tlsterm: the leaf certificate and key: %w", err)
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}, nil
}

// accept takes connections off one listener until it is closed, and stops the whole terminator
// on any other failure, which a unit's restart then answers.
func (s *Server) accept(ctx context.Context, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				s.stop(fmt.Errorf("tlsterm: accepting on %s: %w", ln.Addr(), err))
			}
			return
		}
		s.wg.Go(func() { s.handle(ctx, conn) })
	}
}

// stop records why the terminator ended on its own and ends it.
func (s *Server) stop(err error) {
	s.mu.Lock()
	if s.fatal == nil {
		s.fatal = err
	}
	s.mu.Unlock()
	s.cancel()
}

// handle serves one accepted connection: a slot, the handshake, the upstream, the relay.
func (s *Server) handle(ctx context.Context, conn net.Conn) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		s.closeConn(conn)
		return
	}
	if !s.track(conn) {
		s.closeConn(conn)
		return
	}
	defer s.untrack(conn)
	tc := tls.Server(conn, s.tls)
	if !s.handshake(ctx, tc) {
		s.closeConn(conn)
		return
	}
	up, err := s.dial(ctx)
	if err != nil {
		s.logf("upstream %s: %v", s.cfg.Upstream, err)
		s.closeConn(tc)
		return
	}
	if !s.track(up) {
		s.closeConn(up)
		s.closeConn(tc)
		return
	}
	defer s.untrack(up)
	s.relay(tc, up)
}

// handshake is whether the client completed its handshake in HandshakeLimit.
func (s *Server) handshake(ctx context.Context, tc *tls.Conn) bool {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := s.cfg.Clock.AfterFunc(HandshakeLimit, cancel)
	defer stop()
	return tc.HandshakeContext(ctx) == nil
}

// dial is a connection to the upstream, in UpstreamLimit.
func (s *Server) dial(ctx context.Context) (net.Conn, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := s.cfg.Clock.AfterFunc(UpstreamLimit, cancel)
	defer stop()
	return (&net.Dialer{}).DialContext(ctx, "tcp", s.cfg.Upstream.String())
}

// relay copies each way until both have ended, a half-close passed on, and closes both. A failed
// copy ends the relay at once.
func (s *Server) relay(client, upstream net.Conn) {
	var wg sync.WaitGroup
	wg.Go(func() { s.pipe(upstream, client) })
	s.pipe(client, upstream)
	wg.Wait()
	s.closeConn(client)
	s.closeConn(upstream)
}

// pipe copies src to dst; at its end it half-closes dst, or closes both after a failure so the
// other direction ends too. A reset or a close from the other side is the end of a relay, not
// a fault of the terminator's.
func (s *Server) pipe(dst, src net.Conn) {
	if _, err := io.Copy(dst, src); err != nil {
		s.closeConn(dst)
		s.closeConn(src)
		return
	}
	cw, ok := dst.(interface{ CloseWrite() error })
	if !ok || cw.CloseWrite() != nil {
		s.closeConn(dst)
		s.closeConn(src)
	}
}

func (s *Server) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.done:
		return false
	default:
	}
	s.live[c] = struct{}{}
	return true
}

func (s *Server) untrack(c net.Conn) {
	s.mu.Lock()
	delete(s.live, c)
	s.mu.Unlock()
}

// closeConn closes c; a connection already closed is no failure.
func (s *Server) closeConn(c net.Conn) {
	if err := c.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		s.logf("closing a connection: %v", err)
	}
}

func (s *Server) closeListener(ln net.Listener) {
	if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		s.logf("closing %s: %v", ln.Addr(), err)
	}
}

// logf writes one line of the log, when there is one.
func (s *Server) logf(format string, args ...any) {
	if s.cfg.Log == nil {
		return
	}
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if _, err := fmt.Fprintf(s.cfg.Log, "tlsterm: "+format+"\n", args...); err != nil {
		s.cfg.Log = nil
	}
}
