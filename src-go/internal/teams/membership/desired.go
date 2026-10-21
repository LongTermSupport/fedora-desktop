package membership

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
)

// RelaunchFile is the host's request, beside the desired file, that a session launched on no
// team relaunch once so it accepts team messages (SPEC-hot-control.md H22). The ccy supervisor's
// lifecycle plugin acts on it at the session's next idle moment; the launch after it removes it.
const RelaunchFile = "relaunch.json"

// ReasonRemoved is the one farewell reason: removed by the owner.
const ReasonRemoved = "removed"

// pingbus's bounds on a membership file (helpers/pingbus/membership.py MAX_FILE_BYTES,
// MAX_SEATS and MAX_FAREWELLS): a desired file's seats and farewells, and a report's seats,
// refusals and told roles.
const (
	maxDesiredBytes = 65536
	maxSeats        = 64
	maxFarewells    = 256
)

// isCheckout is pingbus's checkout grammar (membership.py _CHECKOUT_RE): a slash and at most
// 4095 more characters, none a control character. RE2 caps a repeat at 1000, so it is code.
func isCheckout(s string) bool {
	return strings.HasPrefix(s, "/") && characters(s) <= 4096 &&
		!strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// characters is the length of s as Python counts the str pyjson decoded it from: a lone
// surrogate, which pyjson keeps as its three-byte generalised UTF-8 form (0xED 0xA0-0xBF x),
// is one character, not three invalid bytes.
func characters(s string) int {
	n := 0
	for i := 0; i < len(s); n++ {
		if i+2 < len(s) && s[i] == 0xED && s[i+1] >= 0xA0 && s[i+1] <= 0xBF {
			i += 3
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return n
}

// Read is the desired membership in dir, refused unless it is exactly what this package
// writes (section 3.1). A missing file is os.ErrNotExist.
func Read(dir string) (Desired, error) {
	path := filepath.Join(dir, File)
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return Desired{}, fmt.Errorf("the desired membership %s: %w", path, err)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxDesiredBytes+1))
	closeErr := f.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return Desired{}, fmt.Errorf("could not read the desired membership %s: %w", path, err)
	}
	if len(data) > maxDesiredBytes {
		return Desired{}, fmt.Errorf("the desired membership %s is larger than %d bytes", path, maxDesiredBytes)
	}
	d, err := parseDesired(data)
	if err != nil {
		return Desired{}, fmt.Errorf("the desired membership %s is not what ccy-teams writes: %w", path, err)
	}
	if err := d.check(); err != nil {
		return Desired{}, fmt.Errorf("the desired membership %s: %w", path, err)
	}
	return d, nil
}

// parseDesired is section 3.1's keys and their JSON types; check judges the values.
func parseDesired(data []byte) (Desired, error) {
	doc, err := decodeDoc(data)
	if err != nil {
		return Desired{}, err
	}
	var dec decoder
	o := dec.object(doc, "", []string{"v", "session", "kind", "checkout", "generation", "written_ms", "seats", "farewell"})
	d := Desired{
		V: o.integer("v"), Session: o.str("session"), Kind: o.str("kind"), Checkout: o.str("checkout"),
		Generation: o.integer("generation"), WrittenMS: o.integer64("written_ms"),
		Seats: []Seat{}, Farewell: []Farewell{},
	}
	for _, s := range o.objects("seats", maxSeats, "team", "seat") {
		d.Seats = append(d.Seats, Seat{Team: s.str("team"), Seat: s.str("seat")})
	}
	for _, f := range o.objects("farewell", maxFarewells, "team", "seat", "reason") {
		d.Farewell = append(d.Farewell, Farewell{Team: f.str("team"), Seat: f.str("seat"), Reason: f.str("reason")})
	}
	return d, dec.err
}

func (d Desired) check() error {
	switch {
	case d.V != 1:
		return fmt.Errorf("v is %d, not 1", d.V)
	case !sessionName.MatchString(d.Session):
		return fmt.Errorf("%q is not a session name", d.Session)
	case d.Kind != "ccy" && d.Kind != "cc":
		return fmt.Errorf("kind %q is neither ccy nor cc", d.Kind)
	case !isCheckout(d.Checkout):
		return fmt.Errorf("the checkout %q is not an absolute path free of control characters", d.Checkout)
	case d.Generation < 1:
		return errors.New("the generation is not a positive integer")
	case d.WrittenMS < 0:
		return errors.New("written_ms is negative")
	}
	return d.checkEntries()
}

