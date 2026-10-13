// Package tmuxccy drives the tmux server dedicated to CCY (socket "ccy"), so no call can land
// on the user's default server. It lists sessions and their terminals, installs the hook
// that limits a session to one terminal, and attaches.
package tmuxccy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Socket is the tmux server name every call uses.
const Socket = "ccy"

// ErrSessionGone is the session (or the whole server) not existing.
var ErrSessionGone = errors.New("tmux session is gone")

// ErrOpenElsewhere is a session that has a terminal on it, or has gone, at the moment
// of asking to attach. It can be attached from one terminal only.
var ErrOpenElsewhere = errors.New("session is open in another terminal, or gone")

// RefusedError is an attach that was refused. It matches ErrOpenElsewhere under errors.Is,
// and its Error text is the whole message for the person, without the "ERROR: " label,
// which the caller adds by printing it with term.PrintError.
type RefusedError struct {
	Message string
}

// Error is the message for the person.
func (e *RefusedError) Error() string { return e.Message }

// Is matches ErrOpenElsewhere.
func (e *RefusedError) Is(target error) bool { return target == ErrOpenElsewhere }

// Session is one line of list-sessions.
type Session struct {
	Name     string
	Attached int
	Path     string
}

// Client is one terminal on a session: its tty and last activity (unix seconds).
type Client struct {
	TTY      string
	Activity int64
}

// Outcome is how an attach ended.
type Outcome int

const (
	// Refused means nothing was attached (the error says why).
	Refused Outcome = iota
	// Detached means the user attached and then left; the session keeps running.
	Detached
	// Ended means the session ended while attached.
	Ended
	// Kicked means another terminal was on the session afterwards, so the hook detached this one.
	Kicked
)

// Tmux is the CCY tmux server. Stdin, Stdout and Stderr are the terminal handed to
// attach-session and may be nil. Term carries the status lines, so colour follows the
// term rules; it is required.
type Tmux struct {
	Runner execx.Runner
	Term   *term.Term
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Run is tmux -L ccy <args>.
func (t *Tmux) Run(ctx context.Context, args ...string) (execx.Result, error) {
	res, err := t.Runner.Run(ctx, execx.Cmd{Name: "tmux", Args: append([]string{"-L", Socket}, args...)})
	if err != nil {
		return res, fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
	}
	return res, nil
}

// stderrOf is what a refused command wrote to stderr, which is how tmux says why.
func stderrOf(err error) string {
	if exitErr, ok := errors.AsType[*execx.ExitError](err); ok {
		return strings.TrimSpace(exitErr.Stderr)
	}
	return ""
}

func mentions(msg string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(msg, n) {
			return true
		}
	}
	return false
}

// List is every session on the server. No server yet is the normal first-run state and an
// empty list; any other failure is real.
func (t *Tmux) List(ctx context.Context) ([]Session, error) {
	res, err := t.Run(ctx, "list-sessions", "-F", "#{session_name} #{session_attached} #{session_path}")
	if err != nil {
		if mentions(stderrOf(err), "no server running", "No such file or directory") {
			return nil, nil
		}
		return nil, fmt.Errorf("tmux list-sessions failed: %w", err)
	}
	var sessions []Session
	for line := range strings.SplitSeq(string(res.Stdout), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// The path is last and may hold spaces.
		fields := strings.SplitN(line, " ", 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("tmux list-sessions: unreadable line %q", line)
		}
		attached, convErr := strconv.Atoi(fields[1])
		if convErr != nil {
			return nil, fmt.Errorf("tmux list-sessions: attached count in %q: %w", line, convErr)
		}
		sessions = append(sessions, Session{Name: fields[0], Attached: attached, Path: fields[2]})
	}
	return sessions, nil
}

