package statusline

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// member.json's grammar, config._parse_member and the protocol patterns it uses (S21).
const (
	adminLocalpart = "admin"
	dnsNameMax     = 253
	maxPort        = 65535
	// intMaxDigits is the longest decimal text Python 3.11's int() reads.
	intMaxDigits = 4300
)

var (
	memberRequired = []string{"protocol", "team", "user_id", "server_name", "base_url", "plain_http_hosts", "token_file", "admin", "room"}
	memberOptional = []string{"human_text", "limits"}
	// limitBounds are limits.BOUNDS: the keys a member's limits may set, inclusive bounds.
	limitBounds = map[string][2]int64{
		"send_per_minute":        {1, 60},
		"send_burst":             {1, 30},
		"recv_per_sender_minute": {10, 600},
		"ack_timeout_s":          {60, 86400},
		"halt_ack_timeout_s":     {30, 3600},
		"human_max_age_s":        {3600, 604800},
		"wait_timeout_s":         {1, 1790},
	}
	reservedLocalparts = []string{"admin", "conduit"}

	dnsLabel         = `[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?`
	dnsNamePattern   = regexp.MustCompile(`^` + dnsLabel + `(?:\.` + dnsLabel + `)*$`)
	handlePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}\.(?:[1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11})\+` + dnsLabel + `\.(podman|lxc|docker|vm|host)$`)
	humanPattern     = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	roomIDPattern    = regexp.MustCompile(`^![A-Za-z0-9_-]{43}$`)
	printablePattern = regexp.MustCompile(`^[\x21-\x7e]+$`)
	ipvFuturePattern = regexp.MustCompile(`^v[a-fA-F0-9]+\..+$`)
)

// member is what the role read needs of a validated bundle.
type member struct {
	team, userID, serverName, memberType string
}

// refused words a refusal of one member.json key; the value is never quoted (it may be a
// pasted token).
func refused(key, why string) error { return fmt.Errorf("member.json: %s %s", key, why) }

// partition is Python's str.partition without the separator: the text before the first sep
// and after it, or all of s and "".
func partition(s, sep string) (string, string) {
	before, after, found := strings.Cut(s, sep)
	if !found {
		return s, ""
	}
	return before, after
}

// head is the text before the first sep, or all of s.
func head(s, sep string) string {
	return strings.SplitN(s, sep, 2)[0]
}

// checkMemberKeys is _parse_member's key check: no unknown key, every required one present.
func checkMemberKeys(data pyjson.Object) error {
	for key := range data {
		if !slices.Contains(memberRequired, key) && !slices.Contains(memberOptional, key) {
			return errors.New("member.json has an unknown key")
		}
	}
	for _, key := range memberRequired {
		if _, ok := data[key]; !ok {
			return refused(key, "is missing")
		}
	}
	return nil
}

// checkMemberTail is _parse_member's admin, room and token_file checks.
func checkMemberTail(data pyjson.Object, server string) error {
	if admin, isText := data["admin"].(string); !isText || admin != "@"+adminLocalpart+":"+server {
		return refused("admin", "is not @admin:<server_name>")
	}
	if room, isText := data["room"].(string); !isText || !roomIDPattern.MatchString(room) {
		return refused("room", "is not a room ID")
	}
	if file, isText := data["token_file"].(string); !isText || file != tokenFile {
		return refused("token_file", `must be "token", the file inside the bundle`)
	}
	return nil
}

// parseMember is config._parse_member over a decoded member.json (S21).
func parseMember(data pyjson.Object, team string) (member, error) {
	if err := checkMemberKeys(data); err != nil {
		return member{}, err
	}
	if n, ok := data["protocol"].(pyjson.Number); !ok || !n.IsInt(1) {
		return member{}, refused("protocol", "must be the integer 1")
	}
	if name, ok := data["team"].(string); !ok || name != team {
		return member{}, refused("team", "does not equal the bundle directory's name")
	}
	server, ok := data["server_name"].(string)
	if !ok || !isDNSName(server) {
		return member{}, refused("server_name", "is not a lowercase DNS name")
	}
	m := member{team: team, serverName: server}
	m.userID, m.memberType, ok = agentUser(data["user_id"], server)
	if !ok {
		return member{}, refused("user_id", "is not @<handle>:<server_name> for this server_name")
	}
	if err := checkMemberTail(data, server); err != nil {
		return member{}, err
	}
	return m, checkMemberRest(data)
}

// checkMemberRest is the rest of _parse_member, in its order: the plain hosts, the base URL,
// human_text and limits.
func checkMemberRest(data pyjson.Object) error {
	plain, err := plainHTTPHosts(data["plain_http_hosts"])
	if err != nil {
		return err
	}
	if err := checkBaseURL(data["base_url"], plain); err != nil {
		return err
	}
	if value, ok := data["human_text"]; ok {
		if _, isBool := value.(bool); !isBool {
			return refused("human_text", "must be true or false")
		}
	}
	if value, ok := data["limits"]; ok {
		return checkLimits(value)
	}
	return nil
}

func isDNSName(value string) bool {
	return len(value) <= dnsNameMax && dnsNamePattern.MatchString(value)
}

// parseHandle is protocol.parse_handle: the handle's type when value is an agent handle.
func parseHandle(value string) (string, bool) {
	m := handlePattern.FindStringSubmatch(value)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// userLocalpart is protocol.parse_user_id: the user ID as text and its localpart when it is
// `@<localpart>:<server>` with a handle, a reserved localpart or a human localpart, on exactly
// this server.
func userLocalpart(value any, server string) (string, string, bool) {
	text, ok := value.(string)
	if !ok || !strings.HasPrefix(text, "@") {
		return "", "", false
	}
	localpart, on, found := strings.Cut(text[1:], ":")
	if !found || on != server {
		return "", "", false
	}
	if _, isHandle := parseHandle(localpart); isHandle || slices.Contains(reservedLocalparts, localpart) || isHumanLocalpart(localpart) {
		return text, localpart, true
	}
	return "", "", false
}

func isHumanLocalpart(value string) bool {
	return humanPattern.MatchString(value) && !slices.Contains(reservedLocalparts, value)
}

// agentUser is an agent's user ID on this server, with its handle's type
// (protocol.is_agent_user_id).
func agentUser(value any, server string) (string, string, bool) {
	text, localpart, ok := userLocalpart(value, server)
	if !ok {
		return "", "", false
	}
	kind, ok := parseHandle(localpart)
	if !ok {
		return "", "", false
	}
	return text, kind, true
}

// isHumanUser is protocol.is_human_user_id.
func isHumanUser(value any, server string) bool {
	_, localpart, ok := userLocalpart(value, server)
	return ok && isHumanLocalpart(localpart)
}

// plainHTTPHosts is config._plain_http_hosts: distinct IP literals in Python's canonical form.
func plainHTTPHosts(value any) ([]string, error) {
	list, ok := value.([]any)
	if !ok {
		return nil, refused("plain_http_hosts", "must be an array of IP literals")
	}
	hosts := make([]string, 0, len(list))
	for _, entry := range list {
		text, isText := entry.(string)
		if !isText || strings.Contains(text, "%") {
			return nil, refused("plain_http_hosts", "must hold only IP literals")
		}
		canonical, isIP := pyIP(text)
		switch {
		case !isIP:
			return nil, refused("plain_http_hosts", "must hold only IP literals")
		case canonical != text:
			return nil, refused("plain_http_hosts", "must hold IP literals in canonical form")
		case slices.Contains(hosts, text):
			return nil, refused("plain_http_hosts", "lists an address twice")
		}
		hosts = append(hosts, text)
	}
	return hosts, nil
}

// checkLimits is config._limits over a present limits value (limits.parse_limits).
func checkLimits(value any) error {
	object, ok := value.(pyjson.Object)
	if !ok {
		return errors.New("member.json: limits must be an object")
	}
	for key := range object {
		if _, known := limitBounds[key]; !known {
			return errors.New("member.json: limits may hold only the known keys")
		}
	}
	for key, item := range object {
		bounds := limitBounds[key]
		n, isNumber := item.(pyjson.Number)
		v, isInt := n.Int64()
		if !isNumber || n.Kind != pyjson.Int || !isInt || v < bounds[0] || v > bounds[1] {
			return fmt.Errorf("member.json: limits.%s must be an integer from %d to %d", key, bounds[0], bounds[1])
		}
	}
	return nil
}

// checkBaseURL is config.check_base_url: https://<host>[:port], or http://<ip>[:port] with the
// IP in plain; no credentials, path, query or fragment (urllib.parse.urlsplit's reading). Where
// urlsplit raises (a bracket it cannot parse) the Python crashes; this refuses.
func checkBaseURL(value any, plain []string) error {
	text, ok := value.(string)
	if !ok || !printablePattern.MatchString(text) {
		return refused("base_url", "must be a URL of printable ASCII")
	}
	if strings.ContainsAny(text, "?#") {
		return refused("base_url", "must have no query or fragment")
	}
	scheme, rest := splitScheme(text)
	if scheme != "http" && scheme != "https" {
		return refused("base_url", "must be http:// or https://")
	}
	netloc, path := "", rest
	if strings.HasPrefix(rest, "//") {
		netloc = rest[2:]
		path = ""
		if end := strings.IndexByte(netloc, '/'); end >= 0 {
			netloc, path = netloc[:end], netloc[end:]
		}
		if err := checkBrackets(netloc); err != nil {
			return err
		}
	}
	switch {
	case strings.Contains(netloc, "@"):
		return refused("base_url", "must carry no credentials")
	case path != "":
		return refused("base_url", "must have no path")
	}
	host, port := hostInfo(netloc)
	if !validPort(port) {
		return refused("base_url", "has an invalid port")
	}
	return checkHost(scheme, lowerHost(host), plain)
}

// checkHost is check_base_url's judgement of the host: an IP listed in plain for http, an IP
// or a DNS name for https.
func checkHost(scheme, host string, plain []string) error {
	if host == "" {
		return refused("base_url", "has no host")
	}
	ip, isIP := "", false
	if !strings.Contains(host, "%") {
		ip, isIP = pyIP(host)
	}
	if scheme == "http" {
		if !isIP || !slices.Contains(plain, ip) {
			return refused("base_url", "may use http:// only to an IP literal listed in plain_http_hosts")
		}
		return nil
	}
	if !isIP && !isDNSName(host) {
		return refused("base_url", "host is neither an IP literal nor a DNS name")
	}
	return nil
}

// splitScheme is urlsplit's scheme: the text before the first ':' when it starts with an ASCII
// letter and holds only letters, digits, '+', '-' and '.', lowercased.
func splitScheme(url string) (string, string) {
	i := strings.IndexByte(url, ':')
	if i <= 0 || !isASCIILetter(url[0]) {
		return "", url
	}
	for _, c := range []byte(url[:i]) {
		if !isASCIILetter(c) && (c < '0' || c > '9') && c != '+' && c != '-' && c != '.' {
			return "", url
		}
	}
	return strings.ToLower(url[:i]), url[i+1:]
}

func isASCIILetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// checkBrackets is urlsplit's refusal of a netloc's brackets (_check_bracketed_netloc).
func checkBrackets(netloc string) error {
	open, closed := strings.Contains(netloc, "["), strings.Contains(netloc, "]")
	if !open && !closed {
		return nil
	}
	bad := refused("base_url", "has brackets that are not an IPv6 address")
	if open != closed {
		return bad
	}
	hostPort := netloc[strings.LastIndexByte(netloc, '@')+1:]
	hostname := head(hostPort, ":")
	if strings.Contains(hostPort, "[") {
		before, bracketed := partition(hostPort, "[")
		var after string
		hostname, after = partition(bracketed, "]")
		if before != "" || (after != "" && !strings.HasPrefix(after, ":")) {
			return bad
		}
	}
	if strings.HasPrefix(hostname, "v") {
		if !ipvFuturePattern.MatchString(hostname) {
			return bad
		}
		return nil
	}
	if !isScopedIPv6(hostname) {
		return bad
	}
	return nil
}

// isScopedIPv6 is whether ipaddress.ip_address gives an IPv6 address for value, a scope id
// (`%eth0`) allowed.
func isScopedIPv6(value string) bool {
	addr, scope := partition(value, "%")
	if strings.Contains(value, "%") && (scope == "" || strings.Contains(scope, "%")) {
		return false
	}
	if _, ok := parseIPv4(addr); ok {
		return false
	}
	_, ok := parseIPv6(addr)
	return ok
}

// hostInfo is SplitResult._hostinfo: the host and port text of a netloc with no credentials.
func hostInfo(netloc string) (string, string) {
	if strings.Contains(netloc, "[") {
		_, bracketed := partition(netloc, "[")
		host, after := partition(bracketed, "]")
		_, port := partition(after, ":")
		return host, port
	}
	return partition(netloc, ":")
}

// validPort is SplitResult.port not raising: empty, or ASCII digits (no more than int() reads)
// of a value up to 65535.
func validPort(port string) bool {
	if port == "" {
		return true
	}
	if len(port) > intMaxDigits {
		return false
	}
	for _, c := range []byte(port) {
		if c < '0' || c > '9' {
			return false
		}
	}
	digits := strings.TrimLeft(port, "0")
	if len(digits) > len(strconv.Itoa(maxPort)) {
		return false
	}
	n, err := strconv.Atoi("0" + digits)
	return err == nil && n <= maxPort
}

// lowerHost is SplitResult.hostname: lowercased up to a '%', whose zone is kept as written.
func lowerHost(host string) string {
	if !strings.Contains(host, "%") {
		return strings.ToLower(host)
	}
	addr, zone := partition(host, "%")
	return strings.ToLower(addr) + "%" + zone
}
