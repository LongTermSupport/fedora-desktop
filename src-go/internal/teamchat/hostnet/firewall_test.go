package hostnet

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
)

const routeHeader = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"

// route4 is one /proc/net/route line; dest and mask are the kernel's little-endian hex.
func route4(dev, dest, flags, metric, mask string) string {
	return dev + "\t" + dest + "\t00000000\t" + flags + "\t0\t0\t" + metric + "\t" + mask + "\t0\t0\t0\n"
}

// route6 is one /proc/net/ipv6_route line.
func route6(dest, plen, metric, flags, dev string) string {
	return dest + " " + plen + " 00000000000000000000000000000000 00 00000000000000000000000000000000 " +
		metric + " 00000001 00000000 " + flags + " " + dev + "\n"
}

const (
	dest192   = "000200C0" // 192.0.2.0
	mask24    = "00FFFFFF"
	mask0     = "00000000"
	dest198   = "006433C6" // 198.51.100.0
	dest2001  = "20010db8000000000000000000000000"
	destLocal = "fe800000000000000000000000000000"
)

type firewallWorld struct {
	fw   Firewall
	fake *execx.Fake
}

func newFirewall(t *testing.T, route, route6 string) firewallWorld {
	t.Helper()
	proc := t.TempDir()
	if route != "" {
		writeFile(t, filepath.Join(proc, "net/route"), route)
	}
	if route6 != "" {
		writeFile(t, filepath.Join(proc, "net/ipv6_route"), route6)
	}
	fake := &execx.Fake{}
	return firewallWorld{fw: Firewall{Runner: fake, Proc: proc}, fake: fake}
}

func zoneOf(w firewallWorld, dev string, code int, out string) {
	w.fake.On([]string{fwPath, "--get-zone-of-interface=" + dev}, execx.Result{ExitCode: code, Stdout: []byte(out)})
}

func defaultZone(w firewallWorld, code int, out string) {
	w.fake.On([]string{fwPath, "--get-default-zone"}, execx.Result{ExitCode: code, Stdout: []byte(out)})
}

func TestZoneIsTheZoneOfTheInterfaceThatRoutesTheSource(t *testing.T) {
	routes := routeHeader +
		route4("eth0", mask0, "0003", "100", mask0) +
		route4("eth1", dest192, "0001", "100", mask24)
	w := newFirewall(t, routes, "")
	zoneOf(w, "eth1", 0, "trusted\n")
	got, err := w.fw.Zone(context.Background(), netip.MustParsePrefix("192.0.2.0/24"))
	if err != nil || got != "trusted" {
		t.Fatalf("Zone = %q, %v", got, err)
	}
	if len(w.fake.Calls()) != 1 {
		t.Fatalf("calls = %v", w.fake.Calls())
	}
	checkProgramEnv(t, w.fake.Calls())
}

// fwPath is firewall-cmd by its absolute path: root never finds a program on a PATH it inherited.
const fwPath = "/usr/bin/firewall-cmd"

// checkProgramEnv holds every firewall-cmd run to an environment of its own.
func checkProgramEnv(t *testing.T, calls []execx.Cmd) {
	t.Helper()
	for _, c := range calls {
		if !c.CleanEnv || !slices.Equal(c.Env, []string{"PATH=/usr/bin:/bin", "LANG=C"}) {
			t.Errorf("%s ran with CleanEnv %v and Env %q", c.Line(), c.CleanEnv, c.Env)
		}
	}
}

func TestZoneTakesTheLongestPrefixThenTheLowestMetric(t *testing.T) {
	routes := routeHeader +
		route4("wide", mask0, "0003", "1", mask0) +
		route4("dear", dest192, "0001", "300", mask24) +
		route4("cheap", dest192, "0001", "50", mask24) +
		route4("down", dest192, "0000", "1", mask24) +
		route4("rejected", dest192, "0201", "1", mask24) +
		route4("other", dest198, "0001", "1", mask24)
	w := newFirewall(t, routes, "")
	zoneOf(w, "cheap", 0, "work\n")
	got, err := w.fw.Zone(context.Background(), netip.MustParsePrefix("192.0.2.0/24"))
	if err != nil || got != "work" {
		t.Fatalf("Zone = %q, %v", got, err)
	}
}

func TestZoneOfAnInterfaceWithNoZoneIsTheDefaultZone(t *testing.T) {
	w := newFirewall(t, routeHeader+route4("eth1", dest192, "0001", "100", mask24), "")
	zoneOf(w, "eth1", 2, "")
	defaultZone(w, 0, "public\n")
	got, err := w.fw.Zone(context.Background(), netip.MustParsePrefix("192.0.2.0/24"))
	if err != nil || got != "public" {
		t.Fatalf("Zone = %q, %v", got, err)
	}
}

