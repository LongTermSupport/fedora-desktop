package matrix

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
)

// The tests are Plan 00246 SPEC-cross-network X32 in the transport: one listen address probed
// through the TLS terminator as a member's client reaches it (X16), each failure a fixed cause.

// reachServer is an https server on 127.0.0.1 behind leaf that answers code to the versions
// path, and the paths it was asked.
type reachServer struct {
	srv   *httptest.Server
	mu    sync.Mutex
	asked []string
}

func serveReach(t *testing.T, leaf pki.Leaf, code int) *reachServer {
	t.Helper()
	pair, err := tls.X509KeyPair(leaf.CertPEM(), leaf.KeyPEM())
	if err != nil {
		t.Fatal(err)
	}
	rs := &reachServer{}
	rs.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.asked = append(rs.asked, r.URL.Path+" auth="+r.Header.Get("Authorization"))
		rs.mu.Unlock()
		rw.WriteHeader(code)
	}))
	rs.srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}
	rs.srv.StartTLS()
	t.Cleanup(rs.srv.Close)
	return rs
}

func (rs *reachServer) addr(t *testing.T) netip.AddrPort {
	t.Helper()
	return netip.MustParseAddrPort(rs.srv.Listener.Addr().String())
}

func (w tlsWorld) reach(ctx context.Context, ca pki.CA, addr netip.AddrPort, timers Timers) error {
	return Reach(ctx, ReachConfig{Addr: addr, CAPEM: string(ca.CertPEM()), Wall: w.clk, Timers: timers})
}

func wantCause(t *testing.T, err error, cause string) {
	t.Helper()
	re, ok := errors.AsType[*ReachError](err)
	if !ok {
		t.Fatalf("error %v (%T), want a *ReachError", err, err)
	}
	if re.Cause != cause {
		t.Fatalf("cause %q, want %q", re.Cause, cause)
	}
}

// An address whose terminator answers the versions path is nil, and the probe sends no
// credential.
func TestReachAnAnsweringAddressIsNil(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team CA")
	rs := serveReach(t, w.leaf(t, ca, "127.0.0.1"), http.StatusOK)
	if err := w.reach(t.Context(), ca, rs.addr(t), w.clk); err != nil {
		t.Fatalf("Reach: %v", err)
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if len(rs.asked) != 1 || rs.asked[0] != "/_matrix/client/versions auth=" {
		t.Fatalf("asked %v", rs.asked)
	}
}

// An address nothing listens on is refused.
func TestReachAClosedAddressIsRefused(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team CA")
	rs := serveReach(t, w.leaf(t, ca, "127.0.0.1"), http.StatusOK)
	addr := rs.addr(t)
	rs.srv.Close()
	wantCause(t, w.reach(t.Context(), ca, addr, w.clk), ReachRefused)
}

// A leaf signed by another CA fails the handshake, naming the address and the pinned CA's
// fingerprint, never the library's words.
func TestReachALeafOfAnotherCANamesThePinnedFingerprint(t *testing.T) {
	w := newTLSWorld()
	ca, other := w.ca(t, "team CA"), w.ca(t, "other CA")
	rs := serveReach(t, w.leaf(t, other, "127.0.0.1"), http.StatusOK)
	addr := rs.addr(t)
	want := "TLS verification of " + addr.String() + " failed against the pinned CA sha256:" + fingerprintOf(t, ca) + ": " + classUnknownCA
	wantCause(t, w.reach(t.Context(), ca, addr, w.clk), want)
}

// A leaf for another address fails the handshake: the name verified is the listen address.
func TestReachALeafForAnotherAddressIsANameMismatch(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team CA")
	rs := serveReach(t, w.leaf(t, ca, "192.0.2.10"), http.StatusOK)
	addr := rs.addr(t)
	want := "TLS verification of " + addr.String() + " failed against the pinned CA sha256:" + fingerprintOf(t, ca) + ": " + classNameMismatch
	wantCause(t, w.reach(t.Context(), ca, addr, w.clk), want)
}

// An answer that is not 200 is its own cause, by its status code alone.
func TestReachAnAnswerThatIsNot200(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team CA")
	rs := serveReach(t, w.leaf(t, ca, "127.0.0.1"), http.StatusBadGateway)
	wantCause(t, w.reach(t.Context(), ca, rs.addr(t), w.clk), "it answered HTTP 502")
}

// hanging is a listener that accepts and never speaks, and closes what it accepted at the end.
func hanging(t *testing.T) netip.AddrPort {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu    sync.Mutex
		conns []net.Conn
		done  = make(chan struct{})
	)
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Error(err)
		}
		<-done
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			if err := c.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	return netip.MustParseAddrPort(ln.Addr().String())
}

// An address that accepts and never answers is no answer within the bound, armed on the injected
// timers: the probe ends when the fake clock passes ReachLimit, not on any real time.
func TestReachAnAddressThatHangsEndsAtTheBound(t *testing.T) {
	if !strings.HasSuffix(ReachNoAnswer, " "+ReachLimit.String()) {
		t.Fatalf("the cause %q does not name the bound %s", ReachNoAnswer, ReachLimit)
	}
	w := newTLSWorld()
	ca := w.ca(t, "team CA")
	addr := hanging(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	got := make(chan error, 1)
	go func() { got <- w.reach(ctx, ca, addr, w.clk) }()
	if err := w.clk.AwaitDue(ctx, ReachLimit); err != nil {
		t.Fatalf("the bound was never armed: %v", err)
	}
	w.clk.Advance(ReachLimit)
	select {
	case err := <-got:
		wantCause(t, err, ReachNoAnswer)
	case <-ctx.Done():
		t.Fatal("the probe did not end at its bound")
	}
}

// The caller's own cancellation is not a cause of the address: it is the context's error.
func TestReachTheCallersCancellationIsTheContexts(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team CA")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := w.reach(ctx, ca, hanging(t), w.clk)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v, want the context's", err)
	}
	if re, ok := errors.AsType[*ReachError](err); ok {
		t.Fatalf("a cancelled probe is no cause of the address: %v", re)
	}
}

// A CA that is not one, or an address that is not one, is refused before any connection.
func TestReachRefusesABadConfiguration(t *testing.T) {
	w := newTLSWorld()
	ca := w.ca(t, "team CA")
	cases := map[string]ReachConfig{
		"no CA":      {Addr: netip.MustParseAddrPort("127.0.0.1:8448"), Timers: w.clk},
		"no address": {CAPEM: string(ca.CertPEM()), Timers: w.clk},
		"no timers":  {Addr: netip.MustParseAddrPort("127.0.0.1:8448"), CAPEM: string(ca.CertPEM())},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			err := Reach(t.Context(), cfg)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error %v, want ErrInvalid", err)
			}
		})
	}
}

// An IPv6 listen address is reached in brackets and verified by its address.
func TestReachTheBaseOfAnIPv6Address(t *testing.T) {
	got := reachBase(netip.MustParseAddrPort("[2001:db8::1]:8448"))
	if got != "https://[2001:db8::1]:8448" || !strings.HasPrefix(got, schemeHTTPS) {
		t.Fatalf("base %q", got)
	}
}
