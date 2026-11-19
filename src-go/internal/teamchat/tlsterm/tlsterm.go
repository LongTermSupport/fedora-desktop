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
//	   unserved, and the refusal is logged (T9).
//	T6 Close ends every listener, relay and goroutine, and is safe to call twice.
//	T7 A configuration that cannot serve is refused naming why, and holds nothing.
//	T8 A relay that moves no byte either way for IdleLimit is closed, both ends, and its slot is
//	   free; every read and write puts the deadline IdleLimit ahead again. IdleLimit is above the
//	   member's matrix client's own waits (a 30 s sync long-poll, a minute's idle pool), so a
//	   healthy connection is never cut by it.
//	T9 One source address holds at most MaxPerSource connections at once, so one client, which
//	   needs no credential to complete a handshake, cannot fill every slot (Plan 00257 SEC-4); a
//	   team sets it (connections_per_source, `teamchat tls --per-source`). Each refusal is
//	   logged: the first of each RefusalWindow at once, naming the source and why, and the count
//	   of the rest when the window ends.
//	T9a A source at its cap has its held connections checked for life before a new one is
//	   refused (Plan 00257 B.5 review): a short keepalive (Probe) fails those a suspended or
//	   vanished host left behind, whose relays then end and free their shares, and the new
//	   connection waits for a share at most ProbeLimit. Members behind one address reconnecting
//	   after a suspend are admitted, not refused until IdleLimit ends their old connections.
//	T10 Only the Matrix API is relayed (Plan 00257 SEC-5): each request's line and headers are
//	   read and judged before a byte of it reaches the upstream, by the request gate (gate.go),
//	   and the upstream is dialled only once the first request has passed, within RequestLimit of
//	   the handshake. A refused first request is answered and closed; a refused later one closes
//	   the connection. The homeserver's admin routes are reached on loopback only.
package tlsterm

import (
	"bufio"
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
	// IdleLimit is how long a relay may move no byte either way before it is closed.
	IdleLimit = 2 * time.Minute
	// DefaultMaxConns is the number of connections held at once when none is configured.
	DefaultMaxConns = 256
	// DefaultMaxPerSource is the number one source address holds at once when none is
	// configured (Plan 00257 B.5 review, the coordinator's ruling): every slot, so members behind
	// one address are never refused by default; a team declares a lower cap
	// (connections_per_source) to keep one address from holding them all.
	DefaultMaxPerSource = DefaultMaxConns
	// RefusalWindow is how often a refusal is logged at most; the rest are counted (T9).
	RefusalWindow = time.Minute
	// ProbeLimit is how long a source at its cap waits for the liveness check to reclaim one of
	// its connections.
	ProbeLimit = 4 * time.Second
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
	// Clock arms HandshakeLimit, UpstreamLimit and IdleLimit.
	Clock Clock
	// MaxConns is the most connections held at once; 0 is DefaultMaxConns.
	MaxConns int
	// MaxPerSource is the most connections one source address holds at once; 0 is
	// DefaultMaxPerSource.
	MaxPerSource int
	// Probe is the liveness check of a source's held connections; nil is the keepalive probe.
	Probe func(conns []net.Conn)
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

	// live, perSource, bySource, released and fatal are under mu. bySource holds each source's
	// admitted connections, for the liveness check; released is closed and replaced at each
	// release, for a reclaim waiting on one.
	live      map[net.Conn]struct{}
	perSource map[netip.Addr]int
	bySource  map[netip.Addr]map[net.Conn]struct{}
	released  chan struct{}
	fatal     error
	once      sync.Once
	mu        sync.Mutex

	// refusing, refusedMore, refusalStop and refusalsEnded are the refusal log's window, under
	// refusalMu.
	refusalStop   func() bool
	refusedMore   int
	refusalMu     sync.Mutex
	refusing      bool
	refusalsEnded bool

	logMu     sync.Mutex
	logFailed bool
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
		done: ctx.Done(), cancel: cancel, live: map[net.Conn]struct{}{}, perSource: map[netip.Addr]int{},
		bySource: map[netip.Addr]map[net.Conn]struct{}{}, released: make(chan struct{}),
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
		s.refusalMu.Lock()
		s.refusalsEnded = true
		if s.refusalStop != nil {
			s.refusalStop()
		}
		s.refusalMu.Unlock()
	})
}

