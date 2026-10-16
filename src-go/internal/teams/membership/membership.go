// Package membership is `ccy-teams internal membership-init`: the launcher's half of a
// session's desired membership (Plan 00203 SPEC-hot-control.md section 3.1, H1, H31). A ccy launch
// that carries the agent team bus kit calls it just before the container starts; it writes
// generation 1 of $XDG_RUNTIME_DIR/ccy/membership/<session>/membership.json from the launch's
// canonical --teams list (no seats for a plain launch) and prints the directory, which the
// launcher mounts read-only at /run/ccy/membership. Only the owner's tools write there; the
// container cannot. `cc --teams` and `cc --bus` call it with --kind=cc: the directory is
// $XDG_RUNTIME_DIR/pingbus/membership/<session>/, which the session reads directly (it runs as
// the owner, so it could write it too: cc's weaker authority, Decision 3), and the session's
// record is given the seats whole, as `record-seats` gives a ccy session's.
package membership

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The seat and team grammar, pingbus's own (helpers/pingbus/protocol.py HANDLE_SEAT_PATTERN and
// TEAM_NAME_PATTERN; a test pins the copy).
const (
	seatPattern = `[1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11}`
	teamPattern = `[a-z][a-z0-9-]{0,23}`
)

var (
	seatName = regexp.MustCompile(`^(?:` + seatPattern + `)$`)
	teamName = regexp.MustCompile(`^(?:` + teamPattern + `)$`)
	// sessionName is a registry name or a container name: no slash, no leading dot, nothing a
	// file name or a reader would trip on.
	sessionName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@+-]{0,254}$`)
)

// File is the desired membership's file name, in its session's directory.
const File = "membership.json"

const (
	dirMode  = 0o700
	fileMode = 0o600
	sub      = "membership-init"
	optKind  = "--kind="
)

// The two kinds of session that have a desired membership: a ccy container, and a cc session
// on the host.
const (
	KindCcy = "ccy"
	KindCC  = "cc"
)

// Seat is one seat of the desired membership.
type Seat struct {
	Team string `json:"team"`
	Seat string `json:"seat"`
}

// Farewell is the reason a removed seat was removed (empty at launch).
type Farewell struct {
	Team   string `json:"team"`
	Seat   string `json:"seat"`
	Reason string `json:"reason"`
}

// Desired is section 3.1's file, its keys in the spec's order.
type Desired struct {
	V          int        `json:"v"`
	Session    string     `json:"session"`
	Kind       string     `json:"kind"`
	Checkout   string     `json:"checkout"`
	Generation int        `json:"generation"`
	WrittenMS  int64      `json:"written_ms"`
	Seats      []Seat     `json:"seats"`
	Farewell   []Farewell `json:"farewell"`
}

// Init is the membership-init command. Now is its clock.
type Init struct {
	Host *host.Host
	Now  func() time.Time
}

// New is the command over a host, on the wall clock.
func New(h *host.Host) *Init { return &Init{Host: h, Now: time.Now} }

// Dir is a ccy session's membership directory under the runtime directory.
func Dir(runtimeDir, session string) string {
	return filepath.Join(runtimeDir, "ccy", "membership", session)
}

// CCDir is a cc session's membership directory under the runtime directory (H31).
func CCDir(runtimeDir, session string) string {
	return filepath.Join(runtimeDir, "pingbus", "membership", session)
}

// DirOf is the membership directory of a session of the kind.
func DirOf(runtimeDir, kind, session string) string {
	if kind == KindCC {
		return CCDir(runtimeDir, session)
	}
	return Dir(runtimeDir, session)
}

// ParseSeats reads a canonical --teams list (empty: no seat), at most one seat per team.
func ParseSeats(list string) ([]Seat, error) {
	seats := []Seat{}
	if list == "" {
		return seats, nil
	}
	teams := map[string]bool{}
	for i, item := range strings.Split(list, ",") {
		seat, team, ok := strings.Cut(item, "@")
		switch {
		case !ok || strings.Contains(team, "@"):
			return nil, fmt.Errorf("item %d (%q) is not <seat>@<team>", i+1, item)
		case !seatName.MatchString(seat):
			return nil, fmt.Errorf("item %d (%q): %q is not a seat name", i+1, item, seat)
		case !teamName.MatchString(team):
			return nil, fmt.Errorf("item %d (%q): %q is not a team name", i+1, item, team)
		case teams[team]:
			return nil, fmt.Errorf("team %s is named twice: one seat per team", team)
		}
		teams[team] = true
		seats = append(seats, Seat{Team: team, Seat: seat})
	}
	return seats, nil
}

// Run is `ccy-teams internal membership-init [--kind=cc] <session> <checkout> <seats>`.
func (m *Init) Run(_ context.Context, args []string) term.ExitCode {
	desired, err := m.desired(args)
	if err != nil {
		m.say("ccy-teams: internal " + sub + ": " + err.Error() + ". See ccy-teams --help.")
		return term.ExitUsage
	}
	runtimeDir := m.Host.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(runtimeDir) {
		m.say("ccy-teams: internal " + sub + ": XDG_RUNTIME_DIR is not an absolute path (" + fmt.Sprintf("%q", runtimeDir) +
			"), so there is nowhere to keep the session's membership. Launch from a desktop login session.")
		return term.ExitFailure
	}
	dir := DirOf(runtimeDir, desired.Kind, desired.Session)
	if err := writeDesired(dir, desired); err != nil {
		m.Host.ReportErr(err)
		return term.ExitFailure
	}
	if desired.Kind == KindCC {
		if err := m.recordSeats(desired); err != nil {
			m.Host.ReportErr(err)
			return term.ExitFailure
		}
	}
	// A launch answers any relaunch the host asked of the session before it (H22).
	if err := removeRelaunch(dir); err != nil {
		m.Host.ReportErr(err)
		return term.ExitFailure
	}
	m.Host.Print(m.Host.Stdout, dir+"\n")
	return term.ExitOK
}

func (m *Init) desired(args []string) (Desired, error) {
	kind := KindCcy
	if len(args) > 0 && strings.HasPrefix(args[0], "--kind") {
		given, ok := strings.CutPrefix(args[0], optKind)
		if !ok || (given != KindCcy && given != KindCC) {
			return Desired{}, fmt.Errorf("%q: the kind is --kind=ccy or --kind=cc", args[0])
		}
		kind, args = given, args[1:]
	}
	if len(args) != 3 {
		return Desired{}, errors.New("takes [--kind=cc] <session> <checkout> <seats> (the seats may be empty)")
	}
	session, checkout := args[0], args[1]
	if !sessionName.MatchString(session) {
		return Desired{}, fmt.Errorf("%q is not a session name", session)
	}
	if !filepath.IsAbs(checkout) || filepath.Clean(checkout) != checkout {
		return Desired{}, fmt.Errorf("the checkout %q is not a clean absolute path", checkout)
	}
	seats, err := ParseSeats(args[2])
	if err != nil {
		return Desired{}, fmt.Errorf("the seats %q: %w", args[2], err)
	}
	return Desired{
		V: 1, Session: session, Kind: kind, Checkout: checkout, Generation: 1,
		WrittenMS: m.Now().UnixMilli(), Seats: seats, Farewell: []Farewell{},
	}, nil
}

// writeDesired makes the directory and its parents the owner's alone, then replaces the file.
func writeDesired(dir string, desired Desired) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("could not make the membership directory %s: %w", dir, err)
	}
	for _, d := range []string{filepath.Dir(filepath.Dir(dir)), filepath.Dir(dir), dir} {
		info, err := os.Lstat(d)
		if err != nil {
			return fmt.Errorf("could not read the membership directory %s: %w", d, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory (a symlink or a file is in the way); remove it and launch again", d)
		}
		if err := os.Chmod(d, dirMode); err != nil {
			return fmt.Errorf("could not make %s private: %w", d, err)
		}
	}
	data, err := json.Marshal(desired)
	if err != nil {
		return fmt.Errorf("could not encode the membership: %w", err)
	}
	return fsx.WriteAtomic(filepath.Join(dir, File), "session membership", append(data, '\n'), fileMode)
}

// recordSeats gives a cc session's record its seats whole (what a hot change and a restore start
// from). A session with no record (cc run outside ccy's tmux) has none to give; one on no team
// keeps --bus, so a restore brings it back on the bus.
func (m *Init) recordSeats(d Desired) error {
	store, err := m.Host.Registry()
	if err != nil {
		return err
	}
	err = store.SetCCTeams(d.Session, Canonical(d.Seats))
	if errors.Is(err, registry.ErrNoRecord) {
		return nil
	}
	return err
}

func (m *Init) say(msg string) { m.Host.Print(m.Host.Stderr, msg+"\n") }
