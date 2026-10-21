package paths

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrInvalid is the error every Parse function wraps when a value fails its grammar. The error
// names the kind of value, never the value: a value that fails its grammar is never rendered
// (A 2.2).
var ErrInvalid = errors.New("invalid")

// Each identifier below is opaque: only its Parse function makes one, a decode (JSON, YAML, any
// encoding.TextUnmarshaler user) goes through that Parse, and no conversion from a string
// compiles, so a derived name never holds an unchecked string. The zero value was never parsed
// and is refused by MarshalText.

// TeamID is a team's stable id (A 2.2): fixed at creation, never changed, and the one key of
// every name derived from a team.
type TeamID struct{ s string }

// RunID names one session's runtime directory: 16 lowercase hex from crypto/rand (B 2.4).
type RunID struct{ s string }

// CheckoutKey is the first 32 hex of the SHA-256 of a checkout's real path (C 3.7).
type CheckoutKey struct{ s string }

// GuestUser is the user an LXC guest's supervised session runs as.
type GuestUser struct{ s string }

// SetID names a migration set (D 5.3).
type SetID struct{ s string }

// SessionName names a ccy session in the owner's seat store (C 3.7).
type SessionName struct{ s string }

// Digest names one immutable install by its content: a kit directory (C33) or a copy of
// teamchat-admin (C 3.5).
type Digest struct{ s string }

// DisplayName is a team's display name (A 2.2): shown, never an identifier.
type DisplayName struct{ s string }

// ServerVersion is the pinned homeserver's version, part of its binary's name.
type ServerVersion struct{ s string }

// Port is a team's homeserver port. Ports are declared per team, never computed (D 3.8).
type Port uint16

var (
	teamIDGrammar      = regexp.MustCompile(`^[a-z][a-z0-9-]{0,21}$`)
	runIDGrammar       = regexp.MustCompile(`^[0-9a-f]{16}$`)
	checkoutKeyGrammar = regexp.MustCompile(`^[0-9a-f]{32}$`)
	guestUserGrammar   = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	// digestGrammar is lowercase hex, from 64-bit to SHA-256 length: never a word such as the
	// kit root's current link.
	digestGrammar = regexp.MustCompile(`^[0-9a-f]{16,64}$`)
	// segmentGrammar holds the identifiers the design gives no grammar of their own to one path
	// segment that cannot be "." or "..", hold a separator, or read as an option.
	segmentGrammar = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
)

func check(kind string, grammar *regexp.Regexp, s string) (string, error) {
	if !grammar.MatchString(s) {
		return "", fmt.Errorf("%w %s: does not match %s", ErrInvalid, kind, grammar)
	}
	return s, nil
}

// reservedTeamIDs are the ids that would make a team-derived name equal a fixed v2 name: team
// ctl's homeserver user would be teamchat-ctl, the user holding every team's admin token, and
// team membership's runtime directory /run/teamchat/membership, the ccy session's membership
// mount. The package's test derives the list from the fixed names and holds it complete.
var reservedTeamIDs = []string{
	"admin", "backup", "ctl", "fence", "health", "install", "membership", "seats", "server", "status",
}

// ParseTeamID checks a team id against A 2.2's grammar and refuses a reserved one.
func ParseTeamID(s string) (TeamID, error) {
	v, err := check("team id", teamIDGrammar, s)
	if err == nil && slices.Contains(reservedTeamIDs, s) {
		return TeamID{}, fmt.Errorf("%w team id: reserved, it would make a derived name equal a fixed v2 name", ErrInvalid)
	}
	return TeamID{v}, err
}

// ParseDisplayName checks a display name: 1-64 Unicode scalar values, no control, format or
// newline characters.
func ParseDisplayName(s string) (DisplayName, error) {
	n := utf8.RuneCountInString(s)
	bad := !utf8.ValidString(s) || n < 1 || n > 64 ||
		strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) })
	if bad {
		return DisplayName{}, fmt.Errorf("%w display name: not 1-64 characters free of control and format characters", ErrInvalid)
	}
	return DisplayName{s}, nil
}

// ParseRunID checks a run id.
func ParseRunID(s string) (RunID, error) {
	v, err := check("run id", runIDGrammar, s)
	return RunID{v}, err
}

// ParseCheckoutKey checks a checkout key.
func ParseCheckoutKey(s string) (CheckoutKey, error) {
	v, err := check("checkout key", checkoutKeyGrammar, s)
	return CheckoutKey{v}, err
}

// ParseGuestUser checks a guest user name (the portable user-name grammar).
func ParseGuestUser(s string) (GuestUser, error) {
	v, err := check("guest user", guestUserGrammar, s)
	return GuestUser{v}, err
}

// ParseSetID checks a migration set id.
func ParseSetID(s string) (SetID, error) {
	v, err := check("set id", segmentGrammar, s)
	return SetID{v}, err
}

