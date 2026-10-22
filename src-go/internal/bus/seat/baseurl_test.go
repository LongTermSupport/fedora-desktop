package seat

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/pycompat"
)

// baseURLCase is one base_url (raw JSON) and its refusal (check_base_url), "" for accepted.
// The member's plain_http_hosts are 192.0.2.1 and 2001:db8::1.
type baseURLCase struct{ raw, want string }

func badURL(why string) string { return refuse("base_url", why) }

var baseURLCases = []baseURLCase{
	{"1", badURL("must be a URL of printable ASCII")},
	{`""`, badURL("must be a URL of printable ASCII")},
	{js("https://bus.example.com "), badURL("must be a URL of printable ASCII")},
	{js("https://b\u00fcs.example.com"), badURL("must be a URL of printable ASCII")},
	{js("https://bus.example.com?x=1"), badURL("must have no query or fragment")},
	{js("https://bus.example.com#top"), badURL("must have no query or fragment")},
	{js("ftp://bus.example.com"), badURL("must be http:// or https://")},
	{js("bus.example.com"), badURL("must be http:// or https://")},
	{js("1https://bus.example.com"), badURL("must be http:// or https://")},
	{js("ht_tps://bus.example.com"), badURL("must be http:// or https://")},
	{js("https+x://bus.example.com"), badURL("must be http:// or https://")},
	{js("HTTPS://BUS.EXAMPLE.COM"), ""},
	{js("Https://Bus.Example.Com:8448"), ""},
	{js("https://user@bus.example.com"), badURL("must carry no credentials")},
	{js("https://user:pw@bus.example.com"), badURL("must carry no credentials")},
	{js("https://u@[2001:db8::1]"), badURL("must carry no credentials")},
	{js("https://bus.example.com/"), badURL("must have no path")},
	{js("https://bus.example.com/_matrix"), badURL("must have no path")},
	{js("https:bus.example.com"), badURL("must have no path")},
	{js("https:///bus"), badURL("must have no path")},
	{js("https://bus.example.com:"), ""},
	{js("https://bus.example.com:443"), ""},
	{js("https://bus.example.com:00443"), ""},
	{js("https://bus.example.com:0"), ""},
	{js("https://bus.example.com:65535"), ""},
	{js("https://bus.example.com:65536"), badURL("has an invalid port")},
	{js("https://bus.example.com:+1"), badURL("has an invalid port")},
	{js("https://bus.example.com:1:2"), badURL("has an invalid port")},
	{js("https://bus.example.com:443x"), badURL("has an invalid port")},
	{js("https://bus.example.com:99999999999999999999999"), badURL("has an invalid port")},
	{js("http://2001:db8::1"), badURL("has an invalid port")},
	{js("https://"), badURL("has no host")},
	{js("https:"), badURL("has no host")},
	{js("https://:443"), badURL("has no host")},
	{js("http://192.0.2.1"), ""},
	{js("http://192.0.2.1:8008"), ""},
	{js("HTTP://192.0.2.1"), ""},
	{js("http://192.0.2.2"), badURL("may use http:// only to an IP literal listed in plain_http_hosts")},
	{js("http://192.0.2.01"), badURL("may use http:// only to an IP literal listed in plain_http_hosts")},
	{js("http://bus.example.com"), badURL("may use http:// only to an IP literal listed in plain_http_hosts")},
	{js("http://[2001:db8::1]:8008"), ""},
	{js("http://[2001:DB8::1]"), ""},
	{js("http://[2001:db8:0::1]"), ""},
	{js("http://[2001:db8::2]"), badURL("may use http:// only to an IP literal listed in plain_http_hosts")},
	{js("http://[2001:db8::1%eth0]"), badURL("may use http:// only to an IP literal listed in plain_http_hosts")},
	{js("https://[2001:db8::1]"), ""},
	{js("https://[2001:db8::1]:"), ""},
	{js("https://192.0.2.9"), ""},
	{js("https://[v1.example]"), ""},
	{js("https://[vA.x]:443"), ""},
	{js("https://[v1.x_y]"), badURL("host is neither an IP literal nor a DNS name")},
	{js("https://[2001:db8::1%eth0]"), badURL("host is neither an IP literal nor a DNS name")},
	{js("https://bus_x.example.com"), badURL("host is neither an IP literal nor a DNS name")},
	{js("https://bus.example.com."), badURL("host is neither an IP literal nor a DNS name")},
	{js("https://%41"), badURL("host is neither an IP literal nor a DNS name")},
}

