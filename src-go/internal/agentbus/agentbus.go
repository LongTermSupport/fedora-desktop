package agentbus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
)

// Root commands name their program by the absolute paths sudo.AgentBus and sudo.InstallTool:
// sudo's secure_path may not contain /usr/local, and the sudoers rule matches the exact argv.

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

// IsCommandError reports whether err is a tool that ran and exited non-zero, which has already
// copied its own stderr to the terminal.
func IsCommandError(err error) bool {
	cmdErr, ok := errors.AsType[*CommandError](err)
	return ok && cmdErr != nil
}

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

// Client calls the wrapper and the installer. Root runs the root commands: what they print
// for the person (CHANGED lines, the installer's progress) streams live to Root.Stdout, and
// their stderr to its terminal. Err receives the stderr of the commands run as the user; nil
// captures it into the CommandError.
type Client struct {
	Runner execx.Runner
	Root   *sudo.Sudo
	Err    io.Writer

	// ListSeats reads the seats of a resolved checkout top as the user uid; nil is seat.List.
	// A test's world sets it to bring its disk up to date before the real reader reads it.
	ListSeats func(top string, uid int) (seat.Listing, error)

	// uid is the user a checkout must belong to for SeatList; nil is os.Getuid.
	uid func() int
}

// WithStderr is a copy of the client whose commands' stderr goes to wrap(w) for each stream w
// it would have gone to: Err, and the terminal Root copies a failed root command's stderr to. A
// caller running calls side by side keeps each call's words apart this way. A nil Err stays nil,
// captured into the CommandError. The client itself is not changed.
func (c *Client) WithStderr(wrap func(io.Writer) io.Writer) *Client {
	out := *c
	if c.Err != nil {
		out.Err = wrap(c.Err)
	}
	if c.Root != nil && c.Root.Term != nil {
		root, terminal := *c.Root, *c.Root.Term
		terminal.Err = wrap(c.Root.Term.Err)
		root.Term = &terminal
		out.Root = &root
	}
	return &out
}

// SeatList is what "agent-bus seat list" answered in the checkout dir, read in-process by
// bus/seat ("" is the working directory's git top level, one git run; a named dir starts no
// process). A seat that cannot be read is left out and named on Err, and a refusal of the
// whole list is said there too ("agent-bus: <text>", as the wrapper's stderr carried it):
// neither is an error, both return unreadable true. What v1 crashed on is an error.
func (c *Client) SeatList(ctx context.Context, dir string) ([]Seat, bool, error) {
	listing, err := c.readSeats(ctx, dir)
	if refusal, refused := errors.AsType[*seat.RefusalError](err); refused {
		return []Seat{}, true, c.say(refusal.Msg)
	}
	if err != nil {
		return nil, false, fmt.Errorf("agent-bus seat list: %w", err)
	}
	for _, failure := range listing.Failures {
		if err := c.say(failure); err != nil {
			return nil, false, err
		}
	}
	seats := make([]Seat, 0, len(listing.Seats))
	for _, s := range listing.Seats {
		seats = append(seats, Seat{Team: s.Team, Seat: s.Seat, Held: s.Held, Handle: s.Handle})
	}
	return seats, len(listing.Failures) > 0, nil
}

// SeatChoose is "agent-bus seat choose <team>" run as the user in the working directory: the
// seats of the team this checkout can take, and the one to start on.
func (c *Client) SeatChoose(ctx context.Context, team string) (Choices, error) {
	return c.SeatChooseAs(ctx, team, "")
}

// SeatChooseAs is SeatChoose for the member type of the launcher asking ("podman" or "host");
// "" leaves it to agent-bus's default.
func (c *Client) SeatChooseAs(ctx context.Context, team, memberType string) (Choices, error) {
	return c.SeatChooseIn(ctx, "", team, memberType)
}

// SeatTake is "agent-bus seat take <list> --type=<type>" run as the user in the checkout dir: it
// creates each named seat the checkout lacks (sudo, asked first unless noPrompt) and refuses a
// held one (ErrBusy).
func (c *Client) SeatTake(ctx context.Context, dir, list, memberType string, noPrompt bool) error {
	return c.seatTake(ctx, dir, list, memberType, noPrompt)
}

// SeatTakeAllocated is SeatTake for a caller that already holds the checkout's seat
// allocation lock (ccy-teams session add), which agent-bus would otherwise wait for.
func (c *Client) SeatTakeAllocated(ctx context.Context, dir, list, memberType string, noPrompt bool) error {
	return c.seatTake(ctx, dir, list, memberType, noPrompt, "--alloc-held")
}

// SeatChooseIn is SeatChooseAs run in the checkout dir ("" is the working directory).
func (c *Client) SeatChooseIn(ctx context.Context, dir, team, memberType string) (Choices, error) {
	args := []string{"seat", "choose", team}
	if memberType != "" {
		args = append(args, "--type="+memberType)
	}
	res, err := c.userRun(ctx, dir, args...)
	if err != nil {
		return Choices{}, err
	}
	choices, err := ParseChoices(string(res.Stdout))
	if err != nil {
		return Choices{}, fmt.Errorf("agent-bus seat choose %s: %w", team, err)
	}
	return choices, nil
}

// SeatRemove is "agent-bus seat remove <ref>" (SEAT@TEAM) run as the user in dir.
func (c *Client) SeatRemove(ctx context.Context, dir, ref string) error {
	_, err := c.userRun(ctx, dir, "seat", "remove", ref)
	return err
}

