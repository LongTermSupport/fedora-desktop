package host

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/leakcheck"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/tlsverb"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/tlsterm"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// filterMatches is whether some entry of the unit's directive key (IPAddressAllow= or
// IPAddressDeny=) covers addr: `any`, or a prefix. The drop-in is read alone, its base unit not
// at all (a drop-in does not lean on a deny it cannot see).
func filterMatches(t *testing.T, dropIn, key string, addr netip.Addr) bool {
	t.Helper()
	for line := range strings.SplitSeq(dropIn, "\n") {
		value, ok := strings.CutPrefix(line, key)
		if !ok {
			continue
		}
		for field := range strings.FieldsSeq(value) {
			if field == "any" {
				return true
			}
			p, err := netip.ParsePrefix(field)
			if err != nil {
				t.Fatalf("%s entry %q is neither any nor a prefix", key, field)
			}
			if p.Contains(addr) {
				return true
			}
		}
	}
	return false
}

// filterLetsIn is whether a unit's IP filter, as systemd applies it (systemd.resource-control(5)),
// lets addr in: an address an allow entry covers is let in, else one a deny entry covers is kept
// out, else it is let in. An allow-list with no deny lets everything in.
func filterLetsIn(t *testing.T, dropIn string, addr netip.Addr) bool {
	t.Helper()
	return filterMatches(t, dropIn, "IPAddressAllow=", addr) || !filterMatches(t, dropIn, "IPAddressDeny=", addr)
}

// rulesAllow is whether some recorded firewall rule accepts addr on port.
func rulesAllow(rules []Rule, addr netip.Addr, port int) bool {
	return slices.ContainsFunc(rules, func(r Rule) bool { return r.Port == port && r.Source.Contains(addr) })
}

// homeserver is the fake homeserver on 127.0.0.1 at a free port, which the team then declares as
// its port: the terminator relays to 127.0.0.1 at the team's port, as the real one does.
func homeserver(t *testing.T) (*fakehs.Server, uint16) {
	t.Helper()
	hs := fakehs.New(fakehs.Options{ServerName: "ops.example.org", Now: func() time.Time { return t0 }})
	if err := hs.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := hs.Close(); err != nil {
			t.Error(err)
		}
	})
	at := netip.MustParseAddrPort(strings.TrimPrefix(hs.URL(), "http://"))
	return hs, at.Port()
}

// unitStop is the verb's Notify, a context that ends at the unit's stop, and the stop: the
// test's end stops it too.
func unitStop(t *testing.T) (func() (context.Context, context.CancelFunc), context.CancelFunc) {
	t.Helper()
	ctx, stop := context.WithCancel(t.Context())
	t.Cleanup(stop)
	return func() (context.Context, context.CancelFunc) { return ctx, stop }, stop
}

// execStart is the drop-in's ExecStart= as the verb's parsed command line: the member binary, then
// the arguments the unit starts it with.
func execStart(t *testing.T, dropIn string) cli.Invocation {
	t.Helper()
	var argv []string
	for line := range strings.SplitSeq(dropIn, "\n") {
		if value, ok := strings.CutPrefix(line, "ExecStart="); ok {
			if argv != nil {
				t.Fatalf("the drop-in has more than one ExecStart=:\n%s", dropIn)
			}
			argv = strings.Fields(value)
		}
	}
	if len(argv) < 2 || argv[0] != paths.MemberBinaryPath {
		t.Fatalf("the drop-in's ExecStart= %q does not run %s", argv, paths.MemberBinaryPath)
	}
	inv, err := cli.Parse(argv[1:])
	if err != nil {
		t.Fatalf("the member binary refuses the unit's command line %q: %v", argv[1:], err)
	}
	return inv
}

