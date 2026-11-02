// Package guestfacts parses what `lxc apply` reads of an LXC guest and its container before it
// changes anything (Plan 00209 SPEC-n3.md LX4, DESIGN-v2-C-host.md 3.9.2 step 03): the guest's
// passwd and group, the checkout's .git/config and ccy.env.local, the container settings
// lxc-info prints, the guest's source address and a /proc stat's start time. It parses bytes
// and never opens a path; the caller reads them through the guest root (TB1).
//
// Every input but the host's own passwd is the guest's, so each is held to a closed grammar
// (TB2) and every refusal wraps ErrRefused and names the file and the rule, never a guest
// byte (TB2.4).
package guestfacts

import (
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The caps of the guest files, and the site of an entry that names none.
const (
	MaxPasswd   = 1 << 20  // /etc/passwd and /etc/group
	MaxCheckout = 64 << 10 // the checkout's .git/config and ccy.env.local
	MaxStat     = 4096     // a /proc/<pid>/stat
	DefaultSite = "local"
)

// ErrRefused is wrapped by every refusal: a running guest's facts are wrong, which fails that
// member (n3b-D1), and the owner fixes the guest.
var ErrRefused = errors.New("refused")

// ErrNoAccount is a passwd with no account for the user, a refusal of its own: the absent phase
// skips a leaver's guest steps for it, since no file is reachable as that user (C 3.9.2).
var ErrNoAccount = errors.New("no account for the user")

func refuse(file, format string, args ...any) error {
	return fmt.Errorf("%w: %s: %s", ErrRefused, file, fmt.Sprintf(format, args...))
}

// Account is a guest user's passwd entry, as the unit and the guest layout take it.
type Account struct {
	User     paths.GuestUser
	UID, GID int
	Home     string
}

const (
	guestUsersFile = "the guest's /etc/passwd"
	hostUsersFile  = "the host passwd"
	guestGroupFile = "the guest's /etc/group"
	maxID          = 1<<32 - 2 // (uid_t)-1 is no id
)

var idGrammar = regexp.MustCompile(`^(0|[1-9][0-9]{0,9})$`)

// lines splits a passwd-form file into its entries of n colon-separated fields; blank lines
// and # comments are skipped, and any other form refuses the file.
func lines(data []byte, file string, n int) ([][]string, error) {
	if len(data) > MaxPasswd {
		return nil, refuse(file, "larger than %d bytes", MaxPasswd)
	}
	if err := text(data, file, false); err != nil {
		return nil, err
	}
	var entries [][]string
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) != n {
			return nil, refuse(file, "a line is not %d colon-separated fields", n)
		}
		entries = append(entries, fields)
	}
	return entries, nil
}

// text refuses bytes that are not UTF-8 or hold a control character but a newline (and a tab
// where tabs are allowed).
func text(data []byte, file string, tabs bool) error {
	if !utf8.Valid(data) {
		return refuse(file, "not UTF-8")
	}
	for _, r := range string(data) {
		if (r < 0x20 && r != '\n' && (r != '\t' || !tabs)) || r == 0x7f {
			return refuse(file, "holds a control character")
		}
	}
	return nil
}

func parseID(s, file, what string) (int, error) {
	n, err := strconv.Atoi(s)
	if !idGrammar.MatchString(s) || err != nil || n > maxID {
		return 0, refuse(file, "a %s is not a decimal id", what)
	}
	return n, nil
}

// Passwd finds u in a guest passwd file: uid and gid nonzero, home absolute and clean in the
// unit's grammar, the name exactly u and on one line only, and its uid no other account's
// (which would own the agent's files as well). hostUIDs is every uid the host's own
// passwd names (n3-D29); a uid among them is refused naming K15. A nil set checks none (the
// L3 probe, after the apply enforced it).
func Passwd(data []byte, u paths.GuestUser, hostUIDs map[int]bool) (Account, error) {
	entries, err := lines(data, guestUsersFile, 7)
	if err != nil {
		return Account{}, err
	}
	var found *Account
	uids := map[int]int{}
	for _, f := range entries {
		uid, err := parseID(f[2], guestUsersFile, "uid")
		if err != nil {
			return Account{}, err
		}
		gid, err := parseID(f[3], guestUsersFile, "gid")
		if err != nil {
			return Account{}, err
		}
		uids[uid]++
		if f[0] != u.String() {
			continue
		}
		if found != nil {
			return Account{}, refuse(guestUsersFile, "the user is named twice")
		}
		found = &Account{User: u, UID: uid, GID: gid, Home: f[5]}
	}
	if found == nil {
		return Account{}, fmt.Errorf("%w: %s: %w", ErrRefused, guestUsersFile, ErrNoAccount)
	}
	if err := checkAccount(*found, hostUIDs); err != nil {
		return Account{}, err
	}
	if uids[found.UID] > 1 {
		return Account{}, refuse(guestUsersFile, "another account shares the user's uid")
	}
	return *found, nil
}

