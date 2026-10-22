package hostnet

import (
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
)

const tcpHeader = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"

// tcpLine is one /proc/net/tcp line in the kernel's format.
func tcpLine(slot int, local, state string, inode string) string {
	return "   " + strconv.Itoa(slot) + ": " + local + " 00000000:0000 " + state +
		" 00000000:00000000 00:00000000 00000000     0        0 " + inode + " 1 0000000000000000 100 0 0 10 0\n"
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// process makes /proc/<pid> holding the given fd links and cgroup file ("" for no cgroup file).
func process(t *testing.T, proc, pid, cgroup string, links ...string) {
	t.Helper()
	fds := filepath.Join(proc, pid, "fd")
	if err := os.MkdirAll(fds, 0o750); err != nil {
		t.Fatal(err)
	}
	for i, target := range links {
		if err := os.Symlink(target, filepath.Join(fds, strconv.Itoa(i))); err != nil {
			t.Fatal(err)
		}
	}
	if cgroup != "" {
		writeFile(t, filepath.Join(proc, pid, "cgroup"), cgroup)
	}
}

func addrPort(s string) netip.AddrPort { return netip.MustParseAddrPort(s) }

func TestListenersAttributeEachSocketToItsUnit(t *testing.T) {
	proc := t.TempDir()
	writeFile(t, filepath.Join(proc, "net/tcp"), tcpHeader+
		tcpLine(0, "0100007F:2100", "0A", "1001")+ // 127.0.0.1:8448
		tcpLine(1, "00000000:0016", "0A", "1002")+ // 0.0.0.0:22
		tcpLine(2, "0A0200C0:2100", "01", "1004")+ // established: not a listener
		tcpLine(3, "0A0200C0:1F90", "0A", "1005")) // 192.0.2.10:8080, nobody found
	writeFile(t, filepath.Join(proc, "net/tcp6"), tcpHeader+
		tcpLine(0, "00000000000000000000000001000000:2100", "0A", "1003")+ // [::1]:8448
		tcpLine(1, "00000000000000000000000000000000:0016", "0A", "1006")) // [::]:22
	process(t, proc, "100", "0::/system.slice/teamchat-server@alpha.service\n", "socket:[1001]", "/dev/null")
	process(t, proc, "200", "0::/system.slice/sshd.service\n", "pipe:[77]", "socket:[1002]", "socket:[1006]")
	process(t, proc, "300", "0::/user.slice/user-1000.slice/session-3.scope\n", "socket:[1003]")
	process(t, proc, "400", "", "socket:[1005]")                           // ended: no cgroup file
	if err := os.MkdirAll(filepath.Join(proc, "500"), 0o750); err != nil { // ended: no fd directory
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(proc, "self"), "not a process directory")
	if err := os.MkdirAll(filepath.Join(proc, "sys"), 0o750); err != nil {
		t.Fatal(err)
	}

	got, err := Network{Proc: proc}.Listeners()
	if err != nil {
		t.Fatal(err)
	}
	want := []host.Listener{
		{Addr: addrPort("127.0.0.1:8448"), Unit: "teamchat-server@alpha.service"},
		{Addr: addrPort("0.0.0.0:22"), Unit: "sshd.service"},
		{Addr: addrPort("192.0.2.10:8080"), Unit: ""},
		{Addr: addrPort("[::1]:8448"), Unit: ""},
		{Addr: addrPort("[::]:22"), Unit: "sshd.service"},
	}
	if len(got) != len(want) {
		t.Fatalf("listeners = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("listener %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestListenersWithoutIPv6IsNotAnError(t *testing.T) {
	proc := t.TempDir()
	writeFile(t, filepath.Join(proc, "net/tcp"), tcpHeader+tcpLine(0, "0100007F:2100", "0A", "1001"))
	got, err := Network{Proc: proc}.Listeners()
	if err != nil || len(got) != 1 {
		t.Fatalf("Listeners = %v, %v", got, err)
	}
}

func TestListenersRefuseWhatTheyCannotRead(t *testing.T) {
	good := tcpHeader + tcpLine(0, "0100007F:2100", "0A", "1001")
	cases := map[string]struct{ tcp, tcp6 string }{
		"no tcp table":         {"", ""},
		"too few fields":       {tcpHeader + "   0: 0100007F:2100 00000000:0000 0A\n", ""},
		"address not hex":      {tcpHeader + tcpLine(0, "0100007G:2100", "0A", "1001"), ""},
		"port not hex":         {tcpHeader + tcpLine(0, "0100007F:21ZZ", "0A", "1001"), ""},
		"no colon":             {tcpHeader + tcpLine(0, "0100007F2100", "0A", "1001"), ""},
		"short ipv4 address":   {tcpHeader + tcpLine(0, "01007F:2100", "0A", "1001"), ""},
		"state not hex":        {tcpHeader + tcpLine(0, "0100007F:2100", "XX", "1001"), ""},
		"inode not a number":   {tcpHeader + tcpLine(0, "0100007F:2100", "0A", "abc"), ""},
		"bad line in tcp6":     {good, tcpHeader + tcpLine(0, "00000000000000000000000001000000:2100", "0A", "x")},
		"short ipv6 address":   {good, tcpHeader + tcpLine(0, "0000000100000000:2100", "0A", "1")},
		"ipv6 address not hex": {good, tcpHeader + tcpLine(0, "0000000000000000000000000100000G:2100", "0A", "1")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			proc := t.TempDir()
			if c.tcp != "" {
				writeFile(t, filepath.Join(proc, "net/tcp"), c.tcp)
			}
			if c.tcp6 != "" {
				writeFile(t, filepath.Join(proc, "net/tcp6"), c.tcp6)
			}
			if _, err := (Network{Proc: proc}).Listeners(); err == nil {
				t.Fatal("Listeners succeeded")
			}
		})
	}
}

func TestListenersRefuseATableThatIsALink(t *testing.T) {
	proc := t.TempDir()
	writeFile(t, filepath.Join(proc, "elsewhere"), tcpHeader)
	if err := os.MkdirAll(filepath.Join(proc, "net"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(proc, "elsewhere"), filepath.Join(proc, "net/tcp")); err != nil {
		t.Fatal(err)
	}
	if _, err := (Network{Proc: proc}).Listeners(); err == nil {
		t.Fatal("Listeners read a link")
	}
}

func TestCgroupUnit(t *testing.T) {
	cases := map[string]string{
		"0::/system.slice/teamchat-server@alpha.service\n": "teamchat-server@alpha.service",
		"0::/user.slice/user-1.slice/session-3.scope\n":    "",
		"0::/\n": "",
		"12:cpu:/legacy\n0::/system.slice/chronyd.service\n": "chronyd.service",
		"12:cpu:/legacy.service\n":                           "",
		"":                                                   "",
		// n2f-D11: only systemd makes cgroups under /system.slice/ through *.slice elements.
		"0::/system.slice/system-teamchat\\x2dserver.slice/teamchat-server@alpha.service\n":          "teamchat-server@alpha.service",
		"0::/user.slice/user-1000.slice/user@1000.service/app.slice/teamchat-server@alpha.service\n": "",
		"0::/system.slice/foo.service/x.slice/teamchat-server@alpha.service\n":                       "",
		"0::/system.slice/teamchat-server@alpha.service/child\n":                                     "",
		"0::/system.slice/system-teamchat\\x2dserver.slice/teamchat-server@alpha.service/child\n":    "",
		"0::/machine.slice/teamchat-server@alpha.service\n":                                          "",
		"0::/lxc.payload.c1/system.slice/teamchat-server@alpha.service\n":                            "",
		"0::/lxc.payload.c1/teamchat-server@alpha.service\n":                                         "",
		"0::/teamchat-server@alpha.service\n":                                                        "",
		"0::/system.slice//teamchat-server@alpha.service\n":                                          "",
		"0::/system.slice/.slice/teamchat-server@alpha.service\n":                                    "",
		"0::/system.slice/.service\n":                                                                "",
		"0::system.slice/teamchat-server@alpha.service\n":                                            "",
		"0::/system.slice/teamchat-server@alpha.service":                                             "teamchat-server@alpha.service",
	}
	for in, want := range cases {
		if got := cgroupUnit([]byte(in)); got != want {
			t.Errorf("cgroupUnit(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListenersNeedAProcWithAFdDirectoryItCannotRead(t *testing.T) {
	proc := t.TempDir()
	writeFile(t, filepath.Join(proc, "net/tcp"), tcpHeader+tcpLine(0, "0100007F:2100", "0A", "1001"))
	writeFile(t, filepath.Join(proc, "100/fd"), "a file where the directory should be")
	_, err := Network{Proc: proc}.Listeners()
	if err == nil {
		t.Fatal("Listeners succeeded over a broken fd directory")
	}
}

func TestListenersFailWhenProcIsUnreadable(t *testing.T) {
	proc := t.TempDir()
	writeFile(t, filepath.Join(proc, "net/tcp"), tcpHeader+tcpLine(0, "0100007F:2100", "0A", "1001"))
	other := filepath.Join(t.TempDir(), "gone")
	n := Network{Proc: proc}
	n.Proc = other
	if _, err := n.Listeners(); err == nil {
		t.Fatal("Listeners succeeded without a proc")
	}
}

func fakeLinks(links ...link) func() ([]link, error) {
	return func() ([]link, error) { return links, nil }
}

func prefixes(s ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(s))
	for _, p := range s {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}

func TestInterfacesAreNamesAndAddressesWithoutPrefixes(t *testing.T) {
	n := Network{links: fakeLinks(
		link{Name: "lo", Prefixes: prefixes("127.0.0.1/8", "::1/128")},
		link{Name: "eth0", Prefixes: prefixes("192.0.2.10/24")},
		link{Name: "down"},
	)}
	got, err := n.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	want := []host.Interface{
		{Name: "lo", Addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("::1")}},
		{Name: "eth0", Addrs: []netip.Addr{netip.MustParseAddr("192.0.2.10")}},
		{Name: "down"},
	}
	if len(got) != len(want) {
		t.Fatalf("Interfaces = %v", got)
	}
	for i := range want {
		if got[i].Name != want[i].Name || len(got[i].Addrs) != len(want[i].Addrs) {
			t.Fatalf("interface %d = %v, want %v", i, got[i], want[i])
		}
		for j := range want[i].Addrs {
			if got[i].Addrs[j] != want[i].Addrs[j] {
				t.Errorf("interface %d address %d = %v", i, j, got[i].Addrs[j])
			}
		}
	}
}

func TestInterfacesFailWhenTheKernelCannotSay(t *testing.T) {
	boom := errors.New("boom")
	n := Network{links: func() ([]link, error) { return nil, boom }}
	if _, err := n.Interfaces(); !errors.Is(err, boom) {
		t.Fatalf("Interfaces error = %v", err)
	}
	if _, err := n.Bridge(); !errors.Is(err, boom) {
		t.Fatalf("Bridge error = %v", err)
	}
}

func TestBridgeIsTheFirstIPv4PrefixMasked(t *testing.T) {
	n := Network{BridgeName: "lxcbr0", links: fakeLinks(
		link{Name: "eth0", Prefixes: prefixes("192.0.2.10/24")},
		link{Name: "lxcbr0", Prefixes: prefixes("fe80::1/64", "198.51.100.1/24", "203.0.113.1/24")},
	)}
	got, err := n.Bridge()
	if err != nil {
		t.Fatal(err)
	}
	if want := netip.MustParsePrefix("198.51.100.0/24"); got != want {
		t.Fatalf("Bridge = %v, want %v", got, want)
	}
}

func TestBridgeNamesWhatIsMissing(t *testing.T) {
	cases := map[string][]link{
		"no such interface": {{Name: "eth0", Prefixes: prefixes("192.0.2.10/24")}},
		"no IPv4 address":   {{Name: "lxcbr0", Prefixes: prefixes("fe80::1/64")}},
	}
	for name, links := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Network{BridgeName: "lxcbr0", links: fakeLinks(links...)}.Bridge()
			if err == nil || !strings.Contains(err.Error(), "lxcbr0") {
				t.Fatalf("Bridge error = %v, want one naming lxcbr0", err)
			}
		})
	}
}

func TestSystemLinksReadTheRealInterfaces(t *testing.T) {
	links, err := systemLinks()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range links {
		if l.Name == "" {
			t.Fatalf("a link has no name: %v", links)
		}
		for _, p := range l.Prefixes {
			if !p.IsValid() || p.Addr().Zone() != "" || p.Addr().Is4In6() {
				t.Errorf("%s has prefix %v", l.Name, p)
			}
		}
	}
	got, err := Network{}.Interfaces()
	if err != nil || len(got) != len(links) {
		t.Fatalf("Interfaces = %v, %v", got, err)
	}
}

func TestPrefixOfRefusesWhatIsNotAPrefix(t *testing.T) {
	ok := []struct {
		want string
		addr net.Addr
	}{
		{"192.0.2.10/24", &net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.CIDRMask(24, 32)}},
		{"192.0.2.10/24", &net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.CIDRMask(120, 128)}},
		{"192.0.2.10/32", &net.IPAddr{IP: net.ParseIP("192.0.2.10")}},
		{"fe80::1/64", &net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)}},
	}
	for _, c := range ok {
		got, err := prefixOf(c.addr)
		if err != nil || got != netip.MustParsePrefix(c.want) {
			t.Errorf("prefixOf(%v) = %v, %v, want %s", c.addr, got, err, c.want)
		}
	}
	bad := map[string]net.Addr{
		"unix socket":       &net.UnixAddr{Name: "x", Net: "unix"},
		"no ip":             &net.IPNet{Mask: net.CIDRMask(24, 32)},
		"mask not a prefix": &net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.IPMask{255, 0, 255, 0}},
		"mask too short":    &net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.CIDRMask(8, 128)},
	}
	for name, addr := range bad {
		if _, err := prefixOf(addr); err == nil {
			t.Errorf("%s: prefixOf succeeded", name)
		}
	}
}
