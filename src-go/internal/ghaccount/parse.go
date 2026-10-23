package ghaccount

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// version is a gh release, x.y.z.
type version [3]int

// minGhVersion is the first gh whose `auth status` takes --json (E5, E6); multi-account
// support came earlier, in 2.40.0.
var minGhVersion = version{2, 81, 0}

var versionPattern = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// ghVersion is the first x.y.z in the output of `gh --version`.
func ghVersion(out string) (version, error) {
	m := versionPattern.FindStringSubmatch(out)
	if m == nil {
		return version{}, fmt.Errorf("no version in %q", strings.TrimSpace(out))
	}
	var v version
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return version{}, fmt.Errorf("version %q: %w", m[0], err)
		}
		v[i] = n
	}
	return v, nil
}

func (v version) String() string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

func (v version) atLeast(floor version) bool {
	for i := range v {
		if v[i] != floor[i] {
			return v[i] > floor[i]
		}
	}
	return true
}

// status is what `gh auth status --json hosts` says of github.com (E6).
type status struct {
	active  string   // the logged-in active account, "" for none
	working []string // every account whose token is good
}

func (s status) loggedIn(login string) bool { return s.login(login) != "" }

// login is the logged-in account matching login, spelled as gh holds it; "" when none is.
func (s status) login(login string) string {
	for _, l := range s.working {
		if strings.EqualFold(l, login) {
			return l
		}
	}
	return ""
}

func parseStatus(out []byte) (status, error) {
	var doc struct {
		Hosts map[string][]struct {
			Login  string `json:"login"`
			Active bool   `json:"active"`
			State  string `json:"state"`
		} `json:"hosts"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return status{}, fmt.Errorf("not gh's status JSON: %w", err)
	}
	if doc.Hosts == nil {
		return status{}, errors.New("not gh's status JSON: no hosts")
	}
	var st status
	for _, e := range doc.Hosts["github.com"] {
		if e.State != "success" {
			continue
		}
		st.working = append(st.working, e.Login)
		if e.Active {
			st.active = e.Login
		}
	}
	return st, nil
}

type verdictKind int

const (
	scopesOK verdictKind = iota
	scopesMissing
	scopesNotAuthenticated
	scopesUnreadable
)

// verdictWords are the audit's line prefixes without "SCOPES-", in kind order, and whether
// each carries a detail.
type verdictWord struct {
	word   string
	detail bool
}

var verdictWords = []verdictWord{
	scopesOK:               {"OK", false},
	scopesMissing:          {"MISSING", true},
	scopesNotAuthenticated: {"NOT-AUTHENTICATED", false},
	scopesUnreadable:       {"UNREADABLE", true},
}

// verdict is one line of the scope helper's audit (E10).
type verdict struct {
	login  string
	kind   verdictKind
	detail string // the missing scopes, or why they cannot be read
}

// problem is the verdict as the headless failure names it: the helper's line without SCOPES-.
func (v verdict) problem() string {
	s := verdictWords[v.kind].word + " " + v.login
	if v.detail != "" {
		s += " " + v.detail
	}
	return s
}

// parseAudit reads one line per asked login, in order; anything else is refused.
func parseAudit(out string, logins []string) ([]verdict, error) {
	var lines []string
	if out != "" {
		lines = strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	}
	if len(lines) != len(logins) {
		return nil, fmt.Errorf("the scope audit gave %d lines for %d accounts: %q", len(lines), len(logins), out)
	}
	verdicts := make([]verdict, 0, len(logins))
	for i, l := range lines {
		v, err := parseVerdict(l)
		if err != nil {
			return nil, err
		}
		if v.login != logins[i] {
			return nil, fmt.Errorf("the scope audit answered for %q where %q was asked: %q", v.login, logins[i], l)
		}
		verdicts = append(verdicts, v)
	}
	return verdicts, nil
}

func parseVerdict(l string) (verdict, error) {
	parts := strings.SplitN(l, " ", 3)
	if len(parts) < 2 || parts[1] == "" {
		return verdict{}, fmt.Errorf("not a scope audit line: %q", l)
	}
	kind := slices.IndexFunc(verdictWords, func(w verdictWord) bool { return "SCOPES-"+w.word == parts[0] })
	hasDetail := len(parts) == 3 && parts[2] != ""
	if kind < 0 || verdictWords[kind].detail != hasDetail {
		return verdict{}, fmt.Errorf("not a scope audit line: %q", l)
	}
	v := verdict{login: parts[1], kind: verdictKind(kind)}
	if hasDetail {
		v.detail = parts[2]
	}
	return v, nil
}

// parseVaultOutput is the msg of the minimal callback's `host | SUCCESS => {json}` (E11).
func parseVaultOutput(out string) (string, error) {
	head, body, found := strings.Cut(out, "=> ")
	if !found || head == "" {
		return "", errors.New("the output has no 'host | SUCCESS => '")
	}
	var doc struct {
		Msg *string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return "", fmt.Errorf("the output is not a JSON object: %w", err)
	}
	if doc.Msg == nil || *doc.Msg == "" {
		return "", errors.New("the output has no msg string")
	}
	return *doc.Msg, nil
}

// publicKeyField is the base64 key of a .pub line, `<type> <key> [comment]` (E18).
func publicKeyField(pub string) (string, error) {
	fields := strings.Fields(pub)
	if len(fields) < 2 {
		return "", errors.New("not an OpenSSH public key: want '<type> <key> [comment]'")
	}
	return fields[1], nil
}

// keyListed reports whether a line of the key listing has field as its key.
func keyListed(listing, field string) bool {
	for l := range strings.SplitSeq(listing, "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[1] == field {
			return true
		}
	}
	return false
}

var greetingPattern = regexp.MustCompile(`Hi ([^!\s]+)!`)

// sshLogin is the login GitHub's SSH greeting names, "" when it names none (E20).
func sshLogin(out string) string {
	if m := greetingPattern.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}