// checkEntries judges the seats and farewells, at most as many as pingbus takes.
func (d Desired) checkEntries() error {
	switch {
	case d.Seats == nil || d.Farewell == nil:
		return errors.New("seats or farewell is missing")
	case len(d.Seats) > maxSeats || len(d.Farewell) > maxFarewells:
		return fmt.Errorf("more than %d seats or %d farewells", maxSeats, maxFarewells)
	}
	teams := map[string]bool{}
	for _, s := range d.Seats {
		switch {
		case !seatName.MatchString(s.Seat) || !teamName.MatchString(s.Team):
			return fmt.Errorf("seats: %q@%q is not a seat of a team", s.Seat, s.Team)
		case teams[s.Team]:
			return fmt.Errorf("seats: team %s has two seats: one seat per team", s.Team)
		}
		teams[s.Team] = true
	}
	for _, f := range d.Farewell {
		if !seatName.MatchString(f.Seat) || !teamName.MatchString(f.Team) || f.Reason != ReasonRemoved {
			return fmt.Errorf("farewell %s@%s (%q) is not a seat removed by the owner", f.Seat, f.Team, f.Reason)
		}
	}
	return nil
}

// RemoveDesired drops the desired membership of a session that is no longer running: its next
// launch writes it again (SPEC-hot-control.md H28).
func RemoveDesired(dir string) error {
	return fsx.RemoveIfPresent(filepath.Join(dir, File))
}

// ValidSession is whether name is a session name a membership directory may carry.
func ValidSession(name string) bool { return sessionName.MatchString(name) }

// Canonical is seats as a --teams list, in order: <seat>@<team>,...
func Canonical(seats []Seat) string {
	parts := make([]string, len(seats))
	for i, s := range seats {
		parts[i] = s.Seat + "@" + s.Team
	}
	return strings.Join(parts, ",")
}

// Write replaces the desired membership in dir atomically, making dir and its two parents
// private to the owner.
func Write(dir string, d Desired) error {
	if err := d.checkWrite(); err != nil {
		return fmt.Errorf("refusing to write the desired membership: %w", err)
	}
	return writeDesired(dir, d)
}

// checkWrite is check for a membership about to be written, by Write or the launcher.
// encoding/json writes each byte of invalid UTF-8 as U+FFFD, so a checkout that is not UTF-8
// would be written as another checkout. That includes the lone surrogate a read file may hold
// (pyjson keeps it as generalised UTF-8): such a membership is read, but never rewritten.
func (d Desired) checkWrite() error {
	if err := d.check(); err != nil {
		return err
	}
	if !utf8.ValidString(d.Checkout) {
		return fmt.Errorf("the checkout %q is not UTF-8", d.Checkout)
	}
	return nil
}

type relaunch struct {
	V          int    `json:"v"`
	Generation int    `json:"generation"`
	WrittenMS  int64  `json:"written_ms"`
	Why        string `json:"why,omitempty"`
}

// WhyRestart is the why of the request `ccy-teams restart` writes (SPEC-team-add-and-restart
// B4); the lifecycle plugin gives it as the restart's reason. A first join's request has none.
const WhyRestart = "restart"

// WriteRelaunch asks the session whose membership is in dir to relaunch once (RelaunchFile).
func WriteRelaunch(dir string, generation int, now time.Time) error {
	return writeRelaunch(dir, relaunch{V: 1, Generation: generation, WrittenMS: now.UnixMilli()})
}

// WriteRestart asks the session whose membership is in dir to restart in place at its next
// idle moment, resuming its conversation, for a team restart (B4).
func WriteRestart(dir string, generation int, now time.Time) error {
	return writeRelaunch(dir, relaunch{V: 1, Generation: generation, WrittenMS: now.UnixMilli(), Why: WhyRestart})
}

// WithdrawRestart drops the request in dir only when it is a team restart's, and says whether
// it did: a first join's request (H22) is that join's to withdraw.
func WithdrawRestart(dir string) (bool, error) {
	path := filepath.Join(dir, RelaunchFile)
	data, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("could not read the relaunch request %s: %w", path, err)
	}
	if !isRestart(data) {
		return false, nil
	}
	if err := removeRelaunch(dir); err != nil {
		return false, err
	}
	return true, nil
}

// isRestart is whether a request's bytes are a team restart's; anything unreadable is not.
func isRestart(data []byte) bool {
	var r relaunch
	return json.Unmarshal(data, &r) == nil && r.Why == WhyRestart
}

func writeRelaunch(dir string, r relaunch) error {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("could not encode the relaunch request: %w", err)
	}
	return fsx.WriteAtomic(filepath.Join(dir, RelaunchFile), "relaunch request", append(data, '\n'), fileMode)
}

// removeRelaunch drops a relaunch request a launch has answered.
func removeRelaunch(dir string) error {
	return fsx.RemoveIfPresent(filepath.Join(dir, RelaunchFile))
}

// RelaunchPending is whether the session whose membership is in dir has a relaunch request
// it has not answered yet.
func RelaunchPending(dir string) (bool, error) {
	_, err := os.Lstat(filepath.Join(dir, RelaunchFile))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("could not check for a relaunch request: %w", err)
	}
	return true, nil
}

// WithdrawRelaunch drops a relaunch request no longer wanted (the session left its only
// team before the relaunch came).
func WithdrawRelaunch(dir string) error { return removeRelaunch(dir) }
