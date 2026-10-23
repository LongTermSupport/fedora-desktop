package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The names an LXC member's host side adds (C 3.7.5, 3.9.1, 3.9.3). An LXC member is (team,
// container, checkout, seat) and every name of its seat is made here from the entry, never
// parsed back (n3b-D2).

// ContainerName is an LXC container's name, in declare's grammar, so a path can take one.
type ContainerName struct{ s string }

// containerGrammar is one path segment that is never "." or "..", and never reads as an option.
var containerGrammar = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// ParseContainerName checks a container name.
func ParseContainerName(s string) (ContainerName, error) {
	v, err := check("container name", containerGrammar, s)
	return ContainerName{v}, err
}

// String is the container name's text.
func (c ContainerName) String() string { return c.s }

// MarshalText encodes the container name, and refuses the zero value.
func (c ContainerName) MarshalText() ([]byte, error) { return marshal("container name", c.s) }

// UnmarshalText decodes a container name through ParseContainerName.
func (c *ContainerName) UnmarshalText(b []byte) error {
	return unmarshal(c, ParseContainerName, string(b))
}

func (c ContainerName) text() string { return must("container name", c.s) }

// SeatKey is an LXC seat's key, its identity in every guest and host path (C 3.9.1).
type SeatKey struct{ s string }

var seatKeyGrammar = regexp.MustCompile(`^[0-9a-f]{16}$`)

// ParseSeatKey checks a seat key: 16 lowercase hex.
func ParseSeatKey(s string) (SeatKey, error) {
	v, err := check("seat key", seatKeyGrammar, s)
	return SeatKey{v}, err
}

// String is the seat key's text.
func (k SeatKey) String() string { return k.s }

// MarshalText encodes the seat key, and refuses the zero value.
func (k SeatKey) MarshalText() ([]byte, error) { return marshal("seat key", k.s) }

// UnmarshalText decodes a seat key through ParseSeatKey.
func (k *SeatKey) UnmarshalText(b []byte) error { return unmarshal(k, ParseSeatKey, string(b)) }

func (k SeatKey) text() string { return must("seat key", k.s) }

// MaxLXCSeat is the highest seat number an LXC entry may name (DR4); the unit name's bound
// counts its 4 digits.
const MaxLXCSeat = 9999

// ValidLXCSeat is the one seat bound: 1-MaxLXCSeat.
func ValidLXCSeat(n int) bool { return n >= 1 && n <= MaxLXCSeat }

// ValidGuestPath is the one checkout grammar (DR4), which declare, the approval gate, the seat
// key, the record and the apply hold a checkout to, and lxc-attach a home: an absolute, clean
// path below /, valid UTF-8, with no control or format character (tab, ESC, NUL, DEL, C1, a
// bidi override, a line separator), so the SECURITY line and the key's input stay one plain
// line each.
func ValidGuestPath(p string) bool {
	return filepath.IsAbs(p) && filepath.Clean(p) == p && p != "/" && utf8.ValidString(p) &&
		!strings.ContainsFunc(p, func(r rune) bool {
			return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
		})
}

// unitPathGrammar is the characters a path in a session unit, or in a shell it runs, may hold.
var unitPathGrammar = regexp.MustCompile(`^[A-Za-z0-9/._-]+$`)

// ValidUnitPath is a checkout or a home a session unit can carry (C 3.9.4): ValidGuestPath, of
// [A-Za-z0-9/._-] only, so nothing a unit file or a shell reads specially.
func ValidUnitPath(p string) bool { return ValidGuestPath(p) && unitPathGrammar.MatchString(p) }

// maxRepoLabel is the repo label's length (C 3.9.1).
const maxRepoLabel = 24

// LXCSeatKey is the seat key of (team, checkout, seat): the first 16 lowercase hex of SHA-256
// of `<team>\n<checkout>\n<seat>`, the seat in decimal. checkout and seat are declare's
// values (ValidGuestPath and ValidLXCSeat); any other panics, as a zero team does,
// since the key would name no declared seat.
func LXCSeatKey(team TeamID, checkout string, seat int) SeatKey {
	if !ValidGuestPath(checkout) {
		panic("paths: a seat key of a checkout declare refuses")
	}
	if !ValidLXCSeat(seat) {
		panic("paths: a seat key of a seat number declare refuses")
	}
	sum := sha256.Sum256([]byte(team.text() + "\n" + checkout + "\n" + strconv.Itoa(seat)))
	return SeatKey{hex.EncodeToString(sum[:8])}
}

// LXCRepoLabel is the checkout's label for people (C 3.9.1): its base name through
// NormaliseRepo, cut to 24, trailing `-` and `_` trimmed, `checkout` when nothing is left.
// Uniqueness is the key's, not the label's.
func LXCRepoLabel(checkout string) string {
	label, ok := proto.NormaliseRepo(filepath.Base(checkout))
	if !ok {
		return "checkout"
	}
	label = strings.TrimRight(label[:min(len(label), maxRepoLabel)], "-_")
	if label == "" {
		return "checkout"
	}
	return label
}

// LXCDesiredCopy is the host's copy of a seat's desired membership (C 3.9.3).
func LXCDesiredCopy(c ContainerName, k SeatKey) string {
	return AdminLXCDir + "/" + c.text() + "/" + k.text() + ".json"
}

// LXCSession is a seat's session name, lxc-<key> (C 3.9.1); `guest membership` refuses any
// other (GU4).
func LXCSession(k SeatKey) SessionName { return SessionName{"lxc-" + k.text()} }

// LXCLayout is an LXC seat's layout: its state and desired roots under
// ~/.config/teamchat/lxc/<key>, and its run root under the $XDG_RUNTIME_DIR the process
// received (the unit's GuestRuntimeDir, or a hand-run seat's login one), both of which the kit
// scan reads (LX5).
func LXCLayout(home, runtimeDir string, k SeatKey) (Layout, error) {
	if err := checkBase("home", home); err != nil {
		return Layout{}, err
	}
	if err := checkBase("runtime", runtimeDir); err != nil {
		return Layout{}, err
	}
	seat := home + "/.config/" + Product + "/lxc/" + k.text()
	return Layout{
		kitRoot:    HostKitRoot,
		currentKit: HostKitCurrent(),
		state:      seat + "/seats",
		desired:    seat + "/membership",
		runRoot:    runRootUnder(runtimeDir),
	}, nil
}