// List is "agent-bus list <team>" as root: the team's people and agent members.
func (c *Client) List(ctx context.Context, team string) (Roster, error) {
	out, err := c.rootRun(ctx, wrapperName, sudo.AgentBus, "list", team)
	if err != nil {
		return Roster{}, err
	}
	roster, err := ParseRoster(out)
	if err != nil {
		return Roster{}, fmt.Errorf("agent-bus list %s: %w", team, err)
	}
	return roster, nil
}

// ParkMember is "agent-bus park-member <team> <handle>" as root; what it prints streams to Root.Stdout.
func (c *Client) ParkMember(ctx context.Context, team, handle string) error {
	return c.rootShow(ctx, wrapperName, sudo.AgentBus, "park-member", team, handle)
}

// SetRole is "agent-bus set-role <team> <handle> --role=<role>" as root; what it prints streams
// to Root.Stdout. A role that agent-bus would refuse is refused here, before sudo is asked.
func (c *Client) SetRole(ctx context.Context, team, handle, role string) error {
	if role != RoleCoordinator && role != RoleWorker {
		return fmt.Errorf("agent-bus set-role: %q is neither %s nor %s", role, RoleCoordinator, RoleWorker)
	}
	return c.rootShow(ctx, wrapperName, sudo.AgentBus, "set-role", team, handle, "--role="+role)
}

// HumanDevices is "agent-bus human devices <team> <name>" as root: the person's Element
// sessions.
func (c *Client) HumanDevices(ctx context.Context, team, name string) ([]Device, error) {
	out, err := c.rootRun(ctx, wrapperName, sudo.AgentBus, "human", "devices", team, name)
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
	out, err := c.rootRun(ctx, wrapperName, sudo.AgentBus, "human", "password", team, name)
	if err != nil {
		return "", err
	}
	password := strings.TrimRight(strings.SplitN(out, "\n", 2)[0], "\r")
	if password == "" {
		return "", fmt.Errorf("agent-bus human password %s %s printed no password", team, name)
	}
	return password, nil
}

// Backups is "agent-bus-install backups --team <team>" as root.
func (c *Client) Backups(ctx context.Context, team string) ([]Backup, error) {
	out, err := c.rootRun(ctx, installerName, sudo.InstallTool, "backups", "--team", team)
	if err != nil {
		return nil, err
	}
	backups, err := ParseBackups(out)
	if err != nil {
		return nil, fmt.Errorf("agent-bus-install backups %s: %w", team, err)
	}
	return backups, nil
}

// BackupNow is "agent-bus-install backup-now --team <team>" as root; its output streams to Root.Stdout.
func (c *Client) BackupNow(ctx context.Context, team string) error {
	return c.rootShow(ctx, installerName, sudo.InstallTool, "backup-now", "--team", team)
}

// Restore is "agent-bus-install restore --team <team> --backup <id>" as root.
func (c *Client) Restore(ctx context.Context, team, backup string) error {
	return c.rootShow(ctx, installerName, sudo.InstallTool, "restore", "--team", team, "--backup", backup)
}

// RemoveTeam is "agent-bus-install remove --team <team> [--purge]" as root: purge also
// deletes the team's data.
func (c *Client) RemoveTeam(ctx context.Context, team string, purge bool) error {
	argv := []string{sudo.InstallTool, "remove", "--team", team}
	if purge {
		argv = append(argv, "--purge")
	}
	return c.rootShow(ctx, installerName, argv...)
}

func (c *Client) readSeats(ctx context.Context, dir string) (seat.Listing, error) {
	top, err := seat.Checkout(ctx, gitx.New(c.Runner), dir)
	if err != nil {
		return seat.Listing{}, err
	}
	uid := os.Getuid
	if c.uid != nil {
		uid = c.uid
	}
	if c.ListSeats != nil {
		return c.ListSeats(top, uid())
	}
	return seat.List(top, uid())
}

// say writes one line to Err as the wrapper's stderr carried it; with no Err it is dropped.
func (c *Client) say(text string) error {
	if c.Err == nil {
		return nil
	}
	if _, err := io.WriteString(c.Err, wrapperName+": "+text+"\n"); err != nil {
		return fmt.Errorf("agent-bus seat list: %w", err)
	}
	return nil
}

func (c *Client) seatTake(ctx context.Context, dir, list, memberType string, noPrompt bool, extra ...string) error {
	args := []string{"seat", "take", list, "--type=" + memberType}
	if noPrompt {
		args = append(args, "--no-prompt")
	}
	_, err := c.userRun(ctx, dir, append(args, extra...)...)
	return err
}

func (c *Client) userRun(ctx context.Context, dir string, args ...string) (execx.Result, error) {
	res, err := c.Runner.Run(ctx, execx.Cmd{Name: wrapperName, Args: args, Dir: dir, Stderr: c.Err})
	return res, classify(wrapperName, err)
}

func (c *Client) rootRun(ctx context.Context, tool string, argv ...string) (string, error) {
	out, err := c.Root.AsRoot(ctx, argv...)
	return out, classify(tool, err)
}

// rootShow runs argv as root with its output streaming live, not captured.
func (c *Client) rootShow(ctx context.Context, tool string, argv ...string) error {
	return classify(tool, c.Root.Run(ctx, argv...))
}
