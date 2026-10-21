package seat

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/pycompat"
)

// checkBaseURL is config.check_base_url (part of member.json's decoder, which moves to
// internal/bus/state with it): https://<host>[:port], or http://<ip>[:port] with <ip> in
// plainHosts; no credentials, path, query or fragment. Where python's urlsplit raises
// ValueError, v1 crashed; that is a plain error here, never a refusal (SPEC-view B-S3).
func checkBaseURL(v pycompat.Value, plainHosts []string, refuse func(key, why string) error) error {
	s, ok := v.(pycompat.Str)
	if !ok || !printableRE.MatchString(string(s)) {
		return refuse("base_url", "must be a URL of printable ASCII")
	}
	url := string(s)
	if strings.ContainsAny(url, "?#") {
		return refuse("base_url", "must have no query or fragment")
	}
	parts, err := urlsplit(url)
	if err != nil {
		return errors.New("base_url: python's urlsplit raises ValueError (" + err.Error() + "), which v1 did not catch")
	}
	if why := shapeFault(parts); why != "" {
		return refuse("base_url", why)
	}
	host := parts.hostinfo().hostname()
	ip := ipLiteral(host)
	if parts.scheme == "http" {
		if ip == "" || !slices.Contains(plainHosts, ip) {
			return refuse("base_url", "may use http:// only to an IP literal listed in plain_http_hosts")
		}
		return nil
	}
	if ip == "" && !isDNSName(host) {
		return refuse("base_url", "host is neither an IP literal nor a DNS name")
	}
	return nil
}

// shapeFault is the first of check_base_url's refusals of the split URL's parts, in v1's
// order, before its host is judged; "" when there is none.
func shapeFault(parts splitParts) string {
	info := parts.hostinfo()
	switch {
	case parts.scheme != "http" && parts.scheme != "https":
		return "must be http:// or https://"
	case strings.Contains(parts.netloc, "@"):
		return "must carry no credentials"
	case parts.path != "":
		return "must have no path"
	case !validPort(info.port):
		return "has an invalid port"
	case info.hostname() == "":
		return "has no host"
	}
	return ""
}

// valueError is a ValueError python's urllib.parse or ipaddress raised, in its own words.
type valueError string

func (e valueError) Error() string { return string(e) }

const errInvalidIPv6 = valueError("Invalid IPv6 URL")

// splitParts is what urlsplit returns, for a URL that has no query or fragment.
type splitParts struct{ scheme, netloc, path string }

const schemeChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+-."

// urlsplit is urllib.parse.urlsplit for printable ASCII with no '?' or '#' (so its stripping
// of control characters and its query and fragment splits have nothing to do).
func urlsplit(url string) (splitParts, error) {
	var parts splitParts
	rest := url
	if i := strings.IndexByte(url, ':'); i > 0 && isASCIILetter(url[0]) && strings.Trim(url[:i], schemeChars) == "" {
		parts.scheme, rest = strings.ToLower(url[:i]), url[i+1:]
	}
	if strings.HasPrefix(rest, "//") {
		end := len(rest)
		if i := strings.IndexByte(rest[2:], '/'); i >= 0 {
			end = i + 2
		}
		parts.netloc, rest = rest[2:end], rest[end:]
		if err := checkBrackets(parts.netloc); err != nil {
			return splitParts{}, err
		}
	}
	parts.path = rest
	return parts, nil
}

// checkBrackets is urlsplit's test of a netloc's brackets: both or neither, and a bracketed
// netloc as _check_bracketed_netloc wants it.
func checkBrackets(netloc string) error {
	open, closing := strings.Contains(netloc, "["), strings.Contains(netloc, "]")
	switch {
	case open != closing:
		return errInvalidIPv6
	case open:
		return checkBracketedNetloc(netloc)
	default:
		return nil
	}
}

func isASCIILetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// hostPort is a netloc's host and port text ("" for None).
type hostPort struct{ host, port string }

// hostinfo is SplitResult._hostinfo: the host and port after any userinfo, a bracketed host
// without its brackets.
func (p splitParts) hostinfo() hostPort {
	info := rpartition(p.netloc, "@").after
	if open := partition(info, "["); open.found {
		bracketed := partition(open.after, "]")
		return hostPort{bracketed.before, partition(bracketed.after, ":").after}
	}
	plain := partition(info, ":")
	return hostPort{plain.before, plain.after}
}

// hostname is SplitResult.hostname ("" for None): lower-cased up to a '%' zone, kept after.
func (h hostPort) hostname() string {
	zone := partition(h.host, "%")
	if zone.found {
		return strings.ToLower(zone.before) + "%" + zone.after
	}
	return strings.ToLower(h.host)
}

// validPort is SplitResult.port not raising: none, or ASCII digits worth 0 to 65535.
func validPort(port string) bool {
	if port == "" {
		return true
	}
	if strings.Trim(port, "0123456789") != "" {
		return false
	}
	digits := strings.TrimLeft(port, "0")
	return len(digits) < 5 || (len(digits) == 5 && digits <= "65535")
}

var ipvFutureRE = regexp.MustCompile(`^v[a-fA-F0-9]+\..+$`)

// checkBracketedNetloc is urllib.parse._check_bracketed_netloc and _check_bracketed_host.
func checkBracketedNetloc(netloc string) error {
	hostAndPort := rpartition(netloc, "@").after
	host := partition(hostAndPort, ":").before
	if open := partition(hostAndPort, "["); open.found {
		if open.before != "" {
			return errInvalidIPv6
		}
		bracketed := partition(open.after, "]")
		if bracketed.after != "" && !strings.HasPrefix(bracketed.after, ":") {
			return errInvalidIPv6
		}
		host = bracketed.before
	}
	if strings.HasPrefix(host, "v") {
		if !ipvFutureRE.MatchString(host) {
			return valueError("IPvFuture address is invalid")
		}
		return nil
	}
	canonical, ok := pycompat.IPAddress(host)
	if !ok {
		return valueError(pycompat.PyRepr(pycompat.Str(host)) + " does not appear to be an IPv4 or IPv6 address")
	}
	if !strings.Contains(canonical, ":") {
		return valueError("An IPv4 address cannot be in brackets")
	}
	return nil
}

// ipLiteral is config._ip_literal: the canonical form of an IP address without a zone, ""
// for anything else.
func ipLiteral(host string) string {
	if strings.Contains(host, "%") {
		return ""
	}
	canonical, ok := pycompat.IPAddress(host)
	if !ok {
		return ""
	}
	return canonical
}

// parted is python's str.partition / rpartition result.
type parted struct {
	before, after string
	found         bool
}

func partition(s, sep string) parted {
	before, after, found := strings.Cut(s, sep)
	return parted{before, after, found}
}

func rpartition(s, sep string) parted {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return parted{after: s}
	}
	return parted{s[:i], s[i+len(sep):], true}
}
