package paths

import (
	"errors"
	"fmt"
	"regexp"
)

// ErrInvalid is the error every Parse function wraps when a value fails its grammar. The error
// names the kind of value, never the value: a value that fails its grammar is never rendered
// (A 2.2).
var ErrInvalid = errors.New("invalid")

// TeamID is a team's stable id (A 2.2): fixed at creation, never changed, and the one key of
// every name derived from a team. Only ParseTeamID makes one, so a derived name never holds an
// unchecked string.
type TeamID string

// RunID names one session's runtime directory: 16 lowercase hex from crypto/rand (B 2.4).
type RunID string

// CheckoutKey is the first 32 hex of the SHA-256 of a checkout's real path (C 3.7).
type CheckoutKey string

// GuestUser is the user an LXC guest's supervised session runs as.
type GuestUser string

// SetID names a migration set (D 5.3).
type SetID string

// SessionName names a ccy session in the owner's seat store (C 3.7).
type SessionName string

// KitDigest names one immutable kit directory (C33).
type KitDigest string

// ServerVersion is the pinned homeserver's version, part of its binary's name.
type ServerVersion string

// Port is a team's homeserver port. Ports are declared per team, never computed (D 3.8).
type Port uint16

var (
	teamIDGrammar      = regexp.MustCompile(`^[a-z][a-z0-9-]{0,21}$`)
	runIDGrammar       = regexp.MustCompile(`^[0-9a-f]{16}$`)
	checkoutKeyGrammar = regexp.MustCompile(`^[0-9a-f]{32}$`)
	guestUserGrammar   = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	// segmentGrammar holds the identifiers the design gives no grammar of their own to one path
	// segment that cannot be "." or "..", hold a separator, or read as an option.
	segmentGrammar = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
)

func parse[T ~string](kind string, grammar *regexp.Regexp, s string) (T, error) {
	if !grammar.MatchString(s) {
		return "", fmt.Errorf("%w %s: does not match %s", ErrInvalid, kind, grammar)
	}
	return T(s), nil
}

// ParseTeamID checks a team id against A 2.2's grammar.
func ParseTeamID(s string) (TeamID, error) { return parse[TeamID]("team id", teamIDGrammar, s) }

// ParseRunID checks a run id.
func ParseRunID(s string) (RunID, error) { return parse[RunID]("run id", runIDGrammar, s) }

// ParseCheckoutKey checks a checkout key.
func ParseCheckoutKey(s string) (CheckoutKey, error) {
	return parse[CheckoutKey]("checkout key", checkoutKeyGrammar, s)
}

// ParseGuestUser checks a guest user name (the portable user-name grammar).
func ParseGuestUser(s string) (GuestUser, error) {
	return parse[GuestUser]("guest user", guestUserGrammar, s)
}

// ParseSetID checks a migration set id.
func ParseSetID(s string) (SetID, error) { return parse[SetID]("set id", segmentGrammar, s) }

// ParseSessionName checks a seat-store session name.
func ParseSessionName(s string) (SessionName, error) {
	return parse[SessionName]("session name", segmentGrammar, s)
}

// ParseKitDigest checks a kit digest.
func ParseKitDigest(s string) (KitDigest, error) {
	return parse[KitDigest]("kit digest", segmentGrammar, s)
}

// ParseServerVersion checks a homeserver version.
func ParseServerVersion(s string) (ServerVersion, error) {
	return parse[ServerVersion]("server version", segmentGrammar, s)
}

// ParsePort checks a declared port: 1 to 65535.
func ParsePort(n int) (Port, error) {
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("%w port: not in 1-65535", ErrInvalid)
	}
	return Port(n), nil
}

func (t TeamID) String() string { return string(t) }
