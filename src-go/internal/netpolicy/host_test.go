package netpolicy

import (
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNameservers(t *testing.T) {
	text := "search example.org\nnameserver " + addr4(169, 254, 1, 1).String() + "\nnameserver 192.0.2.53\n# nameserver 198.51.100.1\nnameserver fe80::1%eth0\nnameserver junk\noptions ndots:1\n"
	got := Nameservers([]byte(text))
	want := []netip.Addr{addr4(169, 254, 1, 1), netip.MustParseAddr("192.0.2.53"), netip.MustParseAddr("fe80::1")}
	if !slices.Equal(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
}

func TestLXCAddrs(t *testing.T) {
	bridge := v4(10, 0, 3, 1, 24)
	site := addr4(10, 0, 3, 16).String()
	text := "127.0.0.1 localhost\n" + site + " site-a site-a.example.test\n" + addr4(10, 0, 4, 9).String() + " elsewhere\n# " + addr4(10, 0, 3, 99).String() + " commented\n" + site + " again\n"
	got := LXCAddrs([]byte(text), []netip.Prefix{bridge})
	if want := []netip.Addr{addr4(10, 0, 3, 16)}; !slices.Equal(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
	if got := LXCAddrs([]byte(text), nil); len(got) != 0 {
		t.Errorf("no bridge, yet %v", got)
	}
}

func TestReadHost(t *testing.T) {
	ifaces := []Iface{
		{"lo", []netip.Prefix{mustPrefix(t, "127.0.0.1/8"), mustPrefix(t, "::1/128")}},
		{"eth0", []netip.Prefix{mustPrefix(t, "192.0.2.7/24"), mustPrefix(t, "2001:db8::7/64"), mustPrefix(t, "fe80::7/64")}},
		{BusIface, []netip.Prefix{v4(10, 254, 0, 1, 32), mustPrefix(t, "fe80::9/64")}},
		{LXCBridge, []netip.Prefix{v4(10, 0, 3, 1, 24)}},
		{"wg0", []netip.Prefix{v4(169, 254, 9, 9, 16)}},
	}
	h := ReadHost(ifaces)
	if want := []netip.Addr{netip.MustParseAddr("192.0.2.7"), netip.MustParseAddr("2001:db8::7"), addr4(10, 254, 0, 1), addr4(10, 0, 3, 1)}; !slices.Equal(h.Global, want) {
		t.Errorf("global %v, want %v", h.Global, want)
	}
	if want := []netip.Addr{addr4(10, 254, 0, 1)}; !slices.Equal(h.Bus, want) {
		t.Errorf("bus %v, want %v", h.Bus, want)
	}
	if want := []netip.Prefix{v4(10, 0, 3, 0, 24)}; !slices.Equal(h.LXCPrefixes, want) {
		t.Errorf("lxc %v, want %v", h.LXCPrefixes, want)
	}
}

func TestSystemIfacesHasLoopback(t *testing.T) {
	ifaces, err := SystemIfaces()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(ifaces, func(i Iface) bool {
		return slices.ContainsFunc(i.Addrs, func(p netip.Prefix) bool { return p.Addr().IsLoopback() })
	}) {
		t.Errorf("no loopback address among %v", ifaces)
	}
}

func TestHostFile(t *testing.T) {
	got := HostFile("/home/u", "/home/u/projects/My Repo!")
	dir, name := filepath.Split(got)
	if dir != "/home/u/.config/claude-yolo/network/" {
		t.Errorf("dir %q", dir)
	}
	if !strings.HasPrefix(name, "My-Repo-") || !strings.HasSuffix(name, ".env") || len(name) != len("My-Repo-")+16+len(".env") {
		t.Errorf("name %q", name)
	}
	if HostFile("/home/u", "/a/x") == HostFile("/home/u", "/b/x") {
		t.Error("two checkouts of one name share a file")
	}
}