// crashCases are base_url values on which python's urlsplit raises ValueError, a v1 crash
// (SPEC-view B-S3): the Go refuses the whole list with a crash-class error.
var crashCases = []string{
	"https://[2001:db8::1",
	"https://2001:db8::1]",
	"https://[192.0.2.1]",
	"https://a[2001:db8::1]",
	"https://[2001:db8::1]x",
	"https://[vz.x]",
	"https://[v1.]",
	"https://[not-an-address]",
	"https://[x@2001:db8::1]",
	"https://u@[x]",
	"https://[]",
}

// urlsplit's parts and the ValueError it raises, as python's urllib.parse gives them for a
// printable ASCII URL with no query or fragment.
func TestURLSplit(t *testing.T) {
	for _, c := range []struct {
		url                         string
		scheme, netloc, path, crash string
	}{
		{url: "HTTPS://Bus.example.com", scheme: "https", netloc: "Bus.example.com"},
		{url: "https:bus", scheme: "https", path: "bus"},
		{url: "https:", scheme: "https"},
		{url: "1https://x", path: "1https://x"},
		{url: "x+y.z-1://h/p", scheme: "x+y.z-1", netloc: "h", path: "/p"},
		{url: "//h", netloc: "h"},
		{url: ":x", path: ":x"},
		{url: "https://[2001:db8::1]:8", scheme: "https", netloc: "[2001:db8::1]:8"},
		{url: "https://[2001:db8::1", crash: "Invalid IPv6 URL"},
		{url: "https://h]", crash: "Invalid IPv6 URL"},
		{url: "https://a[2001:db8::1]", crash: "Invalid IPv6 URL"},
		{url: "https://[2001:db8::1]x", crash: "Invalid IPv6 URL"},
		{url: "https://[vz.x]", crash: "IPvFuture address is invalid"},
		{url: "https://[192.0.2.1]", crash: "An IPv4 address cannot be in brackets"},
		{url: "https://[x]", crash: "'x' does not appear to be an IPv4 or IPv6 address"},
	} {
		got, err := urlsplit(c.url)
		if c.crash != "" {
			if err == nil || err.Error() != c.crash {
				t.Errorf("%s: got %+v, %v; want ValueError %q", c.url, got, err, c.crash)
			}
			continue
		}
		if err != nil || got.scheme != c.scheme || got.netloc != c.netloc || got.path != c.path {
			t.Errorf("%s: got %+v, %v", c.url, got, err)
		}
	}
}

func TestHostinfo(t *testing.T) {
	for _, c := range []struct{ netloc, host, port string }{
		{"Bus.Example.com:80", "bus.example.com", "80"},
		{"u@h:1", "h", "1"},
		{"[2001:DB8::1%Eth0]:443", "2001:db8::1%Eth0", "443"},
		{"[2001:db8::1]", "2001:db8::1", ""},
		{"h:", "h", ""},
		{"", "", ""},
	} {
		info := splitParts{netloc: c.netloc}.hostinfo()
		if host, port := info.hostname(), info.port; host != c.host || port != c.port {
			t.Errorf("%s: host %q port %q", c.netloc, host, port)
		}
	}
}

func TestCheckBaseURLCrashIsNotARefusal(t *testing.T) {
	refuse := func(key, why string) error { return refusalf("%s %s", key, why) }
	err := checkBaseURL(pycompat.Str("https://[192.0.2.1]"), nil, refuse)
	if err == nil || isRefusal(err) {
		t.Fatalf("got %v", err)
	}
	if err := checkBaseURL(pycompat.Str("https://bus.example.com"), nil, refuse); err != nil {
		t.Fatal(err)
	}
}
