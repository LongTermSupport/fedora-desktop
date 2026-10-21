package state

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

const (
	maxMemberFileBytes = 65536
	maxTokenBytes      = 4096
	dnsNameMax         = 253
	maxPort            = 65535
	// intMaxDigits is the longest decimal text Python 3.11's int() reads.
	intMaxDigits = 4300
	tokenFile    = "token"
	keyBaseURL   = "base_url"
	keyPlain     = "plain_http_hosts"
)

var (
	memberRequired = []string{"protocol", "team", "user_id", "server_name", keyBaseURL, keyPlain, "token_file", "admin", roomKey}
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
		"report_per_hour":        {1, 60},
	}
	// dnsNamePattern is protocol's lowercase DNS name; TODO(bus/proto).
	dnsNamePattern   = regexp.MustCompile(`^` + dnsLabel + `(?:\.` + dnsLabel + `)*$`)
	ipvFuturePattern = regexp.MustCompile(`^v[a-fA-F0-9]+\..+$`)
)

// Member is a validated member.json (S18): the host's description of this member's seat.
type Member struct {
	Team           string
	UserID         string
	Handle         string
	ServerName     string
	BaseURL        string
	PlainHTTPHosts []string
	Admin          string
	Room           string
	HumanText      bool
	// Limits are the limits the bundle sets, by limits.BOUNDS key; the rest are defaults.
	Limits map[string]int64
}

func isDNSName(s string) bool { return len(s) <= dnsNameMax && dnsNamePattern.MatchString(s) }

// ReadMember reads the bundle's member.json for team, checked in v1's order, the first fault
// the error (S18, SB9). An absent file is an error wrapping fs.ErrNotExist.
func ReadMember(b Bundle, team string) (Member, error) {
	path := b.MemberPath()
	o, found, err := readObject(path, maxMemberFileBytes)
	if err != nil {
		return Member{}, err
	}
	if !found {
		return Member{}, fmt.Errorf("%s: %w", path, fs.ErrNotExist)
	}
	if err = checkKeys(path, o, memberRequired, memberOptional, unknownFirst); err != nil {
		return Member{}, err
	}
	protocol, err := anyInt(path, "protocol", field(o, "protocol"))
	if err != nil {
		return Member{}, err
	}
	if protocol != 1 {
		return Member{}, refuse(path, ClassVersion, "protocol is not 1")
	}
	m := Member{HumanText: true}
	if m.Team, err = strField(path, "team", field(o, "team"), func(s string) bool { return s == team }); err != nil {
		return Member{}, err
	}
	if m.ServerName, err = strField(path, "server_name", field(o, "server_name"), isDNSName); err != nil {
		return Member{}, err
	}
	isAgent := func(s string) bool { return isAgentUserID(s, m.ServerName) }
	if m.UserID, err = strField(path, "user_id", field(o, "user_id"), isAgent); err != nil {
		return Member{}, err
	}
	m.Handle = strings.TrimPrefix(strings.TrimSuffix(m.UserID, ":"+m.ServerName), "@")
	return m, m.readRest(path, o)
}

// readRest is the rest of v1's checks, in its order.
func (m *Member) readRest(path string, o pyjson.Ordered) error {
	var err error
	isAdmin := func(s string) bool { return s == "@admin:"+m.ServerName }
	if m.Admin, err = strField(path, "admin", field(o, "admin"), isAdmin); err != nil {
		return err
	}
	if m.Room, err = strField(path, roomKey, field(o, roomKey), isRoomID); err != nil {
		return err
	}
	if _, err = strField(path, "token_file", field(o, "token_file"), func(s string) bool { return s == tokenFile }); err != nil {
		return err
	}
	if m.PlainHTTPHosts, err = plainHTTPHosts(path, field(o, keyPlain)); err != nil {
		return err
	}
	if m.BaseURL, err = strField(path, keyBaseURL, field(o, keyBaseURL), isPrintableToken); err != nil {
		return err
	}
	if why := checkBaseURL(m.BaseURL, m.PlainHTTPHosts); why != "" {
		return refuse(path, ClassGrammar, "base_url "+why)
	}
	if v, ok := o.Get("human_text"); ok {
		if m.HumanText, err = boolField(path, "human_text", v); err != nil {
			return err
		}
	}
	if v, ok := o.Get("limits"); ok {
		m.Limits, err = readLimits(path, v)
	}
	return err
}

