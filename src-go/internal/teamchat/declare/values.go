package declare

import (
	"errors"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The list bounds of DR3 and DR4, and X1's networks.
const (
	maxHumans   = 16
	maxNetworks = 32
	maxDNSName  = 253
)

var (
	dnsLabel      = `[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?`
	dnsNameRe     = regexp.MustCompile(`^` + dnsLabel + `(\.` + dnsLabel + `)+$`)
	dnsHostRe     = regexp.MustCompile(`^` + dnsLabel + `(\.` + dnsLabel + `)*$`)
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

// URLProblem is what makes a team's url one no member may use to reach the team on port, or ""
// when none does: matrix's rule for a member's base URL, the one rule for it (X12: plain http
// only to a loopback IP literal), and the port the url reaches, its explicit port or its scheme's
// default, the team's own. `declare check` and `team apply` both judge a url by it.
func URLProblem(raw string, port int) string {
	_, err := matrix.CanonicalMemberBase(raw)
	if me, ok := errors.AsType[*matrix.Error](err); ok {
		return me.Detail
	}
	if err != nil {
		return err.Error()
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err.Error()
	}
	reaches := u.Port()
	if reaches == "" {
		reaches = map[string]string{"https": "443", "http": "80"}[u.Scheme]
	}
	if reaches != strconv.Itoa(port) {
		return "reaches port " + reaches + ", not the team's port " + strconv.Itoa(port)
	}
	return ""
}

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

func validContainer(s string) bool {
	_, err := paths.ParseContainerName(s)
	return err == nil
}

// msgCheckout is a checkout's refusal: paths.ValidGuestPath's grammar.
const msgCheckout = "is not an absolute, clean path below / with no control or format character"

func validSeat(n int64) bool { return n >= 1 && n <= paths.MaxLXCSeat }

// seat reads a seat number, 1-paths.MaxLXCSeat (DR4).
func (n node) seat() (int, error) {
	v, err := n.integer()
	if err != nil {
		return 0, err
	}
	if !validSeat(v) {
		return 0, n.fail("is not a seat number, 1-" + strconv.Itoa(paths.MaxLXCSeat))
	}
	return int(v), nil
}

func validSite(s string) bool {
	norm, ok := proto.NormaliseSite(s)
	return ok && norm == s
}

// trustKeys are A's forge section's keys (A 5.1); trust is that section exactly (n2b-D1).
var trustKeys = []string{"org", "repos", "exclude", "path_prefixes"}

// trust reads n as A's forge section, decoded and validated by A's own decoder (seam 16): the
// input decoder, which folds the GitHub names a person wrote as GitHub shows them.
func (n node) trust() (proto.Forge, error) {
	if _, err := n.object([]string{"kind"}, trustKeys...); err != nil {
		return proto.Forge{}, err
	}
	f, err := proto.DecodeForgeInput(n.v)
	if err != nil {
		return proto.Forge{}, n.fail("is not a valid forge section (" + err.Error() + ")")
	}
	return f, nil
}

// humans reads n as DR3's humans.
func (n node) humans() ([]string, error) {
	return n.strings(1, maxHumans, proto.ValidHumanName, "is not a valid human name, or is reserved")
}

// networks reads n as X1's networks: 0 to 32 CIDRs, each in canonical masked form.
func (n node) networks() ([]string, error) {
	return n.strings(0, maxNetworks, validPrefix, "is not a CIDR prefix in canonical masked form")
}

func (n node) url() (string, error) {
	return n.checked(validURL, "is not an http or https URL of a host and port alone, in canonical form")
}

func (n node) coordinator() (string, error) {
	return n.checked(validHandle, "is not an agent handle")
}
