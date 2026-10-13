package agentbus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The two programs sudo is allowed to run for this tool. They are named by absolute path
// because sudo's secure_path may not contain /usr/local, and the sudoers rule matches the
// exact argv.
const (
	WrapperPath   = "/usr/local/bin/agent-bus"
	InstallerPath = "/usr/local/sbin/agent-bus-install"
)

const (
	wrapperName   = "agent-bus"
	installerName = "agent-bus-install"
)

// Exit kinds of the wrapper (0 aside) and the installer. A CommandError matches the kind of its
// exit code under errors.Is.
var (
	ErrFailed      = errors.New("failed")                         // 1, or any code not listed
	ErrUsage       = errors.New("usage mistake")                  // 64
	ErrUnreachable = errors.New("homeserver unreachable")         // 69
	ErrRefused     = errors.New("refused")                        // 70
	ErrBusy        = errors.New("busy")                           // 75
	ErrNotRoot     = errors.New("needs root, or run as root")     // 77
	ErrConfig      = errors.New("configuration missing or wrong") // 78
)

// kindOf maps an exit code to its kind.
func kindOf(code int) error {
	switch code {
	case 64:
		return ErrUsage
	case 69:
		return ErrUnreachable
	case 70:
		return ErrRefused
	case 75:
		return ErrBusy
	case 77:
		return ErrNotRoot
	case 78:
		return ErrConfig
	default:
		return ErrFailed
	}
}

// CommandError is a wrapper or installer run that exited non-zero. It unwraps to the runner's
// own error, so a caller can still reach whatever the sudo layer attached to it.
type CommandError struct {
	Tool   string
	Code   int
	Stderr string
	cause  error
}

// Error implements error.
func (f *CommandError) Error() string {
	msg := fmt.Sprintf("%s exited %d", f.Tool, f.Code)
	if s := strings.TrimSpace(f.Stderr); s != "" {
		msg += ": " + s
	}
	return msg
}

// Is matches the kind of the exit code.
func (f *CommandError) Is(target error) bool { return target == kindOf(f.Code) }

// Unwrap returns the runner's error.
func (f *CommandError) Unwrap() error { return f.cause }

// classify turns a runner error into a CommandError when the program ran and exited non-zero;
// a program that could not start stays the plain wrapped error.
func classify(tool string, err error) error {
	if err == nil {
		return nil
	}
	if exit, ok := errors.AsType[*execx.ExitError](err); ok {
		return &CommandError{Tool: tool, Code: exit.Code, Stderr: exit.Stderr, cause: err}
	}
	return fmt.Errorf("%s: %w", tool, err)
}

// AsRoot runs argv (the program by absolute path, then its arguments) as root without a
// password prompt and returns its stdout. It is supplied by the caller's sudo layer.
type AsRoot func(ctx context.Context, argv ...string) ([]byte, error)

// Client calls the wrapper and the installer. Out receives what the root commands print for
// the person (CHANGED lines, the installer's progress); Err receives the stderr of the
// commands run as the user. Nil discards Out and captures Err into the CommandError.
type Client struct {
	Runner execx.Runner
	AsRoot AsRoot
	Out    io.Writer
	Err    io.Writer
}

// SeatList is "agent-bus seat list" run as the user in the checkout dir. The wrapper exits
// 78 when one seat is unreadable but the others were still printed: that is not an error,
// it returns the seats it did print and unreadable true.
func (c *Client) SeatList(ctx context.Context, dir string) ([]Seat, bool, error) {
	res, runErr := c.userRun(ctx, dir, "seat", "list")
	unreadable := false
	if runErr != nil {
		if !errors.Is(runErr, ErrConfig) {
			return nil, false, runErr
		}
		unreadable = true
	}
	seats, err := ParseSeats(res.Stdout)
	if err != nil {
		return nil, false, fmt.Errorf("agent-bus seat list: %w", err)
	}
	return seats, unreadable, nil
}

// SeatRemove is "agent-bus seat remove <ref>" (SEAT@TEAM) run as the user in dir.
func (c *Client) SeatRemove(ctx context.Context, dir, ref string) error {
	_, err := c.userRun(ctx, dir, "seat", "remove", ref)
	return err
}