func checkAccount(a Account, hostUIDs map[int]bool) error {
	switch {
	case a.UID == 0:
		return refuse(guestUsersFile, "the user's uid is 0")
	case a.GID == 0:
		return refuse(guestUsersFile, "the user's gid is 0")
	case hostUIDs[a.UID]:
		return refuse(guestUsersFile, "the user's uid is a host account's (K15): a host account sharing it could reach the agent's files")
	case !paths.ValidUnitPath(a.Home):
		return refuse(guestUsersFile, "the user's home is not an absolute clean path of [A-Za-z0-9/._-] other than /")
	default:
		return nil
	}
}

// HostUIDs is the uid set of the host's own passwd (read by state.ReadFile), for K15.
func HostUIDs(data []byte) (map[int]bool, error) {
	entries, err := lines(data, hostUsersFile, 7)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, refuse(hostUsersFile, "names no account")
	}
	uids := map[int]bool{}
	for _, f := range entries {
		uid, err := parseID(f[2], hostUsersFile, "uid")
		if err != nil {
			return nil, err
		}
		uids[uid] = true
	}
	return uids, nil
}

// GroupName is the name of gid in a guest group file, for the unit's Group=: the first line
// with gid, whose name is a portable user name that no earlier line gives another gid.
func GroupName(data []byte, gid int) (string, error) {
	entries, err := lines(data, guestGroupFile, 4)
	if err != nil {
		return "", err
	}
	first := map[string]int{}
	for _, f := range entries {
		n, err := parseID(f[2], guestGroupFile, "gid")
		if err != nil {
			return "", err
		}
		if _, seen := first[f[0]]; !seen {
			first[f[0]] = n
		}
		if n != gid {
			continue
		}
		if _, err := paths.ParseGuestUser(f[0]); err != nil || first[f[0]] != gid {
			return "", refuse(guestGroupFile, "the group's name is not a portable name that resolves to its gid")
		}
		return f[0], nil
	}
	return "", refuse(guestGroupFile, "no group has the user's gid")
}

// The container settings the apply reads (n3-D22), the keys lxccmd.Config is asked for.
const (
	KeyIdmap     = "lxc.idmap"
	KeyLink      = "lxc.net.0.link"
	KeyStartAuto = "lxc.start.auto"
)

// ConfigKeys are the keys ParseConfig takes, each exactly once.
var ConfigKeys = []string{KeyIdmap, KeyLink, KeyStartAuto}

// Config is the container settings the apply reads (n3-D22): the map is lxccmd.Config's.
type Config struct {
	Link      string
	StartAuto bool
}

