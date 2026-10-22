package pycompat

import (
	"strings"
	"testing"
)

var ipCases = []string{
	"192.0.2.1", "0.0.0.0", "255.255.255.255", "256.1.1.1", "01.2.3.4", "1.2.3", "1.2.3.4.5", "1.2.3.04",
	"1.2.3.0", "1..2.3", "", " 1.2.3.4", "1.2.3.4 ", "\U00000661.2.3.4", "1.2.3.4/32", "1.2.3.-4", "+1.2.3.4",
	"0x1.2.3.4", "1.2.3.4\n", "1.2.3.4:80", "1234.1.1.1", "::", "::1", "1::", "1::2", "2001:db8::1",
	"2001:DB8:0:0:0:0:0:1", "2001:db8:0:0:1:0:0:1", "2001:0:0:1:0:0:0:1", "0:0:0:0:0:0:0:0",
	"1:0:0:0:0:0:0:0", "0:0:0:0:0:0:0:1", "1:2:3:4:5:6:7:8", "1:2:3:4:5:6:7:8:9", "1:2:3:4:5:6:7::",
	"::2:3:4:5:6:7:8", "1::3:4:5:6:7:8", ":1::", "1:::2", "1::2::3", ":", ":::", "1:2", "12345::", "g::",
	"::192.0.2.1", "64:ff9b::192.0.2.1", "::1.2.3", "::1.2.3.04", "1:2:3:4:5:6:1.2.3.4",
	"1:2:3:4:5:6:7:1.2.3.4", "fe80::1%eth0", "fe80::1%", "fe80::1%a%b", "fe80::1%25", "1.2.3.4%eth0",
	"fe80::1%eth0/64", "fe80::1%\U000000e9 x", "::%x", "0::0", "0000:0000::0001", "00000::1",
	"2001:db8:0:1:1:1:1:1", "2001:db8::1:0:0:1", "1:0:0:2:0:0:0:3", "::fffe:1.2.3.4", "[::1]",
	"1:2:3:4:5:6:7:8%", "::1.2.3.4.5", "1:2:3:4:5:6:7:8:", ":1:2:3:4:5:6:7:8", "1::2:3:4:5:6:7:8",
	"0:0:1:0:0:1:0:0", "ABCD:EF01::", "::\U00000661", "1:2:3:4:5::1.2.3.4", "::ffff:1.2.3.4.5",
	"::ffff:0:0", "0:0:0:0:0:ffff:7f00:1", "::ffff:192.0.2.1", "::ffff:c000:201", "::ffff:0.0.0.0",
	"::ffff:1.2.3.4%x", "::FFFF:255.255.255.255",
}

// isMapped is an answer python 3.13 and later writes dotted (B-S5), 3.11 as hex groups.
func isMapped(s string) bool { return strings.HasPrefix(s, "::ffff:") && strings.Contains(s, ".") }

func TestPythonIPAddress(t *testing.T) {
	reqs := make([]pyReq, 0, len(ipCases))
	for _, c := range ipCases {
		reqs = append(reqs, pyReq{"ip", []any{cps(c)}})
	}
	out := python(t, reqs)
	v := pythonVersion(t)
	dotted := v[0] > 3 || v[1] >= 13
	mapped := 0
	for i, c := range ipCases {
		got, ok := IPAddress(c)
		if out[i].failed() {
			if ok {
				t.Errorf("ip_address(%q): python %s, Go %q", c, pyErrText(out[i]), got)
			}
			continue
		}
		if !ok {
			t.Errorf("ip_address(%q): python %s, Go refused", c, out[i].OK)
			continue
		}
		if isMapped(got) && !dotted {
			mapped++
			continue
		}
		if !sameJSON(t, got, out[i].OK) {
			t.Errorf("str(ip_address(%q)) = %q, python %s", c, got, out[i].OK)
		}
	}
	if !dotted && mapped == 0 {
		t.Fatal("no IPv4-mapped case was set aside: the cases lost their mapped addresses")
	}
	t.Logf("%d IPv4-mapped answers set aside (python %d.%d writes them as hex); TestIPv4MappedIsDotted holds them",
		mapped, v[0], v[1])
}

// TestIPv4MappedIsDotted is B-S5: the host's python (3.13 and later) writes them dotted.
func TestIPv4MappedIsDotted(t *testing.T) {
	cases := map[string]string{
		"::ffff:192.0.2.1":       "::ffff:192.0.2.1",
		"::ffff:c000:201":        "::ffff:192.0.2.1",
		"::FFFF:255.255.255.255": "::ffff:255.255.255.255",
		"::ffff:0:0":             "::ffff:0.0.0.0",
		"0:0:0:0:0:ffff:7f00:1":  "::ffff:127.0.0.1",
		"::ffff:1.2.3.4%x":       "::ffff:1.2.3.4%x",
		"::fffe:1.2.3.4":         "::fffe:102:304",
		"::1:ffff:1.2.3.4":       "::1:ffff:102:304",
	}
	for in, want := range cases {
		if got, ok := IPAddress(in); !ok || got != want {
			t.Errorf("IPAddress(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
}
