package proto

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The reserved localparts of A 2.3. `conduit` and `tuwunel` stand for the homeserver's own
// service account until host check HC-A6 records Tuwunel's name; reserving both refuses no
// account anyone needs.
const (
	AdminLocalpart    = "admin"
	HumansWord        = "humans"
	TeamchatLocalpart = "teamchat"
)

var reservedLocalparts = map[string]bool{
	AdminLocalpart: true, HumansWord: true, TeamchatLocalpart: true, "conduit": true, "tuwunel": true,
}

// The probe's reserved handle parts (A 2.3): `teamchat.probe+<site>.host`.
const (
	probeRepo = "teamchat"
	probeSeat = "probe"
	probeType = "host"
)

const (
	repoPattern = `[a-z0-9][a-z0-9_-]{0,47}`
	seatPattern = `[1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11}`
	sitePattern = `[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?`
	typePattern = `podman|lxc|docker|vm|host`

	maxDisplayName = 64
	maxRepo        = 48
	maxSeatNumber  = 999999
)

var (
	teamIDRe    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,21}$`)
	roomIDRe    = regexp.MustCompile(`^![A-Za-z0-9_-]{43}$`)
	eventIDRe   = regexp.MustCompile(`^\$[A-Za-z0-9_-]{43}$`)
	branchRe    = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,100}$`)
	featureRe   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	questionRe  = regexp.MustCompile(`^Q[1-9][0-9]{0,8}$`)
	humanRe     = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	handleRe    = regexp.MustCompile(`^(` + repoPattern + `)\.(` + seatPattern + `)\+(` + sitePattern + `)\.(` + typePattern + `)$`)
	prefixRe    = regexp.MustCompile(`^(` + repoPattern + `)\+(` + sitePattern + `)\.(` + typePattern + `)$`)
	seatRe      = regexp.MustCompile(`^(?:` + seatPattern + `)$`)
	siteRe      = regexp.MustCompile(`^(?:` + sitePattern + `)$`)
	seatDigitRe = regexp.MustCompile(`^[1-9][0-9]{0,5}$`)
)

// ValidTeamID is the team id grammar of A 2.2.
func ValidTeamID(s string) bool { return teamIDRe.MatchString(s) }

// ValidDisplayName is A 2.2's team display name: 1-64 Unicode scalar values, none a control,
// a format character or a line or paragraph separator.
func ValidDisplayName(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	n := 0
	for _, r := range s {
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return false
		}
		n++
	}
	return n >= 1 && n <= maxDisplayName
}

// ValidRoomID is a room version 12 room ID (A 2.2).
func ValidRoomID(s string) bool { return roomIDRe.MatchString(s) }

// ValidEventID is a room version 12 event ID (A 2.2).
func ValidEventID(s string) bool { return eventIDRe.MatchString(s) }

// ValidBranch is A 2.2's branch grammar.
func ValidBranch(s string) bool { return branchRe.MatchString(s) }

// ValidFeature is A 2.2's feature name grammar.
func ValidFeature(s string) bool { return featureRe.MatchString(s) }

// ValidSeat is a handle's `<seat>` (A 2.3): a counter-issued number or an owner-named word.
func ValidSeat(s string) bool { return seatRe.MatchString(s) }

// ValidQuestion is A 2.2's question name, `Q<n>`.
func ValidQuestion(s string) bool { return questionRe.MatchString(s) }

// ParseQuestion is the number of a question name.
func ParseQuestion(s string) (int, bool) {
	if !ValidQuestion(s) {
		return 0, false
	}
	n, err := strconv.Atoi(s[1:])
	return n, err == nil
}

// QuestionName is `Q<n>`.
func QuestionName(n int) string { return "Q" + strconv.Itoa(n) }

// ValidHumanName is A 2.3's human localpart: no `.` and no `+`, so never a handle, and not
// reserved.
func ValidHumanName(s string) bool { return humanRe.MatchString(s) && !reservedLocalparts[s] }

// Handle is an agent's localpart, `<repo>.<seat>+<site>.<type>` (A 2.3).
type Handle struct {
	Repo, Seat, Site, Type string
}

// ParseHandle parses a handle.
func ParseHandle(s string) (Handle, bool) {
	m := handleRe.FindStringSubmatch(s)
	if m == nil {
		return Handle{}, false
	}
	return Handle{Repo: m[1], Seat: m[2], Site: m[3], Type: m[4]}, true
}

func (h Handle) String() string { return h.Repo + "." + h.Seat + "+" + h.Site + "." + h.Type }

// Short is the short name `<repo>.<seat>` (A 2.3).
func (h Handle) Short() string { return h.Repo + "." + h.Seat }

