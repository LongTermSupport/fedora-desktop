package ctl

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Plan 00257 ADMIN-1: the child sends the admin token, the registration key and the admin
// password only on a connection the team's own homeserver accepted.

// fakeConn is a connection with only its two addresses.
type fakeConn struct {
	net.Conn
	local, remote netip.AddrPort
}

func (c fakeConn) LocalAddr() net.Addr  { return net.TCPAddrFromAddrPort(c.local) }
func (c fakeConn) RemoteAddr() net.Addr { return net.TCPAddrFromAddrPort(c.remote) }

// procTree is a /proc under a temporary root: net/tcp and a process per entry.
type procTree struct {
	t    *testing.T
	root string
}

func newProc(t *testing.T) procTree {
	t.Helper()
	p := procTree{t: t, root: t.TempDir()}
	p.write("net/tcp", "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n")
	return p
}

func (p procTree) write(name, text string) {
	p.t.Helper()
	path := filepath.Join(p.root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		p.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		p.t.Fatal(err)
	}
}

// socket adds a TCP row: local and remote as the kernel writes them, owned by uid.
func (p procTree) socket(local, remote netip.AddrPort, uid string) {
	p.t.Helper()
	raw, err := os.ReadFile(filepath.Join(p.root, "net/tcp"))
	if err != nil {
		p.t.Fatal(err)
	}
	row := "   1: " + tcpAddr(local) + " " + tcpAddr(remote) + " 01 00000000:00000000 00:00000000 00000000 " + uid + "        0 4242 1 0000000000000000 20 4 30 10 -1\n"
	p.write("net/tcp", string(raw)+row)
}

// process adds a process in cgroup path with effective uid euid.
func (p procTree) process(pid, cgroup, euid string) {
	p.write(pid+"/cgroup", "0::"+cgroup+"\n")
	p.write(pid+"/status", "Name:\ttuwunel\nUid:\t"+euid+"\t"+euid+"\t"+euid+"\t"+euid+"\n")
}

var (
	hsAddr     = netip.MustParseAddrPort("127.0.0.1:8448")
	clientAddr = netip.MustParseAddrPort("127.0.0.1:51000")
	unitCgroup = "/system.slice/system-teamchat\\x2dserver.slice/" + paths.ServerUnit(mustTeamID("alpha"))
)

func mustTeamID(s string) paths.TeamID {
	id, err := paths.ParseTeamID(s)
	if err != nil {
		panic(err)
	}
	return id
}

// sleeps counts the waits of a check, and never waits.
type sleeps struct{ n int }

func (s *sleeps) sleep(ctx context.Context, _ time.Duration) error {
	s.n++
	return ctx.Err()
}

func (p procTree) check(s *sleeps) error {
	return ProcPeer{Proc: p.root, Sleep: s.sleep}.Check(p.t.Context(), mustTeamID("alpha"), fakeConn{local: clientAddr, remote: hsAddr})
}

func TestADMIN1TheTeamsHomeserverIsAccepted(t *testing.T) {
	p := newProc(t)
	p.socket(hsAddr, clientAddr, "61200")
	p.process("1", "/init.scope", "0")
	p.process("812", unitCgroup, "61200")
	if err := p.check(&sleeps{}); err != nil {
		t.Fatal(err)
	}
}

// A listener of another uid, any uid the unit does not run as, is refused, naming the unit.
func TestADMIN1ASquatterIsRefused(t *testing.T) {
	for _, c := range []struct {
		name  string
		build func(p procTree)
	}{
		{"an unprivileged process", func(p procTree) { p.socket(hsAddr, clientAddr, "1000"); p.process("812", unitCgroup, "61200") }},
		{"the unit is not running", func(p procTree) { p.socket(hsAddr, clientAddr, "61200") }},
		{"the uid of another team's unit", func(p procTree) {
			p.socket(hsAddr, clientAddr, "61300")
			p.process("9", strings.Replace(unitCgroup, "@alpha", "@beta", 1), "61300")
		}},
		{"a process outside the unit", func(p procTree) {
			p.socket(hsAddr, clientAddr, "61200")
			p.process("7", "/user.slice/x.scope", "61200")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := newProc(t)
			c.build(p)
			err := p.check(&sleeps{})
			if err == nil || !strings.Contains(err.Error(), paths.ServerUnit(mustTeamID("alpha"))) {
				t.Errorf("got %v", err)
			}
		})
	}
}

// A socket not listed yet is waited for, a bounded number of times, then refused.
func TestADMIN1AnUnlistedSocketIsWaitedForThenRefused(t *testing.T) {
	p := newProc(t)
	p.socket(hsAddr, netip.MustParseAddrPort("127.0.0.1:51001"), "61200")
	p.process("812", unitCgroup, "61200")
	s := &sleeps{}
	err := p.check(s)
	if err == nil || s.n != peerTries-1 || !strings.Contains(err.Error(), "net/tcp") {
		t.Errorf("after %d waits: %v", s.n, err)
	}
}

// The table's address form is the kernel's: four bytes as one host-order word, the port in hex.
func TestADMIN1TheTableAddressForm(t *testing.T) {
	if got := tcpAddr(netip.MustParseAddrPort("127.0.0.1:8448")); got != "0100007F:2100" {
		t.Errorf("got %s", got)
	}
}

// Open dials the homeserver only through the check: a refusal fails the open before the
// registration key, the password or a token is sent, and nothing reaches the server.
func TestADMIN1OpenSendsNothingToARefusedListener(t *testing.T) {
	w := newWorld(t)
	var checked int
	w.cfg.Peer = func(context.Context, paths.TeamID, net.Conn) error {
		checked++
		return errors.New("the listener is not the team's")
	}
	if _, err := w.open(w.key()); err == nil || !strings.Contains(err.Error(), "the listener is not the team's") {
		t.Fatalf("Open: %v", err)
	}
	if checked == 0 {
		t.Fatal("Open reached the homeserver without the check")
	}
	if _, err := os.Stat(filepath.Join(w.dir, tokenName)); err == nil {
		t.Error("a token was stored through a refused listener")
	}
}

// The production check refuses a server in this test process: it is no team's unit.
func TestADMIN1TheDefaultCheckRefusesAServerOutsideTheUnit(t *testing.T) {
	w := newWorld(t)
	cfg := Config{Root: w.cfg.Root}
	if _, err := cfg.Opener()(t.Context(), w.team, w.port, w.key()); err == nil ||
		!strings.Contains(err.Error(), paths.ServerUnit(w.team)) {
		t.Fatalf("Open with the default check: %v", err)
	}
}