var ifname = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,14}$`)

const containerConfig = "the container's config"

// ParseConfig reads the container's settings. An lxc.idmap is refused by name: the container's
// uids must be the kernel's, which K15 and every guest owner check rely on. A miss names the
// setting; the container's own play owns its config, never the apply.
func ParseConfig(kv map[string]string) (Config, error) {
	for key := range kv {
		if !slices.Contains(ConfigKeys, key) {
			return Config{}, refuse(containerConfig, "%s is not a setting the apply reads", strconv.Quote(key))
		}
	}
	for _, key := range ConfigKeys {
		if _, ok := kv[key]; !ok {
			return Config{}, refuse(containerConfig, "%s was not read", key)
		}
	}
	if kv[KeyIdmap] != "" {
		return Config{}, refuse(containerConfig, "%s is set; a member's container maps no uids", KeyIdmap)
	}
	if !ifname.MatchString(kv[KeyLink]) {
		return Config{}, refuse(containerConfig, "%s is not an interface name", KeyLink)
	}
	var auto bool
	switch kv[KeyStartAuto] {
	case "1":
		auto = true
	case "0", "":
	default:
		return Config{}, refuse(containerConfig, "%s is not 0 or 1", KeyStartAuto)
	}
	return Config{Link: kv[KeyLink], StartAuto: auto}, nil
}

const guestSource = "the guest's address"

// Source checks that one of the guest's addresses is inside a prefix of the team's
// allow_from, on a link that is the host bridge.
func Source(addrs []netip.Addr, allowFrom []string, link, bridge string) error {
	if link != bridge {
		return refuse(guestSource, "the container's link is not the host bridge %s", bridge)
	}
	if len(addrs) == 0 {
		return refuse(guestSource, "the container has no address")
	}
	prefixes := make([]netip.Prefix, 0, len(allowFrom))
	for _, s := range allowFrom {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return refuse(guestSource, "the team's allow_from holds a prefix that does not parse")
		}
		prefixes = append(prefixes, p)
	}
	for _, a := range addrs {
		for _, p := range prefixes {
			if p.Contains(a.Unmap()) {
				return nil
			}
		}
	}
	return refuse(guestSource, "no address of the container is inside the team's allow_from")
}

const procStat = "a /proc stat"

// statStart is field 22's index among the fields after the command's closing parenthesis,
// which start at field 3.
const statStart = 22 - 3

var ticksGrammar = regexp.MustCompile(`^[0-9]{1,19}$`)

// StartTicks is field 22 of a /proc/<pid>/stat, the process's start time in clock ticks, read
// after the last ')' so a command name holding one cannot shift it.
func StartTicks(stat []byte) (int64, error) {
	if len(stat) > MaxStat {
		return 0, refuse(procStat, "larger than %d bytes", MaxStat)
	}
	s := strings.TrimSuffix(string(stat), "\n")
	i := strings.LastIndexByte(s, ')')
	if i < 0 || !strings.HasPrefix(s[i+1:], " ") {
		return 0, refuse(procStat, "no command in parentheses")
	}
	fields := strings.Split(s[i+2:], " ")
	if len(fields) <= statStart {
		return 0, refuse(procStat, "fewer than 22 fields")
	}
	n, err := strconv.ParseInt(fields[statStart], 10, 64)
	if !ticksGrammar.MatchString(fields[statStart]) || err != nil {
		return 0, refuse(procStat, "the start time is not a decimal")
	}
	return n, nil
}

// Prefix is the handle prefix of an LXC entry's first join (C 3.7.4, 3.9.2 step 03),
// `<repo>+<site>.lxc`: repo from gitConfig's origin, else the checkout's base name; site from
// the entry, else envLocal's HOOKS_DAEMON_HOSTNAME, else local. A nil envLocal is no file;
// a nil gitConfig is refused, since a checkout has one.
func Prefix(m declare.LXCMember, gitConfig, envLocal []byte) (proto.Prefix, error) {
	if gitConfig == nil {
		return proto.Prefix{}, refuse(gitFile, "the checkout has none")
	}
	origin, hasOrigin, err := originURL(gitConfig)
	if err != nil {
		return proto.Prefix{}, err
	}
	repo, err := repoName(origin, hasOrigin, m.Checkout)
	if err != nil {
		return proto.Prefix{}, err
	}
	hostname, hasHostname, err := envHostname(envLocal)
	if err != nil {
		return proto.Prefix{}, err
	}
	site := m.Site
	if site == "" {
		site = DefaultSite
		if hasHostname {
			norm, ok := proto.NormaliseSite(hostname)
			if !ok {
				return proto.Prefix{}, refuse(envFile, "HOOKS_DAEMON_HOSTNAME is not a site label")
			}
			site = norm
		}
	}
	p, ok := proto.ParsePrefix(repo + "+" + site + ".lxc")
	if !ok {
		return proto.Prefix{}, refuse("the handle", "the repo and site do not make a handle prefix")
	}
	return p, nil
}

const checkoutName = "the checkout's directory name"

// repoName is the handle's repo: the origin URL's last path component less .git, else the
// checkout directory's base name, normalised (A 2.3).
func repoName(origin string, hasOrigin bool, checkout string) (string, error) {
	file, name := checkoutName, filepath.Base(checkout)
	if hasOrigin {
		file = gitFile
		url := strings.TrimRight(origin, "/")
		name = strings.TrimSuffix(url[strings.LastIndexAny(url, "/:")+1:], ".git")
	}
	repo, ok := proto.NormaliseRepo(name)
	if !ok {
		return "", refuse(file, "nothing is left of the repository name once normalised")
	}
	return repo, nil
}

// SameMember refuses a derived handle that differs from the recorded one (n3-D10, n3b-D2): the
// handle names the member, so a checkout that moved is a new member, under another seat.
func SameMember(derived, recorded proto.Handle) error {
	if recorded != derived {
		return refuse("the handle", "the checkout's origin changed; give the entry another seat to take a new handle")
	}
	return nil
}

const (
	gitFile = "the checkout's .git/config"
	envFile = "the checkout's ccy.env.local"
)

var (
	gitSection = regexp.MustCompile(`^\s*\[([A-Za-z0-9-]+)(?:\s+"((?:[^"\\]|\\.)*)")?\]\s*(?:[#;].*)?$`)
	gitKey     = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9-]*)\s*(?:=(.*))?$`)
	// gitURL is a bare value, with nothing git would unquote or unescape.
	gitURL = regexp.MustCompile(`^\s*([^\s"\\#;]+)\s*(?:[#;].*)?$`)
	// envLine is an assignment of a literal: nothing a shell would expand, quote, join or end.
	envLine = regexp.MustCompile("^(?:export )?([A-Za-z_][A-Za-z0-9_]*)=([^\\s$`'\"\\\\;&|<>()~]*)$")
)

// gitScan is the reading of a .git/config so far.
type gitScan struct {
	inSection, inOrigin, sawOrigin bool
	urls                           []string
}

// originURL is the url of the checkout's `[remote "origin"]`; found is false when there is no
// origin. The file is read as a closed form of sections, keys and comments, and no git program
// runs: a line continued, a key outside a section, an include (whose file is not read) or any
// insteadOf or pushInsteadOf (which would make git fetch from elsewhere than the url read here)
// refuses it.
func originURL(data []byte) (string, bool, error) {
	if len(data) > MaxCheckout {
		return "", false, refuse(gitFile, "larger than %d bytes", MaxCheckout)
	}
	if err := text(data, gitFile, true); err != nil {
		return "", false, err
	}
	var scan gitScan
	for line := range strings.SplitSeq(string(data), "\n") {
		if err := scan.line(line); err != nil {
			return "", false, err
		}
	}
	switch {
	case !scan.sawOrigin:
		return "", false, nil
	case len(scan.urls) != 1:
		return "", false, refuse(gitFile, "origin has %d urls, not one", len(scan.urls))
	default:
		return scan.urls[0], true, nil
	}
}

func (g *gitScan) line(line string) error {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasSuffix(trimmed, `\`):
		return refuse(gitFile, "a line is continued")
	case trimmed == "" || trimmed[0] == '#' || trimmed[0] == ';':
		return nil
	case trimmed[0] == '[':
		return g.section(line)
	default:
		return g.key(line)
	}
}

func (g *gitScan) section(line string) error {
	m := gitSection.FindStringSubmatch(line)
	if m == nil {
		return refuse(gitFile, "a section header is not [name] or [name \"sub\"]")
	}
	if name := strings.ToLower(m[1]); name == "include" || name == "includeif" {
		return refuse(gitFile, "it includes another file")
	}
	g.inSection = true
	g.inOrigin = strings.EqualFold(m[1], "remote") && unescapeSubsection(m[2]) == "origin"
	g.sawOrigin = g.sawOrigin || g.inOrigin
	return nil
}

// unescapeSubsection is a subsection as git reads it: a backslash drops, and the byte after it
// is taken as it is, so `orig\in` is origin.
func unescapeSubsection(sub string) string {
	var b strings.Builder
	escaped := false
	for i := range len(sub) {
		if !escaped && sub[i] == '\\' {
			escaped = true
			continue
		}
		escaped = false
		b.WriteByte(sub[i])
	}
	return b.String()
}

func (g *gitScan) key(line string) error {
	m := gitKey.FindStringSubmatch(line)
	switch {
	case m == nil:
		return refuse(gitFile, "a line is not a section, a key or a comment")
	case !g.inSection:
		return refuse(gitFile, "a key comes before any section")
	}
	key := strings.ToLower(m[1])
	if key == "insteadof" || key == "pushinsteadof" {
		return refuse(gitFile, "it rewrites URLs with insteadOf or pushInsteadOf")
	}
	if !g.inOrigin || key != "url" {
		return nil
	}
	v := gitURL.FindStringSubmatch(m[2])
	if v == nil || !strings.Contains(line, "=") {
		return refuse(gitFile, "origin's url is not a bare value")
	}
	g.urls = append(g.urls, v[1])
	return nil
}

// envHostname is ccy.env.local's HOOKS_DAEMON_HOSTNAME literal; found is false when it is unset
// or there is no file (nil). The file is parsed, never sourced: only blank lines, # comments and
// `[export ]NAME=literal` lines, each name assigned once.
func envHostname(data []byte) (string, bool, error) {
	if data == nil {
		return "", false, nil
	}
	if len(data) > MaxCheckout {
		return "", false, refuse(envFile, "larger than %d bytes", MaxCheckout)
	}
	if err := text(data, envFile, true); err != nil {
		return "", false, err
	}
	var (
		seen     = map[string]bool{}
		hostname string
		found    bool
	)
	for line := range strings.SplitSeq(string(data), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed == "" || trimmed[0] == '#' {
			continue
		}
		m := envLine.FindStringSubmatch(line)
		switch {
		case m == nil:
			return "", false, refuse(envFile, "a line is not a comment or [export ]NAME=literal with no $, `, quote, space or shell operator")
		case seen[m[1]]:
			return "", false, refuse(envFile, "a name is assigned twice")
		}
		seen[m[1]] = true
		if m[1] == "HOOKS_DAEMON_HOSTNAME" {
			hostname, found = m[2], true
		}
	}
	return hostname, found, nil
}