// plainHTTPHosts is config._plain_http_hosts: distinct IP literals in Python's canonical form.
func plainHTTPHosts(path string, v any) ([]string, error) {
	list, err := listField(path, keyPlain, v, maxMemberFileBytes)
	if err != nil {
		return nil, err
	}
	hosts := make([]string, 0, len(list))
	for _, entry := range list {
		text, err := strField(path, "a plain HTTP host", entry, anyText)
		if err != nil {
			return nil, err
		}
		canonical, isIP := "", false
		if !strings.Contains(text, "%") {
			canonical, isIP = pyIP(text)
		}
		if !isIP || canonical != text || slices.Contains(hosts, text) {
			return nil, refuse(path, ClassGrammar, "plain_http_hosts holds only distinct IP literals in canonical form")
		}
		hosts = append(hosts, text)
	}
	return hosts, nil
}

// readLimits is config._limits over a present limits value (limits.parse_limits).
func readLimits(path string, v any) (map[string]int64, error) {
	o, err := mapField(path, "limits", v, len(limitBounds))
	if err != nil {
		return nil, err
	}
	known := make([]string, 0, len(limitBounds))
	for key := range limitBounds {
		known = append(known, key)
	}
	if err = checkKeys(path, o, nil, known, unknownFirst); err != nil {
		return nil, err
	}
	limits := make(map[string]int64, len(o))
	for _, m := range o {
		n, err := anyInt(path, "a limit", m.Value)
		if err != nil {
			return nil, err
		}
		bounds := limitBounds[m.Key]
		if n < bounds[0] || n > bounds[1] {
			return nil, refuse(path, ClassGrammar, fmt.Sprintf("limits.%s is not from %d to %d", m.Key, bounds[0], bounds[1]))
		}
		limits[m.Key] = n
	}
	return limits, nil
}

// checkBaseURL is config.check_base_url, empty when url passes, else why it does not:
// https://<host>[:port], or http://<ip>[:port] with the IP in plain; no credentials, path,
// query or fragment, as urllib.parse.urlsplit reads it. Where urlsplit raises (a bracket it
// cannot parse) v1 crashes; this refuses.
func checkBaseURL(url string, plain []string) string {
	if strings.ContainsAny(url, "?#") {
		return "has a query or fragment"
	}
	scheme, rest := splitScheme(url)
	if scheme != "http" && scheme != "https" {
		return "is not http:// or https://"
	}
	netloc, path := "", rest
	if strings.HasPrefix(rest, "//") {
		netloc, path = rest[2:], ""
		if end := strings.IndexByte(netloc, '/'); end >= 0 {
			netloc, path = netloc[:end], netloc[end:]
		}
		if !bracketsOK(netloc) {
			return "has brackets that are not an IPv6 address"
		}
	}
	switch {
	case strings.Contains(netloc, "@"):
		return "carries credentials"
	case path != "":
		return "has a path"
	}
	host, port := hostInfo(netloc)
	if !validPort(port) {
		return "has an invalid port"
	}
	return checkHost(scheme, lowerHost(host), plain)
}

// checkHost is check_base_url's judgement of the host: an IP listed in plain for http, an IP
// or a DNS name for https.
func checkHost(scheme, host string, plain []string) string {
	if host == "" {
		return "has no host"
	}
	ip, isIP := "", false
	if !strings.Contains(host, "%") {
		ip, isIP = pyIP(host)
	}
	if scheme == "http" && (!isIP || !slices.Contains(plain, ip)) {
		return "may use http:// only to an IP literal listed in plain_http_hosts"
	}
	if !isIP && !isDNSName(host) {
		return "host is neither an IP literal nor a DNS name"
	}
	return ""
}