func TestZoneWithNoRouteIsTheDefaultZone(t *testing.T) {
	w := newFirewall(t, routeHeader+route4("eth1", dest198, "0001", "100", mask24), "")
	defaultZone(w, 0, "public\n")
	got, err := w.fw.Zone(context.Background(), netip.MustParsePrefix("192.0.2.0/24"))
	if err != nil || got != "public" {
		t.Fatalf("Zone = %q, %v", got, err)
	}
	if w.fake.Called(fwPath + " --get-zone-of-interface") {
		t.Fatal("asked for an interface's zone without a route")
	}
}

func TestZoneOfAnIPv6Source(t *testing.T) {
	routes6 := route6(dest2001, "20", "00000400", "00000001", "eth2") +
		route6(destLocal, "40", "00000100", "00000001", "eth3") +
		route6(strings.Repeat("0", 32), "00", "00000100", "00000001", "eth0") +
		route6(strings.Repeat("0", 32), "00", "00000001", "00000201", "lo")
	w := newFirewall(t, routeHeader, routes6)
	zoneOf(w, "eth2", 0, "internal\n")
	got, err := w.fw.Zone(context.Background(), netip.MustParsePrefix("2001:db8::/32"))
	if err != nil || got != "internal" {
		t.Fatalf("Zone = %q, %v", got, err)
	}
}

func TestZoneOfAnIPv6SourceWithNoIPv6IsTheDefaultZone(t *testing.T) {
	w := newFirewall(t, routeHeader, "")
	defaultZone(w, 0, "public\n")
	got, err := w.fw.Zone(context.Background(), netip.MustParsePrefix("2001:db8::/32"))
	if err != nil || got != "public" {
		t.Fatalf("Zone = %q, %v", got, err)
	}
}

func TestZoneFailsOnWhatItCannotRead(t *testing.T) {
	src := netip.MustParsePrefix("192.0.2.0/24")
	cases := map[string]struct{ route, route6 string }{
		"no ipv4 table":       {"", ""},
		"too few fields":      {routeHeader + "eth1\t000200C0\t0\n", ""},
		"destination not hex": {routeHeader + route4("eth1", "zz0200C0", "0001", "1", mask24), ""},
		"mask not hex":        {routeHeader + route4("eth1", dest192, "0001", "1", "zzFFFFFF"), ""},
		"flags not hex":       {routeHeader + route4("eth1", dest192, "zz", "1", mask24), ""},
		"metric not a number": {routeHeader + route4("eth1", dest192, "0001", "x", mask24), ""},
		"mask not contiguous": {routeHeader + route4("eth1", dest192, "0001", "1", "FF00FF00"), ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newFirewall(t, c.route, c.route6)
			zoneOf(w, "eth1", 0, "public\n")
			if _, err := w.fw.Zone(context.Background(), src); err == nil {
				t.Fatal("Zone succeeded")
			}
		})
	}
}

func TestZoneFailsOnABadIPv6Table(t *testing.T) {
	cases := map[string]string{
		"too few fields":      "fe80 40\n",
		"destination not hex": route6("zz800000000000000000000000000000", "40", "00000100", "00000001", "eth3"),
		"destination short":   route6("fe80", "40", "00000100", "00000001", "eth3"),
		"length not hex":      route6(dest2001, "zz", "00000100", "00000001", "eth3"),
		"length too long":     route6(dest2001, "81", "00000100", "00000001", "eth3"),
		"metric not hex":      route6(dest2001, "20", "zz", "00000001", "eth3"),
		"flags not hex":       route6(dest2001, "20", "00000100", "zz", "eth3"),
	}
	for name, table := range cases {
		t.Run(name, func(t *testing.T) {
			w := newFirewall(t, routeHeader, table)
			if _, err := w.fw.Zone(context.Background(), netip.MustParsePrefix("2001:db8::/32")); err == nil {
				t.Fatal("Zone succeeded")
			}
		})
	}
}

func TestZoneErrorsFromFirewalld(t *testing.T) {
	routes := routeHeader + route4("eth1", dest192, "0001", "100", mask24)
	src := netip.MustParsePrefix("192.0.2.0/24")
	cases := map[string]struct {
		setup func(w firewallWorld)
		want  string
	}{
		"exit 1 on the interface": {func(w firewallWorld) { zoneOf(w, "eth1", 1, "") }, "exited 1"},
		"exit 252":                {func(w firewallWorld) { zoneOf(w, "eth1", 252, "") }, "exited 252"},
		"zone with a space":       {func(w firewallWorld) { zoneOf(w, "eth1", 0, "my zone\n") }, "zone name"},
		"empty zone":              {func(w firewallWorld) { zoneOf(w, "eth1", 0, "\n") }, "zone name"},
		"overlong zone":           {func(w firewallWorld) { zoneOf(w, "eth1", 0, strings.Repeat("a", 33)+"\n") }, "zone name"},
		"not installed": {func(w firewallWorld) {
			w.fake.OnFunc([]string{fwPath}, func(execx.Cmd) (execx.Result, error) {
				return execx.Result{}, &execx.NotInstalledError{Name: "firewall-cmd"}
			})
		}, "firewall-cmd"},
		"default zone fails": {func(w firewallWorld) { zoneOf(w, "eth1", 2, ""); defaultZone(w, 3, "") }, "exited 3"},
		"default zone bad":   {func(w firewallWorld) { zoneOf(w, "eth1", 2, ""); defaultZone(w, 0, "a b\n") }, "zone name"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newFirewall(t, routes, "")
			c.setup(w)
			_, err := w.fw.Zone(context.Background(), src)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Zone error = %v, want one containing %q", err, c.want)
			}
		})
	}
}

