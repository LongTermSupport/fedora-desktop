package host

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/leakcheck"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/tlsterm"
)

// filterAllows is whether the IPAddressAllow= line of a unit's drop-in lets addr in.
func filterAllows(t *testing.T, dropIn string, addr netip.Addr) bool {
	t.Helper()
	line := strings.TrimSuffix(strings.TrimPrefix(lineWith(t, dropIn, "IPAddressAllow="), "IPAddressAllow="), "\n")
	for field := range strings.FieldsSeq(line) {
		if netip.MustParsePrefix(field).Contains(addr) {
			return true
		}
	}
	return false
}

// rulesAllow is whether some recorded firewall rule accepts addr on port.
func rulesAllow(rules []Rule, addr netip.Addr, port int) bool {
	return slices.ContainsFunc(rules, func(r Rule) bool { return r.Port == port && r.Source.Contains(addr) })
}

// echoServer is a loopback server standing for the homeserver: it echoes each connection.
func echoServer(t *testing.T) netip.AddrPort {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			wg.Go(func() {
				if _, cerr := io.Copy(c, c); cerr != nil {
					t.Logf("the echo ended: %v", cerr)
				}
				if cerr := c.Close(); cerr != nil {
					t.Logf("closing the echo: %v", cerr)
				}
			})
		}
	})
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Error(err)
		}
		wg.Wait()
	})
	return netip.MustParseAddrPort(ln.Addr().String())
}

// memberReaches is a member's ping through the terminator at at: its one root is pool (the
// bundle's CA), TLS 1.3 its least version, and name the address it means to connect to.
func memberReaches(t *testing.T, pool *x509.CertPool, at netip.AddrPort, name string) error {
	t.Helper()
	cfg := &tls.Config{RootCAs: pool, ServerName: name, MinVersion: tls.VersionTLS13, Time: func() time.Time { return t0 }}
	c, err := (&tls.Dialer{Config: cfg}).DialContext(t.Context(), "tcp", at.String())
	if err != nil {
		return err
	}
	defer func() {
		if cerr := c.Close(); cerr != nil {
			t.Errorf("closing the member's connection: %v", cerr)
		}
	}()
	if _, err = c.Write([]byte("ping")); err != nil {
		return err
	}
	got := make([]byte, 4)
	if _, err = io.ReadFull(c, got); err != nil {
		return err
	}
	if string(got) != "ping" {
		return fmt.Errorf("the echo was %q", got)
	}
	return nil
}

// theTerminator is the terminator of a team's apply, listening on two loopback addresses with the
// leaf the apply made, over the homeserver at up.
func theTerminator(t *testing.T, w *world, up netip.AddrPort) *tlsterm.Server {
	t.Helper()
	leaf := theLeaf(t, w)
	srv, err := tlsterm.Start(t.Context(), tlsterm.Config{
		Listen:   []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0"), netip.MustParseAddrPort("127.0.0.2:0")},
		Upstream: up, CertPEM: leaf.CertPEM(), KeyPEM: leaf.KeyPEM(), Clock: clock.NewFake(0, t0.UnixMilli()),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := srv.Close(); cerr != nil {
			t.Error(cerr)
		}
	})
	return srv
}

// letsIn is the addresses the unit's filter and the firewall rules, as the apply wrote them, let
// in and keep out: the two must agree with each other and with the declared networks.
func letsIn(t *testing.T, w *world, port int) {
	t.Helper()
	team := ops(t)
	dropIn := w.read(paths.TLSDropInFile(team))
	rules, err := decodeFirewall([]byte(w.read(paths.FirewallRecord(team))), team)
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		addr string
		in   bool
	}{
		"the first network's own address":   {"192.0.2.10", true},
		"a host of the first network":       {"192.0.2.77", true},
		"the second network's own address":  {"203.0.113.9", true},
		"a host of the second network":      {"203.0.113.200", true},
		"a network that was never declared": {"198.51.100.7", false},
		"beside the first network":          {"192.0.3.1", false},
		"above the second network":          {"203.0.114.1", false},
		"below the second network":          {"203.0.112.255", false},
	} {
		addr := netip.MustParseAddr(c.addr)
		if got := filterAllows(t, dropIn, addr); got != c.in {
			t.Errorf("%s (%s): the unit's filter allows it = %v, want %v", name, c.addr, got, c.in)
		}
		if got := rulesAllow(rules, addr, port); got != c.in {
			t.Errorf("%s (%s): the firewall rules accept it = %v, want %v", name, c.addr, got, c.in)
		}
	}
}

// X29: on one machine with two addresses, a team that declares both networks is reached through
// each of them over TLS by a member that holds only the bundle's CA, and an address outside the
// declared networks is refused by the filter and by the firewall rules the apply recorded. The
// documentation addresses cannot be bound, so the terminator listens on two loopback addresses
// with the leaf the apply made, and the member names each derived address as the one it is
// connecting to: the handshake is the real one, and the names are the part the networks decide.
func TestX29TwoDeclaredNetworksAreServedAndAnOutsideAddressIsRefused(t *testing.T) {
	leakcheck.Check(t)
	w := newWorld(t)
	withSecondNIC(w)
	doc := teamDoc()
	docTeam(doc)["networks"] = []any{"192.0.2.0/24", "203.0.113.0/24"}
	if _, err := w.apply(approved(w, doc)); err != nil {
		t.Fatal(err)
	}
	team := ops(t)
	units, err := decodeUnits([]byte(w.read(paths.UnitsRecord(team))), team)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"192.0.2.10", "203.0.113.9"}; !slices.Equal(units.listen, want) {
		t.Fatalf("the listen addresses %v, want one in each network: %v", units.listen, want)
	}
	pool, err := pki.Pool(theCA(t, w).CertPEM())
	if err != nil {
		t.Fatal(err)
	}
	srv := theTerminator(t, w, echoServer(t))
	for i, at := range srv.Addrs() {
		if err := memberReaches(t, pool, at, units.listen[i]); err != nil {
			t.Errorf("a member could not reach %s through %s: %v", units.listen[i], at, err)
		}
	}
	if err := memberReaches(t, pool, srv.Addrs()[0], "198.51.100.7"); err == nil {
		t.Error("a member took the leaf for an address no network declared")
	}
	letsIn(t, w, units.port)
}