func (c Config) maxConns() int {
	if c.MaxConns == 0 {
		return DefaultMaxConns
	}
	return c.MaxConns
}

// maxPerSource is the configured bound of one source, never above the slots.
func (c Config) maxPerSource() int {
	n := c.MaxPerSource
	if n == 0 {
		n = DefaultMaxPerSource
	}
	return min(n, c.maxConns())
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
	case c.MaxPerSource < 0 || c.MaxPerSource > maxConnsLimit:
		return nil, fmt.Errorf("tlsterm: %d connections a source is not between 0 and %d", c.MaxPerSource, maxConnsLimit)
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

// handle serves one accepted connection: a slot and its source's share, the handshake, the first
// request, the upstream, the relay.
func (s *Server) handle(ctx context.Context, conn net.Conn) {
	src := sourceOf(conn)
	release, why := s.admit(src, conn)
	if why != "" {
		s.refused("a connection from %s refused: %s", src, why)
		s.closeConn(conn)
		return
	}
	defer release()
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
	in := &source{r: tc}
	br := bufio.NewReaderSize(in, maxLine)
	first, ok := s.firstRequest(conn, tc, br, src)
	if !ok {
		s.closeConn(tc)
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
	s.relay(tc, up, in, br, first, src)
}

// sourceOf is the address a connection comes from, one bucket for any it cannot read.
func sourceOf(conn net.Conn) netip.Addr {
	ap, err := netip.ParseAddrPort(conn.RemoteAddr().String())
	if err != nil {
		return netip.Addr{}
	}
	return ap.Addr().Unmap()
}

// admit takes a slot and a share of src's for conn, and answers their release, or why there is
// none. A source at its cap first has its held connections checked for life (reclaim), so those
// a suspended or vanished host left behind free their shares; it is refused only if none does.
func (s *Server) admit(src netip.Addr, conn net.Conn) (func(), string) {
	select {
	case s.slots <- struct{}{}:
	default:
		return nil, fmt.Sprintf("all %d connections are held", s.cfg.maxConns())
	}
	for checked := false; ; checked = true {
		s.mu.Lock()
		n := s.perSource[src]
		if n < s.cfg.maxPerSource() {
			s.perSource[src]++
			if s.bySource[src] == nil {
				s.bySource[src] = map[net.Conn]struct{}{}
			}
			s.bySource[src][conn] = struct{}{}
			s.mu.Unlock()
			return func() { s.release(src, conn) }, ""
		}
		held := make([]net.Conn, 0, len(s.bySource[src]))
		for c := range s.bySource[src] {
			held = append(held, c)
		}
		s.mu.Unlock()
		if checked {
			<-s.slots
			return nil, fmt.Sprintf("%d connections are held from it, and each answered the liveness check", n)
		}
		s.reclaim(src, held)
	}
}

// release gives back conn's share of src and its slot, and wakes a reclaim waiting on src.
func (s *Server) release(src netip.Addr, conn net.Conn) {
	s.mu.Lock()
	if s.perSource[src]--; s.perSource[src] <= 0 {
		delete(s.perSource, src)
	}
	delete(s.bySource[src], conn)
	if len(s.bySource[src]) == 0 {
		delete(s.bySource, src)
	}
	close(s.released)
	s.released = make(chan struct{})
	s.mu.Unlock()
	<-s.slots
}

// reclaim checks held, src's connections, for life and waits, at most ProbeLimit, until src
// holds fewer than its cap: a dead one fails under the check and its relay releases its share.
func (s *Server) reclaim(src netip.Addr, held []net.Conn) {
	probe := s.cfg.Probe
	if probe == nil {
		probe = s.keepaliveProbe
		defer s.restoreKeepalive(held)
	}
	expired := make(chan struct{})
	stop := s.cfg.Clock.AfterFunc(ProbeLimit, func() { close(expired) })
	defer stop()
	probe(held)
	for {
		s.mu.Lock()
		room := s.perSource[src] < s.cfg.maxPerSource()
		released := s.released
		s.mu.Unlock()
		if room {
			return
		}
		select {
		case <-released:
		case <-expired:
			return
		case <-s.done:
			return
		}
	}
}

// firstRequest is the connection's first request head, read within RequestLimit of the
// handshake; the raw connection is closed when the limit is up, which ends the read. A refused
// one is answered, and logged.
func (s *Server) firstRequest(raw net.Conn, tc *tls.Conn, br *bufio.Reader, src netip.Addr) (head, bool) {
	stop := s.cfg.Clock.AfterFunc(RequestLimit, func() { s.closeConn(raw) })
	defer stop()
	h, err := readHead(br)
	if r, refused := errors.AsType[*refusal](err); refused {
		s.refused("a request from %s refused: %s", src, r.why)
		if _, werr := tc.Write(answer(r)); werr != nil && !errors.Is(werr, net.ErrClosed) {
			s.logf("answering a refused request from %s: %v", src, werr)
		}
		return head{}, false
	}
	return h, err == nil
}

// source is the client's side as the gate reads it: each read that moved a byte touches the
// relay's idle timer once the relay has one.
type source struct {
	r    io.Reader
	idle *idleTimer
}

func (s *source) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 && s.idle != nil {
		s.idle.touch()
	}
	return n, err
}

// refused logs a refusal: the first of a RefusalWindow at once, the count of the rest when the
// window ends (T9).
func (s *Server) refused(format string, args ...any) {
	s.refusalMu.Lock()
	if s.refusalsEnded {
		s.refusalMu.Unlock()
		return
	}
	if s.refusing {
		s.refusedMore++
		s.refusalMu.Unlock()
		return
	}
	s.refusing = true
	s.refusalStop = s.cfg.Clock.AfterFunc(RefusalWindow, s.endRefusalWindow)
	s.refusalMu.Unlock()
	s.logf(format, args...)
}

// endRefusalWindow says how many refusals the window counted, and opens the next.
func (s *Server) endRefusalWindow() {
	s.refusalMu.Lock()
	n := s.refusedMore
	s.refusing, s.refusedMore, s.refusalStop = false, 0, nil
	s.refusalMu.Unlock()
	if n > 0 {
		s.logf("%d more refused in the last %v", n, RefusalWindow)
	}
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

// relay copies each way until both have ended, a half-close passed on, and closes both: the
// client's requests through the gate, from first on, the upstream's answers as they come. A
// failed copy or a refused request ends the relay at once, and so does IdleLimit with no byte
// moved either way: closing both ends makes each copy fail, so the relay ends and its slot is
// free.
func (s *Server) relay(client, upstream net.Conn, in *source, br *bufio.Reader, first head, src netip.Addr) {
	idle := &idleTimer{clock: s.cfg.Clock, expire: func() {
		s.closeConn(client)
		s.closeConn(upstream)
	}}
	idle.touch()
	defer idle.end()
	in.idle = idle
	var wg sync.WaitGroup
	wg.Go(func() { s.forward(upstream, client, br, first, idle, src) })
	s.pipe(client, upstream, idle)
	wg.Wait()
	s.closeConn(client)
	s.closeConn(upstream)
}

// pipe copies src to dst, each byte read or written putting idle's deadline ahead; at its end
// it half-closes dst, or closes both after a failure so the other direction ends too. A reset
// or a close from the other side is the end of a relay, not a fault of the terminator's. The
// copy goes through a reader and a writer that touch idle, not through wrapped connections, so
// dst is still the connection whose CloseWrite passes the half-close on.
func (s *Server) pipe(dst, src net.Conn, idle *idleTimer) {
	if _, err := io.Copy(touchWriter{dst, idle}, touchReader{src, idle}); err != nil {
		s.closeConn(dst)
		s.closeConn(src)
		return
	}
	s.halfClose(dst, src)
}

// forward relays the client's requests to dst, each judged by the gate before its bytes go,
// starting with h, until the client ends between two requests, which is passed on as a
// half-close; a refused request, a broken one or a failed write closes both.
func (s *Server) forward(dst, client net.Conn, br *bufio.Reader, h head, idle *idleTimer, src netip.Addr) {
	w := touchWriter{dst, idle}
	for {
		_, err := w.Write(h.raw)
		if err == nil {
			err = relayBody(w, br, h)
		}
		if err == nil {
			h, err = readHead(br)
		}
		if errors.Is(err, io.EOF) {
			s.halfClose(dst, client)
			return
		}
		if r, ok := errors.AsType[*refusal](err); ok {
			// The refused request's bytes never go; the upstream's answers to the requests
			// before it still reach the client, and the relay then ends.
			s.refused("a request from %s refused: %s", src, r.why)
			s.halfClose(dst, client)
			return
		}
		if err != nil {
			s.closeConn(dst)
			s.closeConn(client)
			return
		}
	}
}

// halfClose passes the end of src's writing on to dst, or closes both when dst cannot.
func (s *Server) halfClose(dst, src net.Conn) {
	cw, ok := dst.(interface{ CloseWrite() error })
	if !ok || cw.CloseWrite() != nil {
		s.closeConn(dst)
		s.closeConn(src)
	}
}

// idleTimer calls expire once IdleLimit has passed since the last touch, unless it has ended.
// The Clock has timers only, so each touch disarms the timer armed by the one before and arms
// a new one; a firing already on its way when a touch disarmed it carries an old generation and
// does nothing.
type idleTimer struct {
	clock  Clock
	expire func()

	mu    sync.Mutex
	gen   uint64
	stop  func() bool
	ended bool
}

// touch puts the deadline IdleLimit from now.
func (t *idleTimer) touch() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ended {
		return
	}
	t.disarm()
	t.gen++
	gen := t.gen
	t.stop = t.clock.AfterFunc(IdleLimit, func() { t.fire(gen) })
}

// fire is the timer armed at generation gen falling due.
func (t *idleTimer) fire(gen uint64) {
	t.mu.Lock()
	if t.ended || gen != t.gen {
		t.mu.Unlock()
		return
	}
	t.ended = true
	t.mu.Unlock()
	t.expire()
}

// end disarms the timer for good: the relay is over.
func (t *idleTimer) end() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ended = true
	t.disarm()
}

