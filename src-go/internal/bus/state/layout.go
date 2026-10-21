package state

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
)

// The environment variables the layout reads (S6, S11, S12).
const (
	envHome        = "PINGBUS_HOME"
	envXDGConfig   = "XDG_CONFIG_HOME"
	envUserHome    = "HOME"
	envMembership  = "PINGBUS_MEMBERSHIP"
	envSessionsDir = "PINGBUS_SESSIONS_DIR"
	membershipOff  = "off"
	injectKeyHex   = 16
)

// The suffixes of a session's inject marks (S7, S44).
const (
	InjectWatch      = "watch"
	InjectTranscript = "transcript"
	InjectStalled    = "stalled"
	InjectMissing    = "missing"
	InjectSpawned    = "spawned"
)

var (
	// ErrBusOff is PINGBUS_MEMBERSHIP=off: the session was launched without the bus.
	ErrBusOff = errors.New("PINGBUS_MEMBERSHIP is off: this session was launched without the bus")
	// ErrNoMembership is PINGBUS_MEMBERSHIP unset: the session is not a membership session.
	ErrNoMembership = errors.New("PINGBUS_MEMBERSHIP is not set")

	injectSuffixes = []string{InjectWatch, InjectTranscript, InjectStalled, InjectMissing, InjectSpawned}
	injectKeyPat   = regexp.MustCompile(`^[0-9a-f]{16}$`)
	// sessionPattern is membership.SESSION_RE, a desired session's name.
	sessionPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@+-]{0,254}$`)
)

// absolute is value when it is an absolute path, else a grammar refusal naming the variable.
func absolute(name, value string) (string, error) {
	if !filepath.IsAbs(value) {
		return "", refuse(name, ClassGrammar, "not an absolute path")
	}
	return value, nil
}

// ResolveHome is PINGBUS_HOME (S6): `$PINGBUS_HOME`, else `$XDG_CONFIG_HOME/pingbus` when that
// is absolute, else `$HOME/.config/pingbus`. An empty variable is an unset one.
func ResolveHome(getenv func(string) string) (Home, error) {
	if explicit := getenv(envHome); explicit != "" {
		dir, err := absolute(envHome, explicit)
		return Home{Dir: dir}, err
	}
	if xdg := getenv(envXDGConfig); xdg != "" && filepath.IsAbs(xdg) {
		return Home{Dir: filepath.Join(xdg, "pingbus")}, nil
	}
	home, err := absolute(envUserHome, getenv(envUserHome))
	if err != nil {
		return Home{}, err
	}
	return Home{Dir: filepath.Join(home, ".config", "pingbus")}, nil
}

// Home is a PINGBUS_HOME: a member's own, or a session home (S7, S8).
type Home struct {
	Dir string
}

// SessionHome is the session home of a ccy container (S8).
func SessionHome() Home { return Home{Dir: "/tmp/pingbus-home"} }

// Bundle is team's bundle in this home (in a session home, a link onto a seat).
func (h Home) Bundle(team string) (Bundle, error) {
	if !isTeamName(team) {
		return Bundle{}, refuse(h.Dir, ClassGrammar, "not a team name")
	}
	return Bundle{Dir: filepath.Join(h.Dir, team)}, nil
}

// StopGuardPath is the stop hook's guard, shared by v1 and v2 hooks (S43).
func (h Home) StopGuardPath() string { return filepath.Join(h.Dir, "stop-guard.json") }

// GoodbyePath is the session's goodbye record (S42).
func (h Home) GoodbyePath() string { return filepath.Join(h.Dir, "goodbye.json") }

// WatchLogPath is the watcher's log.
func (h Home) WatchLogPath() string { return filepath.Join(h.Dir, "watch.log") }

// ToldPath is what this ccy session was last told (S38).
func (h Home) ToldPath() string { return filepath.Join(h.Dir, "told.json") }

// ToldLockPath is the teller's lock (L9).
func (h Home) ToldLockPath() string { return filepath.Join(h.Dir, "told.lock") }

// ToldRolesPath is the roles a cc session was last told (S38).
func (h Home) ToldRolesPath() string { return filepath.Join(h.Dir, "told-roles.json") }

// RelayLockPath is the delivery lock (L8).
func (h Home) RelayLockPath() string { return filepath.Join(h.Dir, "relay", "lock") }

// KeeperLockPath is a session home's keeper lock (L1).
func (h Home) KeeperLockPath() string { return filepath.Join(h.Dir, "keeper.lock") }

// KeeperLogPath is a session home's keeper log.
func (h Home) KeeperLogPath() string { return filepath.Join(h.Dir, "keeper.log") }

// AppliedPath is a session home's applied membership (S37).
func (h Home) AppliedPath() string { return filepath.Join(h.Dir, "applied.json") }

// ActivePath is the mark present while the keeper holds a seat (S44).
func (h Home) ActivePath() string { return filepath.Join(h.Dir, "active") }

// SettingsPath is a session home's Claude Code settings.
func (h Home) SettingsPath() string { return filepath.Join(h.Dir, "settings.json") }

// StatusLineInnerPath is the status line command the guard falls through to.
func (h Home) StatusLineInnerPath() string { return filepath.Join(h.Dir, "statusline-inner") }

// SocketRecordPath is the session's inbox socket record (S41).
func (h Home) SocketRecordPath() string { return filepath.Join(h.Dir, "socket.json") }

// DownPath is team's homeserver-down mark (S44).
func (h Home) DownPath(team string) (string, error) {
	if !isTeamName(team) {
		return "", refuse(h.Dir, ClassGrammar, "not a team name")
	}
	return filepath.Join(h.Dir, "down", team), nil
}

// InjectPath is one inject mark of the session whose key is key (S7, S13).
func (h Home) InjectPath(key, suffix string) (string, error) {
	if !injectKeyPat.MatchString(key) || !slices.Contains(injectSuffixes, suffix) {
		return "", refuse(h.Dir, ClassGrammar, "an inject mark is a 16 hex digit key and a known suffix")
	}
	return filepath.Join(h.Dir, "inject", key+"."+suffix), nil
}

// InjectKey is a session's inject key: the first 16 hex digits of SHA-256 of its socket path,
// encoded as Python's surrogatepass encodes it, which is the string's bytes (S13).
func InjectKey(socketPath string) string {
	sum := sha256.Sum256([]byte(socketPath))
	return hex.EncodeToString(sum[:])[:injectKeyHex]
}

// Bundle is a team's bundle directory: the host's member.json, token and README, and the
// member's state/ (S7, S10).
type Bundle struct {
	Dir string
}

// MemberPath is the bundle's member.json (S18).
func (b Bundle) MemberPath() string { return filepath.Join(b.Dir, "member.json") }

// TokenPath is the bundle's token (S19).
func (b Bundle) TokenPath() string { return filepath.Join(b.Dir, "token") }

// ReadmePath is the host's text for a human (S22).
func (b Bundle) ReadmePath() string { return filepath.Join(b.Dir, "README") }

// State is the bundle's state directory.
func (b Bundle) State() Team { return Team{Dir: filepath.Join(b.Dir, "state")} }

// Checkout is a checkout whose sessions hold seats (S10).
type Checkout struct {
	Dir string
}

// CCYCheckout is the checkout inside a ccy container.
func CCYCheckout() Checkout { return Checkout{Dir: "/workspace"} }

// Seat is one seat of team in this checkout.
func (c Checkout) Seat(team, seat string) (Seat, error) {
	if !isTeamName(team) || !isSeatName(seat) {
		return Seat{}, refuse(c.Dir, ClassGrammar, "not a team and seat")
	}
	return Seat{Dir: filepath.Join(c.pingbusDir(), "seats", team, seat)}, nil
}

// ReportDir is where session's keeper publishes its applied membership and conversation.
func (c Checkout) ReportDir(session string) (string, error) {
	if !sessionPattern.MatchString(session) {
		return "", refuse(c.Dir, ClassGrammar, "not a session name")
	}
	return filepath.Join(c.pingbusDir(), "sessions", session), nil
}

func (c Checkout) pingbusDir() string { return filepath.Join(c.Dir, ".claude", "ccy", "pingbus") }

// ReportApplied is a report directory's applied membership (S37).
func ReportApplied(reportDir string) string { return filepath.Join(reportDir, "applied.json") }

// ReportConversation is a report directory's conversation record (S39).
func ReportConversation(reportDir string) string {
	return filepath.Join(reportDir, "conversation.json")
}

// Seat is one seat directory: a bundle plus its lock and goodbye record.
type Seat struct {
	Dir string
}

// Bundle is the seat as a bundle.
func (s Seat) Bundle() Bundle { return Bundle(s) }

// LockPath is the seat lock (L1, S23).
func (s Seat) LockPath() string { return filepath.Join(s.Dir, "seat.lock") }

// SaidGoodbyePath is the seat's goodbye record (S24).
func (s Seat) SaidGoodbyePath() string { return filepath.Join(s.Dir, "said-goodbye.json") }

// MembershipDir is the desired membership's directory, `$PINGBUS_MEMBERSHIP` (S11):
// ErrBusOff for `off`, ErrNoMembership when unset, a refusal when relative.
func MembershipDir(getenv func(string) string) (string, error) {
	switch marker := getenv(envMembership); marker {
	case membershipOff:
		return "", ErrBusOff
	case "":
		return "", ErrNoMembership
	default:
		return absolute(envMembership, marker)
	}
}

// DesiredPath is the desired membership in its directory (S36).
func DesiredPath(dir string) string { return filepath.Join(dir, "membership.json") }

// RelaunchPath is the relaunch request beside it (S40).
func RelaunchPath(dir string) string { return filepath.Join(dir, "relaunch.json") }

// SessionsDir is the status line's session records' directory (S12): `$PINGBUS_SESSIONS_DIR`,
// else `/tmp/pingbus-sessions-<uid>`.
func SessionsDir(getenv func(string) string, uid int) (string, error) {
	if dir := getenv(envSessionsDir); dir != "" {
		return absolute(envSessionsDir, dir)
	}
	return "/tmp/pingbus-sessions-" + strconv.Itoa(uid), nil
}
