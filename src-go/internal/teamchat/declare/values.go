package declare

import (
	"net/netip"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The list bounds of DR3 and DR4.
const (
	maxHumans    = 16
	maxListen    = 8
	maxAllowFrom = 32
	maxSeat      = 9999
	maxDNSName   = 253
)

var (
	dnsLabel      = `[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?`
	dnsNameRe     = regexp.MustCompile(`^` + dnsLabel + `(\.` + dnsLabel + `)+$`)
	dnsHostRe     = regexp.MustCompile(`^` + dnsLabel + `(\.` + dnsLabel + `)*$`)
	containerRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	urlPortDigits = regexp.MustCompile(`^[1-9][0-9]{0,4}$`)
)

// validAddress is an IP address in its canonical text, with no zone, that is not unspecified.
func validAddress(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && a.String() == s && a.Zone() == "" && !a.IsUnspecified()
}

// validPrefix is a CIDR prefix in its canonical masked text.
func validPrefix(s string) bool {
	p, err := netip.ParsePrefix(s)
	return err == nil && p.Masked().String() == s
}

// validServerName is a lower-case DNS name of at least two labels.
func validServerName(s string) bool { return len(s) <= maxDNSName && dnsNameRe.MatchString(s) }

// validURL is DR3's url: http or https, an IP or DNS host, an optional port, nothing else, in its
// canonical text.
func validURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.String() != s {
		return false
	}
	return bare(u) && validURLPort(u.Port()) && validURLHost(u.Hostname())
}

// bare is a URL of a scheme and a host alone.
func bare(u *url.URL) bool {
	return u.User == nil && u.Opaque == "" && u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == ""
}

func validURLPort(port string) bool {
	if port == "" {
		return true
	}
	n, err := strconv.Atoi(port)
	return urlPortDigits.MatchString(port) && err == nil && n <= 65535
}

func validURLHost(host string) bool {
	if a, err := netip.ParseAddr(host); err == nil {
		return a.String() == host && a.Zone() == "" && !a.IsUnspecified()
	}
	return len(host) <= maxDNSName && dnsHostRe.MatchString(host)
}

// validHandle is an agent handle that is not the probe.
func validHandle(s string) bool {
	h, ok := proto.ParseHandle(s)
	return ok && !h.IsProbe() && h.String() == s
}

func validContainer(s string) bool { return containerRe.MatchString(s) }

func validCheckout(s string) bool { return filepath.IsAbs(s) && filepath.Clean(s) == s && s != "/" }

func validSite(s string) bool {
	norm, ok := proto.NormaliseSite(s)
	return ok && norm == s
}

// trustKeys are A's forge section's keys (A 5.1); trust is that section exactly (n2b-D1).
var trustKeys = []string{"org", "repos", "exclude", "path_prefixes"}

// trust reads n as A's forge section, decoded and validated by A's own decoder (seam 16).
func (n node) trust() (proto.Forge, error) {
	if _, err := n.object([]string{"kind"}, trustKeys...); err != nil {
		return proto.Forge{}, err
	}
	f, err := proto.DecodeForge(n.v)
	if err != nil {
		return proto.Forge{}, n.fail("is not a valid forge section (" + err.Error() + ")")
	}
	return f, nil
}

// humans reads n as DR3's humans.
func (n node) humans() ([]string, error) {
	return n.strings(1, maxHumans, proto.ValidHumanName, "is not a valid human name, or is reserved")
}

func (n node) listen() ([]string, error) {
	return n.strings(1, maxListen, validAddress, "is not an IP address in canonical form, or is unspecified")
}

func (n node) allowFrom() ([]string, error) {
	return n.strings(0, maxAllowFrom, validPrefix, "is not a CIDR prefix in canonical masked form")
}

func (n node) url() (string, error) {
	return n.checked(validURL, "is not an http or https URL of a host and port alone, in canonical form")
}

func (n node) coordinator() (string, error) {
	return n.checked(validHandle, "is not an agent handle")
}