// disarm stops the armed timer; whether it had already fired does not matter, because a firing
// is judged by its generation. The caller holds mu.
func (t *idleTimer) disarm() {
	if t.stop != nil {
		t.stop()
		t.stop = nil
	}
}

// touchReader and touchWriter are a relay's two ends as io.Copy sees them: each read or write
// that moved a byte touches the relay's idle timer.
type touchReader struct {
	r    io.Reader
	idle *idleTimer
}

func (r touchReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.idle.touch()
	}
	return n, err
}

type touchWriter struct {
	w    io.Writer
	idle *idleTimer
}

func (w touchWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if n > 0 {
		w.idle.touch()
	}
	return n, err
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

// logf writes one line of the log, when there is one that has not failed. Every connection's
// goroutine logs, so whether the log failed is read and written under logMu only; the
// configuration itself is never written after Start.
func (s *Server) logf(format string, args ...any) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if s.cfg.Log == nil || s.logFailed {
		return
	}
	if _, err := fmt.Fprintf(s.cfg.Log, "tlsterm: "+format+"\n", args...); err != nil {
		s.logFailed = true
	}
}

// probeKeepAlive is the liveness check's keepalive: a probe once a connection has been idle a
// second, which a dead peer (a host suspended, or gone behind a NAT) never answers, so the kernel
// fails the connection after Count probes a second apart and its relay ends.
var probeKeepAlive = net.KeepAliveConfig{Enable: true, Idle: time.Second, Interval: time.Second, Count: 2}

// heldKeepAlive is the keepalive an accepted connection holds otherwise: Go's listener's.
var heldKeepAlive = net.KeepAliveConfig{Enable: true, Idle: 15 * time.Second, Interval: 15 * time.Second, Count: 9}

// keepaliveProbe is the production liveness check: the short keepalive on each connection.
func (s *Server) keepaliveProbe(conns []net.Conn) { s.setKeepAlive(conns, probeKeepAlive) }

// restoreKeepalive gives each connection its usual keepalive once a check is over.
func (s *Server) restoreKeepalive(conns []net.Conn) { s.setKeepAlive(conns, heldKeepAlive) }

func (s *Server) setKeepAlive(conns []net.Conn, cfg net.KeepAliveConfig) {
	for _, c := range conns {
		tc, ok := c.(*net.TCPConn)
		if !ok {
			continue
		}
		if err := tc.SetKeepAliveConfig(cfg); err != nil && !errors.Is(err, net.ErrClosed) {
			s.logf("setting the keepalive of a connection from %s: %v", sourceOf(c), err)
		}
	}
}
