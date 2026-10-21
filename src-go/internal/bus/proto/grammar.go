package proto

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// The identifier patterns of §3, §6 and §11 (SPEC P4-P13), ASCII only.
const (
	handleRepoPattern  = `[a-z0-9][a-z0-9_-]{0,47}`
	handleHostPattern  = `[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?`
	handleSeatPattern  = `[1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11}`
	handleTypes        = `podman|lxc|docker|vm|host`
	ownerPattern       = `[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?`
	repoPattern        = `[a-z0-9._-]{1,100}`
	shaPattern         = `[0-9a-f]{40}`
	numPattern         = `[1-9][0-9]{0,9}`
	segPattern         = `[A-Za-z0-9._-]{1,64}`
	pathPattern        = segPattern + `(?:/` + segPattern + `){0,7}`
	ownerRepoPattern   = `(` + ownerPattern + `)/(` + repoPattern + `)`
	maxRefLen          = 700
	descriptionMax     = 120
	reservedLocalparts = "admin conduit"
)

func whole(pattern string) *regexp.Regexp { return regexp.MustCompile(`^(?:` + pattern + `)$`) }

var (
	handleRE = whole(`(` + handleRepoPattern + `)\.(` + handleSeatPattern + `)\+(` + handleHostPattern +
		`)\.(` + handleTypes + `)`)
	seatRE      = whole(handleSeatPattern)
	hostRE      = whole(handleHostPattern)
	teamRE      = whole(`[a-z][a-z0-9-]{0,23}`)
	humanRE     = whole(`[a-z][a-z0-9_-]{0,31}`)
	roomIDRE    = whole(`![A-Za-z0-9_-]{43}`)
	eventIDRE   = whole(`\$[A-Za-z0-9_-]{43}`)
	branchRE    = whole(`[A-Za-z0-9._/-]{1,100}`)
	ownerRE     = whole(ownerPattern)
	repoNameRE  = whole(repoPattern)
	pathRE      = whole(pathPattern)
	ownerRepoRE = whole(ownerRepoPattern)
	refREs      = []struct {
		form string
		re   *regexp.Regexp
	}{
		{FormPath, whole(`path:` + ownerRepoPattern + `@(` + shaPattern + `):(` + pathPattern + `)`)},
		{FormCommit, whole(`commit:` + ownerRepoPattern + `@(` + shaPattern + `)`)},
		{FormPR, whole(`pr:` + ownerRepoPattern + `#(` + numPattern + `)@(` + shaPattern + `)`)},
		{FormIssue, whole(`issue:` + ownerRepoPattern + `#(` + numPattern + `)`)},
	}
	refRepoPartRE = regexp.MustCompile(`(?s)^(path|commit|pr|issue):([^@#]*)(.*)$`)
)

// IsTeamName is SPEC P4.
func IsTeamName(s string) bool { return teamRE.MatchString(s) }

// ParseHandle is SPEC P5: a handle's four parts.
func ParseHandle(s string) (Handle, bool) {
	m := handleRE.FindStringSubmatch(s)
	if m == nil {
		return Handle{}, false
	}
	return Handle{Repo: m[1], Seat: m[2], Host: m[3], Type: m[4]}, true
}

// N is a numbered seat's number.
func (h Handle) N() (int64, bool) {
	if h.Seat == "" || h.Seat[0] < '1' || h.Seat[0] > '9' {
		return 0, false
	}
	n, err := strconv.ParseInt(h.Seat, 10, 64)
	return n, err == nil
}

// IsSeat is SPEC P6.
func IsSeat(s string) bool { return seatRE.MatchString(s) }

func isReserved(localpart string) bool {
	return slices.Contains(strings.Fields(reservedLocalparts), localpart)
}

// IsHumanLocalpart is SPEC P7: never a reserved localpart (P8).
func IsHumanLocalpart(s string) bool { return humanRE.MatchString(s) && !isReserved(s) }

// ParseUserID is SPEC P9: the localpart of @<localpart>:<server> on exactly this server, when
// it is a handle, a reserved localpart or a human localpart.
func ParseUserID(s, serverName string) (string, bool) {
	rest, ok := strings.CutPrefix(s, "@")
	if !ok {
		return "", false
	}
	localpart, server, found := strings.Cut(rest, ":")
	if !found || server != serverName {
		return "", false
	}
	if _, handle := ParseHandle(localpart); handle || isReserved(localpart) || IsHumanLocalpart(localpart) {
		return localpart, true
	}
	return "", false
}

// IsAgentUserID is a user ID whose localpart is a handle; any non-string is not.
func IsAgentUserID(v any, serverName string) bool {
	localpart, ok := userLocalpart(v, serverName)
	if !ok {
		return false
	}
	_, handle := ParseHandle(localpart)
	return handle
}

