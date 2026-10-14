// Package systemd is the tools' view of systemd: the agent-bus homeserver units, and the
// launch of Element in a scope of its own.
package systemd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const (
	unitPrefix = "agent-bus-hs@"
	unitSuffix = ".service"
	tailLines  = 5

	systemctlBin = "systemctl"
)

// HomeserverUp is HomeserverState's answer for a homeserver that is running; any other answer
// is "down (<systemd's word>)".
const HomeserverUp = "up"

// Systemd runs systemctl and systemd-run through a Runner.
type Systemd struct {
	Runner execx.Runner
}

// Unit is one line of "systemctl list-units": the unit, its load, active and sub states,
// and the description (the rest of the line).
type Unit struct {
	Name, Team, Load, Active, Sub, Description string
}

// CanReboot asks logind whether this account may reboot with nobody to authenticate. It is
// busctl's reply as printed (s "yes"), together with busctl's error and its words on failure.
func (s *Systemd) CanReboot(ctx context.Context) (string, error) {
	return execx.Combined(ctx, s.Runner, execx.Cmd{Name: "busctl", Args: []string{
		"--system", "call", "org.freedesktop.login1", "/org/freedesktop/login1",
		"org.freedesktop.login1.Manager", "CanReboot",
	}})
}

// Reboot asks systemd to reboot, streaming systemctl's own words to out. The runner's error
// comes back as it is, so a caller can read the exit code of a refusal.
func (s *Systemd) Reboot(ctx context.Context, out io.Writer) error {
	_, err := s.Runner.Run(ctx, execx.Cmd{Name: systemctlBin, Args: []string{"reboot"}, Stdout: out, Stderr: out})
	return err
}

// HomeserverUnits lists every agent-bus-hs@* service unit systemd knows, loaded or not.
func (s *Systemd) HomeserverUnits(ctx context.Context) ([]Unit, error) {
	res, err := s.Runner.Run(ctx, execx.Cmd{Name: systemctlBin, Args: []string{
		"list-units", "--all", "--plain", "--no-legend", "--type=service", unitPrefix + "*",
	}})
	if err != nil {
		return nil, fmt.Errorf("systemctl could not list the agent-bus homeserver units: %w", err)
	}
	var units []Unit
	for line := range strings.SplitSeq(string(res.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], unitPrefix) || !strings.HasSuffix(fields[0], unitSuffix) {
			continue
		}
		u := Unit{Name: fields[0], Team: strings.TrimSuffix(strings.TrimPrefix(fields[0], unitPrefix), unitSuffix)}
		rest := []*string{&u.Load, &u.Active, &u.Sub}
		for i, field := range fields[1:] {
			if i >= len(rest) {
				u.Description = strings.Join(fields[1+len(rest):], " ")
				break
			}
			*rest[i] = field
		}
		units = append(units, u)
	}
	return units, nil
}

// HomeserverState is "up", or "down (<systemd's word>)". A non-zero is-active exit is the
// answer, not a failure; only a systemctl that could not run is an error.
func (s *Systemd) HomeserverState(ctx context.Context, team string) (string, error) {
	res, err := s.Runner.Run(ctx, execx.Cmd{Name: systemctlBin, Args: []string{"is-active", unitPrefix + team + unitSuffix}})
	state := strings.TrimSpace(string(res.Stdout))
	if err != nil {
		if exitErr, ok := errors.AsType[*execx.ExitError](err); !ok || exitErr == nil {
			return "", err
		}
	}
	return StateWord(state), nil
}

// StateWord is HomeserverState's answer for the word systemd gives a unit's active state (the
// ACTIVE column of list-units, or what is-active prints): "up" for active, else the word in
// "down (...)". A list-units pass already carries it for every unit, so a caller that has one
// does not ask is-active once more per team.
func StateWord(active string) string {
	switch active {
	case "active":
		return HomeserverUp
	case "":
		return "down (unknown)"
	}
	return "down (" + active + ")"
}

// ElementLogPath is where a team's Element messages go: under stateHome (XDG_STATE_HOME)
// or ~/.local/state when that is empty.
func ElementLogPath(stateHome, home, team string) string {
	if stateHome == "" {
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "ccy-teams", "element-"+team+".log")
}

// Launch is one start of a team's Element. PID makes the scope's unit name unique per run
// of the tool.
type Launch struct {
	Team    string
	Desktop string // the team's Element launcher (.desktop file)
	PID     int
	Log     string
}

// LaunchError is a failed Element launch, with the end of the log for the person to see.
// When the log could not be read for its tail, Tail is empty and TailErr says why.
type LaunchError struct {
	Log     string
	Tail    string
	TailErr error
	Err     error
}

func (e *LaunchError) Error() string {
	msg := fmt.Sprintf("Element did not start; its messages are in %s: %v", e.Log, e.Err)
	if e.TailErr != nil {
		msg += fmt.Sprintf(" (and the log could not be read: %v)", e.TailErr)
	}
	return msg
}

// Unwrap exposes the underlying failure.
func (e *LaunchError) Unwrap() error { return e.Err }

// LaunchElement starts Element as the desktop would, independent of this terminal: in its
// own user scope (closing a terminal tab ends everything in the tab's scope) and its own
// session (no hangup), with its chatter (Electron logs a great deal) appended to the log
// file, not among the picker's lines. gio returns once Element is started, so a non-zero
// exit is a failed launch.
func (s *Systemd) LaunchElement(ctx context.Context, l Launch) error {
	if err := os.MkdirAll(filepath.Dir(l.Log), 0o750); err != nil {
		return fmt.Errorf("create the Element log directory: %w", err)
	}
	logFile, err := os.OpenFile(l.Log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open the Element log: %w", err)
	}
	_, runErr := s.Runner.Run(ctx, execx.Cmd{
		Name: "systemd-run",
		Args: []string{
			"--user", "--scope", "--quiet", "--collect",
			"--unit", "ccy-teams-element-" + l.Team + "-" + strconv.Itoa(l.PID),
			"--description", "Element for agent team " + l.Team, "--",
			"setsid", "-w", "gio", "launch", l.Desktop,
		},
		Stdout: logFile,
		Stderr: logFile,
	})
	if closeErr := logFile.Close(); closeErr != nil {
		return fmt.Errorf("close the Element log: %w", closeErr)
	}
	if runErr == nil {
		return nil
	}
	tail, tailErr := logTail(l.Log)
	return &LaunchError{Log: l.Log, Tail: tail, TailErr: tailErr, Err: runErr}
}

// logTail is the last few lines of the log, or why they could not be read.
func logTail(path string) (string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("read the Element log: %w", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > tailLines {
		lines = lines[len(lines)-tailLines:]
	}
	return strings.Join(lines, "\n"), nil
}
