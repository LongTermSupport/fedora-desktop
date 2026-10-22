// Package unit is an LXC seat's session unit text (Plan 00209 SPEC-n3 SU1, C 3.9.4): one unit
// per seat, rendered from the values the host read and declared. Each value is grammar-checked
// before it reaches the text, so no value can carry a systemd specifier, a variable, a space
// that splits ExecStart, or a newline that adds a directive.
package unit

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// ErrInvalid is a value outside its grammar, or unit text with no single ExecStart.
var ErrInvalid = errors.New("unit: invalid")

// Session is a seat's session unit values, each grammar-checked by Render: the user, group and
// home the host read, and the seat's declared team, checkout and seat number, which name the
// unit and give its key.
type Session struct {
	User     paths.GuestUser
	Group    string
	Home     string
	Checkout string
	Team     paths.TeamID
	Seat     int
}

// maxSeat is declare's highest seat number (DR4).
const maxSeat = 9999

// Tmux is the tmux the unit runs, the one path the LX4 check requires of the guest.
const Tmux = "/usr/bin/tmux"

// pathGrammar is the characters a home or checkout may hold in the unit text.
var pathGrammar = regexp.MustCompile(`^[A-Za-z0-9/._-]+$`)

const execStartKey = "ExecStart="

// Render is C 3.9.4's unit text with the values filled in, byte for byte. A value outside its
// grammar is refused by name: group by the user-name grammar; home and checkout absolute,
// clean, not "/", and only [A-Za-z0-9/._-] (no space, no '%' specifier, no '$', no newline);
// a team; a seat 1-9999.
func Render(s Session) ([]byte, error) {
	if s.User.String() == "" {
		return nil, fmt.Errorf("%w user: none", ErrInvalid)
	}
	if _, err := paths.ParseGuestUser(s.Group); err != nil {
		return nil, fmt.Errorf("%w group %q: %w", ErrInvalid, s.Group, err)
	}
	if err := checkPath("home", s.Home); err != nil {
		return nil, err
	}
	if err := checkPath("checkout", s.Checkout); err != nil {
		return nil, err
	}
	if s.Team.String() == "" {
		return nil, fmt.Errorf("%w team: none", ErrInvalid)
	}
	if s.Seat < 1 || s.Seat > maxSeat {
		return nil, fmt.Errorf("%w seat %d: not 1-%d", ErrInvalid, s.Seat, maxSeat)
	}
	user := s.User.String()
	key := paths.LXCSeatKey(s.Team, s.Checkout, s.Seat)
	runtimeDir := paths.GuestRuntimeDir(key)
	seat := strconv.Itoa(s.Seat)
	execStart := strings.Join([]string{
		Tmux, "-S", paths.GuestTmuxSocket(key), "new-session", "-d", "-s",
		paths.GuestTmuxSession(s.Team, s.Checkout, s.Seat),
		paths.MemberBinaryPath, "run", "--claude", "--kind", "lxc", "--key", key.String(),
		"--resume-recorded", "--", "claude",
	}, " ")
	var b bytes.Buffer
	for _, line := range []string{
		"[Unit]",
		"Description=" + paths.Product + " session for " + user + ": team " + s.Team.String() + ", " +
			paths.LXCRepoLabel(s.Checkout) + ", seat " + seat,
		"After=network-online.target",
		"Wants=network-online.target",
		"StartLimitIntervalSec=600",
		"StartLimitBurst=6",
		"",
		"[Service]",
		"Type=forking",
		"User=" + user,
		"Group=" + s.Group,
		"WorkingDirectory=" + s.Checkout,
		"RuntimeDirectory=" + strings.TrimPrefix(runtimeDir, "/run/"),
		"RuntimeDirectoryMode=0700",
		"Environment=XDG_RUNTIME_DIR=" + runtimeDir + " HOME=" + s.Home,
		"Environment=PATH=" + s.Home + "/.local/bin:/usr/local/bin:/usr/bin:/bin",
		execStartKey + execStart,
		"TimeoutStopSec=30",
		"Restart=always",
		"RestartSec=10",
		"",
		"[Install]",
		"WantedBy=multi-user.target",
	} {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}

// checkPath holds a home or checkout to an absolute, clean path of the unit's characters.
func checkPath(name, p string) error {
	if !pathGrammar.MatchString(p) || !path.IsAbs(p) || path.Clean(p) != p || p == "/" {
		return fmt.Errorf("%w %s %q: not an absolute, clean path below / of [A-Za-z0-9/._-]", ErrInvalid, name, p)
	}
	return nil
}

// ExecStart is the unit's command line split on spaces (Render's grammar makes that exact).
func ExecStart(text []byte) ([]string, error) {
	var found []string
	for line := range strings.Lines(string(text)) {
		if v, ok := strings.CutPrefix(strings.TrimSuffix(line, "\n"), execStartKey); ok {
			found = append(found, v)
		}
	}
	if len(found) != 1 {
		return nil, fmt.Errorf("%w: %d ExecStart lines, not one", ErrInvalid, len(found))
	}
	words := strings.Split(found[0], " ")
	for _, w := range words {
		if w == "" || strings.ContainsAny(w, "\t\r") {
			return nil, fmt.Errorf("%w: ExecStart %q is not single-spaced words", ErrInvalid, found[0])
		}
	}
	return words, nil
}