var rule = host.Rule{Zone: "public", Source: netip.MustParsePrefix("192.0.2.0/24"), Port: 8448}

func TestQueryReadsTheExitStatus(t *testing.T) {
	cases := []struct {
		code      int
		permanent bool
		present   bool
		fails     bool
		argv      string
	}{
		{0, false, true, false, fwPath + " --zone=public --query-rich-rule=" + rule.Rich()},
		{1, true, false, false, fwPath + " --permanent --zone=public --query-rich-rule=" + rule.Rich()},
		{2, false, false, true, ""},
		{252, true, false, true, ""},
	}
	for _, c := range cases {
		fake := &execx.Fake{}
		fake.On([]string{fwPath}, execx.Result{ExitCode: c.code})
		got, err := Firewall{Runner: fake}.Query(context.Background(), c.permanent, rule)
		if c.fails {
			if err == nil {
				t.Errorf("code %d: Query succeeded", c.code)
			}
			continue
		}
		if err != nil || got != c.present {
			t.Errorf("code %d: Query = %v, %v", c.code, got, err)
		}
		if line := fake.Calls()[0].Line(); line != c.argv {
			t.Errorf("code %d: argv = %q, want %q", c.code, line, c.argv)
		}
		args := fake.Calls()[0].Args
		if last := args[len(args)-1]; last != "--query-rich-rule="+rule.Rich() {
			t.Errorf("rule argument = %q", last)
		}
		checkProgramEnv(t, fake.Calls())
	}
}

func TestAddAndRemoveRunTheirFlagWithTheRuleAsOneArgument(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		fake := &execx.Fake{}
		fake.On([]string{fwPath}, execx.Result{})
		fw := Firewall{Runner: fake}
		if err := fw.Add(context.Background(), permanent, rule); err != nil {
			t.Fatal(err)
		}
		if err := fw.Remove(context.Background(), permanent, rule); err != nil {
			t.Fatal(err)
		}
		prefix := ""
		if permanent {
			prefix = "--permanent "
		}
		calls := fake.Calls()
		if got, want := calls[0].Line(), fwPath+" "+prefix+"--zone=public --add-rich-rule="+rule.Rich(); got != want {
			t.Errorf("add argv = %q, want %q", got, want)
		}
		if got, want := calls[1].Line(), fwPath+" "+prefix+"--zone=public --remove-rich-rule="+rule.Rich(); got != want {
			t.Errorf("remove argv = %q, want %q", got, want)
		}
		checkProgramEnv(t, calls)
	}
}

func TestAddAndRemoveNameAFailure(t *testing.T) {
	fake := &execx.Fake{}
	fake.On([]string{fwPath}, execx.Result{ExitCode: 12, Stderr: []byte("INVALID_RULE")})
	fw := Firewall{Runner: fake}
	for name, err := range map[string]error{
		"add":    fw.Add(context.Background(), false, rule),
		"remove": fw.Remove(context.Background(), true, rule),
	} {
		var exit *execx.ExitError
		if !errors.As(err, &exit) || exit.Code != 12 || !strings.Contains(err.Error(), name) {
			t.Errorf("%s error = %v", name, err)
		}
	}
}

func TestARuleInAnInvalidZoneIsRefusedBeforeFirewallCmdRuns(t *testing.T) {
	fake := &execx.Fake{}
	fw := Firewall{Runner: fake}
	bad := rule
	bad.Zone = "pub lic"
	if _, err := fw.Query(context.Background(), false, bad); err == nil {
		t.Error("Query accepted the zone")
	}
	if err := fw.Add(context.Background(), false, bad); err == nil {
		t.Error("Add accepted the zone")
	}
	if err := fw.Remove(context.Background(), false, bad); err == nil {
		t.Error("Remove accepted the zone")
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("firewall-cmd ran: %v", fake.Calls())
	}
}

func TestAFirewallCmdThatCannotStartFailsTheQuery(t *testing.T) {
	fake := &execx.Fake{}
	fake.OnFunc([]string{fwPath}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{}, &execx.NotInstalledError{Name: "firewall-cmd"}
	})
	if _, err := (Firewall{Runner: fake}).Query(context.Background(), false, rule); !execx.IsNotInstalled(err) {
		t.Fatalf("Query error = %v", err)
	}
}