// credentialsDir is the directory systemd gives the unit for its LoadCredential= lines: each named
// file a copy of the host file the line names, read from the world's root, at mode 0400.
func credentialsDir(t *testing.T, w *world, dropIn string) string {
	t.Helper()
	dir := t.TempDir()
	n := 0
	for line := range strings.SplitSeq(dropIn, "\n") {
		value, ok := strings.CutPrefix(line, "LoadCredential=")
		if !ok {
			continue
		}
		name, from, ok := strings.Cut(value, ":")
		if !ok || name == "" || strings.Contains(name, "/") {
			t.Fatalf("LoadCredential=%s is not a name and a path", value)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(w.read(from)), 0o400); err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n == 0 {
		t.Fatalf("the drop-in loads no credential:\n%s", dropIn)
	}
	return dir
}

// route maps an address the documentation ranges name, which no test can bind, to a loopback
// address that stands for it: the terminator binds the stand-in, and the member's dial to the
// documentation address reaches it.
type route map[netip.Addr]netip.Addr

func (r route) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	at, err := netip.ParseAddrPort(addr)
	if err != nil {
		return nil, err
	}
	stand, ok := r[at.Addr()]
	if !ok {
		return nil, fmt.Errorf("no stand-in for %s", at.Addr())
	}
	return (&net.Dialer{}).DialContext(ctx, network, netip.AddrPortFrom(stand, at.Port()).String())
}

// terminator is the verb's terminator over the stand-ins: tlsterm itself, given exactly the
// configuration the verb built, but with each listen address bound at its stand-in. It refuses a
// listen address with no stand-in, and an upstream that is not upstream, and says on started that
// it serves.
func (r route) terminator(upstream netip.AddrPort, started chan<- struct{}) func(context.Context, tlsterm.Config) error {
	return func(ctx context.Context, cfg tlsterm.Config) error {
		if cfg.Upstream != upstream {
			return fmt.Errorf("the verb's upstream is %s, want the homeserver %s", cfg.Upstream, upstream)
		}
		stood := make([]netip.AddrPort, 0, len(cfg.Listen))
		for _, a := range cfg.Listen {
			stand, ok := r[a.Addr()]
			if !ok {
				return fmt.Errorf("the verb listens on %s, which no network derived", a)
			}
			stood = append(stood, netip.AddrPortFrom(stand, a.Port()))
		}
		cfg.Listen = stood
		srv, err := tlsterm.Start(ctx, cfg)
		if err != nil {
			return err
		}
		close(started)
		<-ctx.Done()
		return srv.Close()
	}
}

// memberOf is a member's client as a bundle builds it, for base, trusting only caPEM, and dialling
// through r.
func memberOf(t *testing.T, base, caPEM, token string, r route) *matrix.Client {
	t.Helper()
	c, err := matrix.NewMember(matrix.Config{
		BaseURL: base, CAPEM: caPEM, Token: token, Wall: clock.NewFake(0, t0.UnixMilli()),
		Clock: clock.NewFake(0, t0.UnixMilli()), Dial: r.dial,
	})
	if err != nil {
		t.Fatalf("a member client for %s: %v", base, err)
	}
	t.Cleanup(c.Close)
	return c
}

// letsIn is the addresses the unit's filter and the firewall rules, as the apply wrote them, let
// in and keep out: the two must agree with each other and with the declared networks. The filter
// is read as systemd applies it, so the deny half is what keeps an outside address out; and the
// unit's socket bind is its port alone, with all others denied.
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
		if got := filterLetsIn(t, dropIn, addr); got != c.in {
			t.Errorf("%s (%s): the unit's filter lets it in = %v, want %v", name, c.addr, got, c.in)
		}
		if got := rulesAllow(rules, addr, port); got != c.in {
			t.Errorf("%s (%s): the firewall rules accept it = %v, want %v", name, c.addr, got, c.in)
		}
	}
	wantBind := "\nSocketBindDeny=any\nSocketBindAllow=tcp:" + strconv.Itoa(port) + "\n"
	if !strings.Contains(dropIn, wantBind) || strings.Count(dropIn, "SocketBind") != 2 {
		t.Errorf("the unit's socket bind is not%s alone:\n%s", strings.ReplaceAll(wantBind, "\n", " "), dropIn)
	}
}