// ParseSessionName checks a seat-store session name.
func ParseSessionName(s string) (SessionName, error) {
	v, err := check("session name", segmentGrammar, s)
	return SessionName{v}, err
}

// ParseDigest checks a digest.
func ParseDigest(s string) (Digest, error) {
	v, err := check("digest", digestGrammar, s)
	return Digest{v}, err
}

// ParseServerVersion checks a homeserver version.
func ParseServerVersion(s string) (ServerVersion, error) {
	v, err := check("server version", segmentGrammar, s)
	return ServerVersion{v}, err
}

// ParsePort checks a declared port: 1 to 65535.
func ParsePort(n int) (Port, error) {
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("%w port: not in 1-65535", ErrInvalid)
	}
	return Port(n), nil
}

// marshal is an id's text, refused for the zero value, which was never parsed.
func marshal(kind, s string) ([]byte, error) {
	if s == "" {
		return nil, fmt.Errorf("%w %s: the zero value was never parsed", ErrInvalid, kind)
	}
	return []byte(s), nil
}

// unmarshal sets *dst to the parse of text, or leaves it and returns the parse's error.
func unmarshal[T any](dst *T, parse func(string) (T, error), text []byte) error {
	v, err := parse(string(text))
	if err != nil {
		return err
	}
	*dst = v
	return nil
}

// String is the team id's text.
func (t TeamID) String() string { return t.s }

// MarshalText encodes the team id, and refuses the zero value.
func (t TeamID) MarshalText() ([]byte, error) { return marshal("team id", t.s) }

// UnmarshalText decodes a team id through ParseTeamID.
func (t *TeamID) UnmarshalText(b []byte) error { return unmarshal(t, ParseTeamID, b) }

// String is the run id's text.
func (r RunID) String() string { return r.s }

// MarshalText encodes the run id, and refuses the zero value.
func (r RunID) MarshalText() ([]byte, error) { return marshal("run id", r.s) }

// UnmarshalText decodes a run id through ParseRunID.
func (r *RunID) UnmarshalText(b []byte) error { return unmarshal(r, ParseRunID, b) }

// String is the checkout key's text.
func (k CheckoutKey) String() string { return k.s }

// MarshalText encodes the checkout key, and refuses the zero value.
func (k CheckoutKey) MarshalText() ([]byte, error) { return marshal("checkout key", k.s) }

// UnmarshalText decodes a checkout key through ParseCheckoutKey.
func (k *CheckoutKey) UnmarshalText(b []byte) error { return unmarshal(k, ParseCheckoutKey, b) }

// String is the guest user's text.
func (u GuestUser) String() string { return u.s }

// MarshalText encodes the guest user, and refuses the zero value.
func (u GuestUser) MarshalText() ([]byte, error) { return marshal("guest user", u.s) }

// UnmarshalText decodes a guest user through ParseGuestUser.
func (u *GuestUser) UnmarshalText(b []byte) error { return unmarshal(u, ParseGuestUser, b) }

// String is the set id's text.
func (s SetID) String() string { return s.s }

// MarshalText encodes the set id, and refuses the zero value.
func (s SetID) MarshalText() ([]byte, error) { return marshal("set id", s.s) }

// UnmarshalText decodes a set id through ParseSetID.
func (s *SetID) UnmarshalText(b []byte) error { return unmarshal(s, ParseSetID, b) }

// String is the session name's text.
func (n SessionName) String() string { return n.s }

// MarshalText encodes the session name, and refuses the zero value.
func (n SessionName) MarshalText() ([]byte, error) { return marshal("session name", n.s) }

// UnmarshalText decodes a session name through ParseSessionName.
func (n *SessionName) UnmarshalText(b []byte) error { return unmarshal(n, ParseSessionName, b) }

// String is the digest's text.
func (d Digest) String() string { return d.s }

// MarshalText encodes the digest, and refuses the zero value.
func (d Digest) MarshalText() ([]byte, error) { return marshal("digest", d.s) }

// UnmarshalText decodes a digest through ParseDigest.
func (d *Digest) UnmarshalText(b []byte) error { return unmarshal(d, ParseDigest, b) }

// String is the display name's text.
func (d DisplayName) String() string { return d.s }

// MarshalText encodes the display name, and refuses the zero value.
func (d DisplayName) MarshalText() ([]byte, error) { return marshal("display name", d.s) }

// UnmarshalText decodes a display name through ParseDisplayName.
func (d *DisplayName) UnmarshalText(b []byte) error { return unmarshal(d, ParseDisplayName, b) }

// String is the server version's text.
func (v ServerVersion) String() string { return v.s }

// MarshalText encodes the server version, and refuses the zero value.
func (v ServerVersion) MarshalText() ([]byte, error) { return marshal("server version", v.s) }

// UnmarshalText decodes a server version through ParseServerVersion.
func (v *ServerVersion) UnmarshalText(b []byte) error { return unmarshal(v, ParseServerVersion, b) }