// IsHumanUserID is a user ID whose localpart is a human localpart; any non-string is not.
func IsHumanUserID(v any, serverName string) bool {
	localpart, ok := userLocalpart(v, serverName)
	return ok && IsHumanLocalpart(localpart)
}

func userLocalpart(v any, serverName string) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return ParseUserID(s, serverName)
}

// IsRoomID and IsEventID are SPEC P10.
func IsRoomID(s string) bool { return roomIDRE.MatchString(s) }

// IsEventID is SPEC P10.
func IsEventID(s string) bool { return eventIDRE.MatchString(s) }

// IsBranch is SPEC P11.
func IsBranch(s string) bool { return branchRE.MatchString(s) }

// IsOwner is SPEC P12's owner.
func IsOwner(s string) bool { return ownerRE.MatchString(s) }

func repoNameOK(repo string) bool {
	return repo != "." && repo != ".." && !strings.HasSuffix(repo, ".git")
}

// IsRepoName is SPEC P12's repository name.
func IsRepoName(s string) bool { return repoNameRE.MatchString(s) && repoNameOK(s) }

// IsOwnerRepo is SPEC P12's OWNER/REPO.
func IsOwnerRepo(s string) bool {
	m := ownerRepoRE.FindStringSubmatch(s)
	return m != nil && repoNameOK(m[2])
}

func pathOK(path string) bool {
	if !pathRE.MatchString(path) {
		return false
	}
	for seg := range strings.SplitSeq(path, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// ParseRef is SPEC P13.
func ParseRef(s string) (Ref, bool) {
	if charLen(s) > maxRefLen {
		return Ref{}, false
	}
	for _, f := range refREs {
		m := f.re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		ref := Ref{Text: s, Form: f.form, Owner: m[1], Repo: m[2]}
		if !repoNameOK(ref.Repo) {
			return Ref{}, false
		}
		switch f.form {
		case FormPath:
			ref.SHA, ref.Path = m[3], m[4]
			if !pathOK(ref.Path) {
				return Ref{}, false
			}
		case FormCommit:
			ref.SHA = m[3]
		case FormPR:
			ref.Num, ref.SHA = mustNum(m[3]), m[4]
		case FormIssue:
			ref.Num = mustNum(m[3])
		}
		return ref, true
	}
	return Ref{}, false
}

// mustNum reads a NUM the grammar has matched: at most ten digits, so it fits.
func mustNum(digits string) int64 {
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		panic("proto: NUM grammar admitted " + digits)
	}
	return n
}

// LowercaseRefRepo is SPEC P14: only OWNER/REPO of anything reference-shaped is lowercased.
func LowercaseRefRepo(s string) string {
	m := refRepoPartRE.FindStringSubmatch(s)
	if m == nil {
		return s
	}
	return m[1] + ":" + pyLower(m[2]) + m[3]
}

// IsPathPrefix is SPEC P15: a PATH ending in "/", or an exact PATH.
func IsPathPrefix(s string) bool { return pathOK(strings.TrimSuffix(s, "/")) }

// PathAllowed is SPEC P15.
func PathAllowed(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasSuffix(p, "/") && strings.HasPrefix(path, p) || path == p {
			return true
		}
	}
	return false
}

// IsDescription is SPEC P16: 1 to 120 code points, stripped, no Cc or Cf (Go's Unicode tables).
func IsDescription(s string) bool {
	n := charLen(s)
	if n < 1 || n > descriptionMax || pyStrip(s) != s {
		return false
	}
	for _, r := range points(s) {
		if unicode.In(r, unicode.Cc, unicode.Cf) {
			return false
		}
	}
	return true
}

// Source is where a reference's repository stands with the team (§11).
type Source string

// The sources: listed in repos, owned by the org, or neither.
const (
	SourceListed   Source = "listed"
	SourceOrg      Source = "org"
	SourceExternal Source = "external"
)

// RefSource is SPEC P27: listed wins over the org.
func RefSource(ref Ref, ctx *Context) Source {
	if _, listed := ctx.Repos[ref.FullRepo()]; listed {
		return SourceListed
	}
	if ctx.Org != "" && ref.Owner == ctx.Org {
		return SourceOrg
	}
	return SourceExternal
}

// TrustsAnyBranch is an org repository not listed, or one listed with no branches.
func TrustsAnyBranch(ref Ref, ctx *Context) bool {
	switch RefSource(ref, ctx) {
	case SourceOrg:
		return true
	case SourceListed:
		return len(ctx.Repos[ref.FullRepo()]) == 0
	case SourceExternal:
	}
	return false
}

// IsExcluded is a repository of the org that the team excludes.
func IsExcluded(ref Ref, ctx *Context) bool {
	return ctx.Org != "" && ref.Owner == ctx.Org && slices.Contains(ctx.Exclude, ref.Repo)
}