// partition is Python's str.partition without the separator.
func partition(s, sep string) (string, string) {
	before, after, found := strings.Cut(s, sep)
	if !found {
		return s, ""
	}
	return before, after
}

// head is the text before the first sep, or all of s.
func head(s, sep string) string { return strings.SplitN(s, sep, 2)[0] }

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

// bracketsOK is urlsplit not refusing a netloc's brackets (_check_bracketed_netloc).
func bracketsOK(netloc string) bool {
	open, closed := strings.Contains(netloc, "["), strings.Contains(netloc, "]")
	if !open && !closed {
		return true
	}
	if open != closed {
		return false
	}
	hostPort := netloc[strings.LastIndexByte(netloc, '@')+1:]
	hostname := head(hostPort, ":")
	if strings.Contains(hostPort, "[") {
		before, bracketed := partition(hostPort, "[")
		var after string
		hostname, after = partition(bracketed, "]")
		if before != "" || (after != "" && !strings.HasPrefix(after, ":")) {
			return false
		}
	}
	if strings.HasPrefix(hostname, "v") {
		return ipvFuturePattern.MatchString(hostname)
	}
	return isScopedIPv6(hostname)
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
	if len(port) > intMaxDigits || strings.Trim(port, "0123456789") != "" {
		return false
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

// ReadToken reads the bundle's token (S19, R43), checked on the open descriptor: a regular
// file owned by uid with no mode bit outside 0600, 1 to 4096 printable ASCII characters. It is
// re-read on every use; no refusal quotes it. An absent token wraps fs.ErrNotExist.
func ReadToken(b Bundle, uid int) (string, error) {
	path := b.TokenPath()
	file, err := openRegular(path, unix.O_RDONLY)
	if err != nil {
		return "", err
	}
	defer closeQuietly(file)
	var st unix.Stat_t
	if err = unix.Fstat(int(file.Fd()), &st); err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	if int(st.Uid) != uid || st.Mode&0o7777&^fileMode != 0 {
		return "", refuse(path, ClassFile, "not owned by this user with mode 0600")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxTokenBytes+1))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if len(raw) > maxTokenBytes {
		return "", refuse(path, ClassSize, fmt.Sprintf("larger than %d bytes", maxTokenBytes))
	}
	if !isPrintableToken(string(raw)) {
		return "", refuse(path, ClassSyntax, "not one or more printable ASCII characters")
	}
	return string(raw), nil
}

// CheckDistinctBundles refuses two bundles of one command that share a user ID or a token
// (S20); tokens[i] is members[i]'s.
func CheckDistinctBundles(members []Member, tokens []string) error {
	users := make([]string, len(members))
	for i, m := range members {
		users[i] = m.UserID
	}
	if !distinct(users) || !distinct(tokens) {
		return refuse("the bundles", ClassGrammar, "two bundles share a user ID or a token")
	}
	return nil
}

// CheckBundleLink is S21 for a team entry of a home: a directory, or a link to a real
// directory (not a link) owned by uid with mode exactly 0700.
func CheckBundleLink(entry string, uid int) error {
	info, err := os.Lstat(entry)
	if err != nil {
		return fmt.Errorf("stat %s: %w", entry, err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return nil
	}
	target, err := os.Readlink(entry)
	if err != nil {
		return fmt.Errorf("read link %s: %w", entry, err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(entry), target)
	}
	var st unix.Stat_t
	if err := unix.Lstat(target, &st); err != nil {
		return errors.Join(refuse(entry, ClassFile, "a link to nothing"), err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || int(st.Uid) != uid || st.Mode&0o7777 != dirMode {
		return refuse(entry, ClassFile, "a link that is not to this user's 0700 directory")
	}
	return nil
}
