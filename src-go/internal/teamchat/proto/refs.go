package proto

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// MaxRefLen is the longest reference, in characters (A 9.1; the grammar is ASCII).
const MaxRefLen = 700

// The reference forms of A 9.1.
const (
	RefPath   = "path"
	RefCommit = "commit"
	RefPR     = "pr"
	RefIssue  = "issue"
)

const (
	ownerPattern   = `[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?`
	repoPattern9   = `[a-z0-9._-]{1,100}`
	shaPattern     = `[0-9a-f]{40}`
	numPattern     = `[1-9][0-9]{0,9}`
	segmentPattern = `[A-Za-z0-9._-]{1,64}`
	pathPattern    = segmentPattern + `(?:/` + segmentPattern + `){0,7}`
	ownerRepo      = `(` + ownerPattern + `)/(` + repoPattern9 + `)`
)

var (
	ownerRe     = regexp.MustCompile(`^` + ownerPattern + `$`)
	ownerRepoRe = regexp.MustCompile(`^` + ownerRepo + `$`)
	pathRe      = regexp.MustCompile(`^` + pathPattern + `$`)
	refRes      = map[string]*regexp.Regexp{
		RefPath:   regexp.MustCompile(`^path:` + ownerRepo + `@(` + shaPattern + `):(` + pathPattern + `)$`),
		RefCommit: regexp.MustCompile(`^commit:` + ownerRepo + `@(` + shaPattern + `)$`),
		RefPR:     regexp.MustCompile(`^pr:` + ownerRepo + `#(` + numPattern + `)@(` + shaPattern + `)$`),
		RefIssue:  regexp.MustCompile(`^issue:` + ownerRepo + `#(` + numPattern + `)$`),
	}
	refRepoPartRe = regexp.MustCompile(`(?s)^(path|commit|pr|issue):([^@#]*)(.*)$`)
)

// Ref is a parsed reference (A 9.1). Text is the reference as sent.
type Ref struct {
	Text, Form, Owner, Repo, SHA, Path string
	Num                                int
}

// OwnerRepo is `OWNER/REPO`.
func (r Ref) OwnerRepo() string { return r.Owner + "/" + r.Repo }

// ValidOwner is A 9.1's OWNER, which is also the roster's `forge.org`.
func ValidOwner(s string) bool { return ownerRe.MatchString(s) }

// ValidOwnerRepo is A 9.1's `OWNER/REPO`, as the roster's `repos` and `exclude` name one.
func ValidOwnerRepo(s string) bool {
	m := ownerRepoRe.FindStringSubmatch(s)
	return m != nil && repoNameOK(m[2])
}

func repoNameOK(repo string) bool {
	return repo != "." && repo != ".." && !strings.HasSuffix(repo, ".git")
}

func pathOK(path string) bool {
	if !pathRe.MatchString(path) {
		return false
	}
	for seg := range strings.SplitSeq(path, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// ValidPathPrefix is a roster `path_prefixes` entry: a PATH, or a PATH followed by `/`.
func ValidPathPrefix(s string) bool {
	return pathOK(strings.TrimSuffix(s, "/"))
}

// PathAllowed is whether a `path` reference's PATH matches the roster's prefixes: an entry
// ending `/` is a prefix, any other is exact; no prefixes allow every path (A 9.1).
func PathAllowed(path string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if strings.HasSuffix(p, "/") && strings.HasPrefix(path, p) || path == p {
			return true
		}
	}
	return false
}

// ParseRef parses a reference; false when it fails the grammar or the length cap.
func ParseRef(s string) (Ref, bool) {
	if len(s) > MaxRefLen {
		return Ref{}, false
	}
	form, _, found := strings.Cut(s, ":")
	re, known := refRes[form]
	if !found || !known {
		return Ref{}, false
	}
	m := re.FindStringSubmatch(s)
	if m == nil || !repoNameOK(m[2]) {
		return Ref{}, false
	}
	r := Ref{Text: s, Form: form, Owner: m[1], Repo: m[2]}
	switch form {
	case RefPath:
		r.SHA, r.Path = m[3], m[4]
	case RefCommit:
		r.SHA = m[3]
	case RefPR:
		r.SHA = m[4]
		r.Num = atoiDigits(m[3])
	case RefIssue:
		r.Num = atoiDigits(m[3])
	}
	if r.Path != "" && !pathOK(r.Path) {
		return Ref{}, false
	}
	return r, true
}

// atoiDigits is the value of at most ten decimal digits the grammar already matched.
func atoiDigits(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// LowercaseRefRepo lowercases, in ASCII, only the `OWNER/REPO` of something shaped like a
// reference, as a sender does before validating (A 9.1); anything else is returned unchanged.
func LowercaseRefRepo(s string) string {
	m := refRepoPartRe.FindStringSubmatch(s)
	if m == nil {
		return s
	}
	return m[1] + ":" + asciiLower(m[2]) + m[3]
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// RefClass is a reference's trust class (A 9.2).
type RefClass int

// The trust classes, in the order A 9.2 decides them.
const (
	RefExcluded RefClass = iota
	RefListed
	RefOrg
	RefExternal
)

// Class is the reference's trust class under this forge section: excluded, then listed (which
// wins over org), then org, else external (A 9.2).
func (f Forge) Class(r Ref) RefClass {
	name := r.OwnerRepo()
	switch {
	case slices.Contains(f.Exclude, name):
		return RefExcluded
	case f.listed(name) != nil:
		return RefListed
	case f.Org != "" && r.Owner == f.Org:
		return RefOrg
	}
	return RefExternal
}

// ListedBranches are the branches that alone prove a listed reference; nil when its
// repository is listed without branches, or not listed (A 9.3).
func (f Forge) ListedBranches(r Ref) []string {
	if repo := f.listed(r.OwnerRepo()); repo != nil {
		return repo.Branches
	}
	return nil
}

func (f Forge) listed(name string) *ForgeRepo {
	for i := range f.Repos {
		if f.Repos[i].Repo == name {
			return &f.Repos[i]
		}
	}
	return nil
}

// ForgeGitHub is the one forge kind v2 checks (A 5.1).
const ForgeGitHub = "github"

// ForgeOrigin is the API origin of a forge kind, fixed here and never taken from the roster
// (A 5.1, 9.3); empty for an unknown kind.
func ForgeOrigin(kind string) string {
	if kind == ForgeGitHub {
		return "https://api.github.com"
	}
	return ""
}