// Prefix is the handle's seat counter prefix, `<repo>+<site>.<type>`.
func (h Handle) Prefix() Prefix { return Prefix{Repo: h.Repo, Site: h.Site, Type: h.Type} }

// SeatNumber is a counter-issued seat's number; false for an owner-named seat.
func (h Handle) SeatNumber() (int, bool) {
	if !seatDigitRe.MatchString(h.Seat) {
		return 0, false
	}
	n, err := strconv.Atoi(h.Seat)
	return n, err == nil
}

// IsProbe is whether this is the team's reserved acceptance probe handle (A 2.3).
func (h Handle) IsProbe() bool {
	return h.Repo == probeRepo && h.Seat == probeSeat && h.Type == probeType
}

// ProbeHandle is the reserved probe handle of a site.
func ProbeHandle(site string) Handle {
	return Handle{Repo: probeRepo, Seat: probeSeat, Site: site, Type: probeType}
}

// Prefix is the part of a handle seats are counted under: `<repo>+<site>.<type>`.
type Prefix struct {
	Repo, Site, Type string
}

// ParsePrefix parses a prefix.
func ParsePrefix(s string) (Prefix, bool) {
	m := prefixRe.FindStringSubmatch(s)
	if m == nil {
		return Prefix{}, false
	}
	return Prefix{Repo: m[1], Site: m[2], Type: m[3]}, true
}

func (p Prefix) String() string { return p.Repo + "+" + p.Site + "." + p.Type }

// Numbered is the handle of seat number n under this prefix.
func (p Prefix) Numbered(n int) (Handle, bool) {
	if n < 1 || n > maxSeatNumber {
		return Handle{}, false
	}
	return p.Named(strconv.Itoa(n))
}

// Named is the handle of an owner-named seat under this prefix.
func (p Prefix) Named(seat string) (Handle, bool) {
	if !seatRe.MatchString(seat) {
		return Handle{}, false
	}
	return ParseHandle(Handle{Repo: p.Repo, Seat: seat, Site: p.Site, Type: p.Type}.String())
}

// NormaliseRepo turns a checkout's repository name into a handle's repo: ASCII lowercase,
// every other character outside `[a-z0-9_-]` a `-`, leading `-` and `_` removed, at most 48
// characters. False when nothing is left.
func NormaliseRepo(name string) (string, bool) {
	out := strings.TrimLeft(normalise(name, "_-"), "-_")
	if len(out) > maxRepo {
		out = out[:maxRepo]
	}
	return out, out != ""
}

// NormaliseSite turns an owner's site label into a handle's site: ASCII lowercase, every
// other character outside `[a-z0-9-]` a `-`; false when the result fails the site grammar.
func NormaliseSite(label string) (string, bool) {
	out := normalise(label, "-")
	return out, siteRe.MatchString(out)
}

func normalise(s, keep string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 'a' - 'A')
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || strings.ContainsRune(keep, r):
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// IDClass is what a user ID names.
type IDClass int

// The classes of user ID on a team's server.
const (
	IDNone IDClass = iota
	IDAgent
	IDHuman
	IDAdmin
	IDReserved
)

// UserID is `@<localpart>:<server>`.
func UserID(localpart, server string) string { return "@" + localpart + ":" + server }

// AdminID is the team's admin account, the anchor every bundle pins (A 6.2).
func AdminID(server string) string { return UserID(AdminLocalpart, server) }

// Localpart splits a user ID at its first `:`; false unless it starts `@`, has a localpart
// and names exactly this server (A 2.2).
func Localpart(id, server string) (string, bool) {
	rest, ok := strings.CutPrefix(id, "@")
	if !ok {
		return "", false
	}
	local, host, found := strings.Cut(rest, ":")
	if !found || local == "" || host != server {
		return "", false
	}
	return local, true
}

// ClassifyUserID is what a user ID names on this server: an agent (its localpart is a
// handle), a human (a human name), the admin, another reserved name, or nothing.
func ClassifyUserID(id, server string) IDClass {
	local, ok := Localpart(id, server)
	switch {
	case !ok:
		return IDNone
	case local == AdminLocalpart:
		return IDAdmin
	case reservedLocalparts[local]:
		return IDReserved
	case humanRe.MatchString(local):
		return IDHuman
	}
	if _, isHandle := ParseHandle(local); isHandle {
		return IDAgent
	}
	return IDNone
}

// AgentHandle is the handle of an agent user ID.
func AgentHandle(id, server string) (Handle, bool) {
	local, ok := Localpart(id, server)
	if !ok {
		return Handle{}, false
	}
	return ParseHandle(local)
}