// X29: on one machine with two addresses, a team that declares both networks is reached through
// each of them over TLS by a member that holds only the bundle's CA, and an address outside the
// declared networks is refused by the filter and by the firewall rules the apply recorded.
//
// What runs is what the unit runs: the verb `teamchat tls` (tlsverb.Serve) is given the command
// line parsed from the rendered ExecStart= and a credentials directory made from the rendered
// LoadCredential= lines, and the terminator it configures is tlsterm itself over the fake
// homeserver at 127.0.0.1 and the team's port. The member is matrix.NewMember with the CA the
// admin issues bundles with (CAPEM) and the url's form, https to a derived address and the team's
// port, and it makes an authenticated call. Its limit: the documentation addresses cannot be
// bound, so the terminator binds a loopback stand-in for each derived listen address and the
// member's dial to that address reaches the stand-in; the names the leaf carries, the name the
// member checks, the port, the credentials and the trust are the real ones. systemd itself (the
// filter, the socket bind, the credentials' copy) is not run: the drop-in's text is read as
// systemd would apply it.
func TestX29TwoDeclaredNetworksAreServedAndAnOutsideAddressIsRefused(t *testing.T) {
	leakcheck.Check(t)
	hs, port := homeserver(t)
	portText := strconv.Itoa(int(port))
	w := newWorld(t)
	withSecondNIC(w)
	doc := teamDoc()
	docTeam(doc)["networks"] = []any{"192.0.2.0/24", "203.0.113.0/24"}
	docTeam(doc)["port"] = int(port)
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
	dropIn := w.read(paths.TLSDropInFile(team))
	r := route{
		netip.MustParseAddr("192.0.2.10"):   netip.MustParseAddr("127.0.0.2"),
		netip.MustParseAddr("203.0.113.9"):  netip.MustParseAddr("127.0.0.3"),
		netip.MustParseAddr("198.51.100.7"): netip.MustParseAddr("127.0.0.2"),
	}
	creds := credentialsDir(t, w, dropIn)
	var stderr bytes.Buffer
	e := cli.Env{Stderr: &stderr, Getenv: func(k string) string {
		if k == paths.EnvCredentials {
			return creds
		}
		return ""
	}}
	inv := execStart(t, dropIn)
	notify, stop := unitStop(t)
	started := make(chan struct{})
	done := make(chan term.ExitCode, 1)
	go func() {
		done <- tlsverb.Serve(e, inv, tlsverb.Deps{
			Run:    r.terminator(netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), port), started),
			Clock:  clock.NewFake(0, t0.UnixMilli()),
			Notify: notify,
		})
	}()
	select {
	case <-started:
	case code := <-done:
		t.Fatalf("the verb ended before serving, exit %d: %s", code, stderr.String())
	}
	caPEM, err := CAPEM(w.root, team)
	if err != nil {
		t.Fatal(err)
	}
	token, err := hs.MintToken(hs.AddUser("alice", "", false))
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range units.listen {
		base := "https://" + addr + ":" + portText
		who, werr := memberOf(t, base, caPEM, token, r).Whoami(t.Context())
		if werr != nil || who.UserID != "@alice:ops.example.org" {
			t.Errorf("a member could not reach the homeserver at %s: %+v, %v", base, who, werr)
		}
	}
	outside := "https://198.51.100.7:" + portText
	if _, werr := memberOf(t, outside, caPEM, token, r).Whoami(t.Context()); werr == nil {
		t.Errorf("a member took the leaf for %s, an address no network declared", outside)
	}
	stop()
	if code := <-done; code != term.ExitOK {
		t.Errorf("the verb's stop exited %d: %s", code, stderr.String())
	}
	if units.port != int(port) {
		t.Errorf("the units record the port %d, want the team's %d", units.port, port)
	}
	letsIn(t, w, units.port)
}