// Clients are the terminals on a session, as list-clients counts them: the single-attach
// hook's own view. ErrSessionGone when the session or server is not there.
func (t *Tmux) Clients(ctx context.Context, name string) ([]Client, error) {
	res, err := t.Run(ctx, "list-clients", "-t", "="+name, "-F", "#{client_tty} #{client_activity}")
	if err != nil {
		msg := stderrOf(err)
		if mentions(msg, "can't find session", "no server running", "No such file or directory") {
			return nil, ErrSessionGone
		}
		return nil, fmt.Errorf("could not list the terminals on '%s': %w", name, err)
	}
	var clients []Client
	for line := range strings.SplitSeq(string(res.Stdout), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		tty, activity, ok := strings.Cut(line, " ")
		if !ok {
			return nil, fmt.Errorf("tmux list-clients: unreadable line %q", line)
		}
		secs, convErr := strconv.ParseInt(activity, 10, 64)
		if convErr != nil {
			return nil, fmt.Errorf("tmux list-clients: activity in %q: %w", line, convErr)
		}
		clients = append(clients, Client{TTY: tty, Activity: secs})
	}
	return clients, nil
}

// IsDetached is true only if the session exists and no terminal is on it. list-clients is
// used rather than #{session_attached}, which drops a terminal told to leave but not yet
// gone, though the hook still counts it. A session that is gone is false, not an error.
func (t *Tmux) IsDetached(ctx context.Context, name string) (bool, error) {
	clients, err := t.Clients(ctx, name)
	if errors.Is(err, ErrSessionGone) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(clients) == 0, nil
}

// SingleAttachHook is the tmux hook command that enforces one terminal per session: when
// the session then has more than one client, the one that just arrived is detached.
func SingleAttachHook() string {
	return "if-shell 'test $(tmux -L " + Socket + ` list-clients -t "#{session_name}" | wc -l) -gt 1' 'detach-client'`
}

// InstallHook sets the single-attach hook on the server.
func (t *Tmux) InstallHook(ctx context.Context) error {
	_, err := t.Run(ctx, "set-hook", "-g", "client-attached", SingleAttachHook())
	return err
}

// Attach attaches this terminal to a detached session and, when the user detaches again,
// reports the state they left it in. Attach prints its status lines through Term, but never
// an error it returns: a session with a terminal on it at the moment of asking returns a
// *RefusedError (matching ErrOpenElsewhere) and prints nothing, so the caller prints the
// error once, with term.PrintError. The one refusal that is an outcome and not an error,
// Kicked (the hook detached this terminal after arrival), is printed here with
// term.PrintError because Attach returns no error for it.
func (t *Tmux) Attach(ctx context.Context, name string) (Outcome, error) {
	if name == "" {
		return Refused, errors.New("attach requires a session name")
	}
	detached, err := t.IsDetached(ctx, name)
	if err != nil {
		return Refused, err
	}
	if !detached {
		return Refused, &RefusedError{Message: "'" + name + "' is open in another terminal, or gone. It can be attached from one terminal only."}
	}
	if hookErr := t.InstallHook(ctx); hookErr != nil {
		return Refused, hookErr
	}
	t.Term.Printf("Attaching to '%s'.\n", name)
	_, err = t.Runner.Run(ctx, execx.Cmd{
		Name: "tmux", Args: []string{"-L", Socket, "attach-session", "-t", "=" + name},
		Stdin: t.Stdin, Stdout: t.Stdout, Stderr: t.Stderr,
	})
	if err != nil {
		return Refused, fmt.Errorf("could not attach to '%s': %w", name, err)
	}
	clients, err := t.Clients(ctx, name)
	if errors.Is(err, ErrSessionGone) {
		t.Term.Printf("'%s' has ended.\n", name)
		return Ended, nil
	}
	if err != nil {
		return Refused, err
	}
	if len(clients) > 0 {
		t.Term.PrintError("'" + name + "' is open in another terminal, so this terminal was not attached to it.")
		return Kicked, nil
	}
	t.Term.Printf("Detached. '%s' keeps running; run ccy in its project directory to return to it.\n", name)
	return Detached, nil
}