// List is "agent-bus list <team>" as root: the team's people and agent members.
func (c *Client) List(ctx context.Context, team string) (Roster, error) {
	out, err := c.rootRun(ctx, wrapperName, WrapperPath, "list", team)
	if err != nil {
		return Roster{}, err
	}
	roster, err := ParseRoster(out)
	if err != nil {
		return Roster{}, fmt.Errorf("agent-bus list %s: %w", team, err)
	}
	return roster, nil
}

// ParkMember is "agent-bus park-member <team> <handle>" as root; what it prints goes to Out.
func (c *Client) ParkMember(ctx context.Context, team, handle string) error {
	return c.rootShow(ctx, wrapperName, WrapperPath, "park-member", team, handle)
}

// HumanDevices is "agent-bus human devices <team> <name>" as root: the person's Element
// sessions.
func (c *Client) HumanDevices(ctx context.Context, team, name string) ([]Device, error) {
	out, err := c.rootRun(ctx, wrapperName, WrapperPath, "human", "devices", team, name)
	if err != nil {
		return nil, err
	}
	devices, err := ParseDevices(out)
	if err != nil {
		return nil, fmt.Errorf("agent-bus human devices %s %s: %w", team, name, err)
	}
	return devices, nil
}

// HumanPassword is "agent-bus human password <team> <name>" as root: it sets a new Element
// password and prints it as the one line of output.
func (c *Client) HumanPassword(ctx context.Context, team, name string) (string, error) {
	out, err := c.rootRun(ctx, wrapperName, WrapperPath, "human", "password", team, name)
	if err != nil {
		return "", err
	}
	password := strings.TrimRight(strings.SplitN(string(out), "\n", 2)[0], "\r")
	if password == "" {
		return "", fmt.Errorf("agent-bus human password %s %s printed no password", team, name)
	}
	return password, nil
}

// Backups is "agent-bus-install backups --team <team>" as root.
func (c *Client) Backups(ctx context.Context, team string) ([]Backup, error) {
	out, err := c.rootRun(ctx, installerName, InstallerPath, "backups", "--team", team)
	if err != nil {
		return nil, err
	}
	backups, err := ParseBackups(out)
	if err != nil {
		return nil, fmt.Errorf("agent-bus-install backups %s: %w", team, err)
	}
	return backups, nil
}

// BackupNow is "agent-bus-install backup-now --team <team>" as root; its output goes to Out.
func (c *Client) BackupNow(ctx context.Context, team string) error {
	return c.rootShow(ctx, installerName, InstallerPath, "backup-now", "--team", team)
}

// Restore is "agent-bus-install restore --team <team> --backup <id>" as root.
func (c *Client) Restore(ctx context.Context, team, backup string) error {
	return c.rootShow(ctx, installerName, InstallerPath, "restore", "--team", team, "--backup", backup)
}

// RemoveTeam is "agent-bus-install remove --team <team> [--purge]" as root: purge also
// deletes the team's data.
func (c *Client) RemoveTeam(ctx context.Context, team string, purge bool) error {
	argv := []string{InstallerPath, "remove", "--team", team}
	if purge {
		argv = append(argv, "--purge")
	}
	return c.rootShow(ctx, installerName, argv...)
}

func (c *Client) userRun(ctx context.Context, dir string, args ...string) (execx.Result, error) {
	res, err := c.Runner.Run(ctx, execx.Cmd{Name: wrapperName, Args: args, Dir: dir, Stderr: c.Err})
	return res, classify(wrapperName, err)
}

func (c *Client) rootRun(ctx context.Context, tool string, argv ...string) ([]byte, error) {
	out, err := c.AsRoot(ctx, argv...)
	return out, classify(tool, err)
}

// rootShow runs argv as root and writes what it printed to Out.
func (c *Client) rootShow(ctx context.Context, tool string, argv ...string) error {
	out, err := c.rootRun(ctx, tool, argv...)
	if c.Out != nil {
		if _, werr := c.Out.Write(out); werr != nil {
			return fmt.Errorf("%s output: %w", tool, werr)
		}
	}
	return err
}
