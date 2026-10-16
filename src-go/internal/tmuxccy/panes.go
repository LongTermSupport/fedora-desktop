package tmuxccy

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Pane is one pane of a session: the session it is in and the pid of the process it runs.
type Pane struct {
	Session string
	PID     int
}

// Panes is every pane of every session on the server, in tmux's order. No server yet is the
// normal first-run state and an empty list; any other failure is real.
func (t *Tmux) Panes(ctx context.Context) ([]Pane, error) {
	res, err := t.Run(ctx, "list-panes", "-a", "-F", "#{session_name} #{pane_pid}")
	if err != nil {
		if mentions(stderrOf(err), "no server running", "No such file or directory") {
			return nil, nil
		}
		return nil, fmt.Errorf("tmux list-panes failed: %w", err)
	}
	var panes []Pane
	for line := range strings.SplitSeq(string(res.Stdout), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// The pid is last; the session name before it may hold a space.
		cut := strings.LastIndex(line, " ")
		if cut < 0 {
			return nil, fmt.Errorf("tmux list-panes: unreadable line %q", line)
		}
		pid, convErr := strconv.Atoi(line[cut+1:])
		if convErr != nil {
			return nil, fmt.Errorf("tmux list-panes: pid in %q: %w", line, convErr)
		}
		panes = append(panes, Pane{Session: line[:cut], PID: pid})
	}
	return panes, nil
}

// paneTarget is the target of a pane command on a session. The trailing colon is required: real
// tmux refuses the bare =name form for pane commands ("can't find pane").
func paneTarget(name string) string { return "=" + name + ":" }

// CapturePane is the visible text of the session's pane, with its escape sequences when
// escapes is set (capture-pane -e).
func (t *Tmux) CapturePane(ctx context.Context, name string, escapes bool) (string, error) {
	args := []string{"capture-pane", "-p"}
	if escapes {
		args = append(args, "-e")
	}
	res, err := t.Run(ctx, append(args, "-t", paneTarget(name))...)
	if err != nil {
		return "", fmt.Errorf("the screen of %s could not be read: %s", name, reason(err))
	}
	return string(res.Stdout), nil
}

// SendKeys types text into the session's pane literally (send-keys -l).
func (t *Tmux) SendKeys(ctx context.Context, name, text string) error {
	if _, err := t.Run(ctx, "send-keys", "-t", paneTarget(name), "-l", text); err != nil {
		return fmt.Errorf("typing into %s failed: %s", name, reason(err))
	}
	return nil
}

// SendEnter presses Enter in the session's pane.
func (t *Tmux) SendEnter(ctx context.Context, name string) error {
	if _, err := t.Run(ctx, "send-keys", "-t", paneTarget(name), "Enter"); err != nil {
		return fmt.Errorf("pressing Enter in %s failed: %s", name, reason(err))
	}
	return nil
}

// SendEscape presses Escape in the session's pane: how a person interrupts claude's turn.
func (t *Tmux) SendEscape(ctx context.Context, name string) error {
	if _, err := t.Run(ctx, "send-keys", "-t", paneTarget(name), "Escape"); err != nil {
		return fmt.Errorf("pressing Escape in %s failed: %s", name, reason(err))
	}
	return nil
}

// KillSession ends the session and everything running in it. A session that is already gone is
// ErrSessionGone, so a caller can treat it as ended rather than as a failure.
func (t *Tmux) KillSession(ctx context.Context, name string) error {
	_, err := t.Run(ctx, "kill-session", "-t", "="+name)
	if err == nil {
		return nil
	}
	if mentions(stderrOf(err), "can't find session", "no server running", "No such file or directory") {
		return ErrSessionGone
	}
	return fmt.Errorf("could not end '%s': %w", name, err)
}

// reason is what a refused tmux command said, or the error itself when it said nothing.
func reason(err error) string {
	if msg := stderrOf(err); msg != "" {
		return msg
	}
	return err.Error()
}

func (t *Tmux) socket() string {
	if t.Socket == "" {
		return Socket
	}
	return t.Socket
}

func (t *Tmux) now() time.Time {
	if t.Now == nil {
		return time.Now()
	}
	return t.Now()
}

func (t *Tmux) pause(d time.Duration) {
	if t.Sleep == nil {
		time.Sleep(d)
		return
	}
	t.Sleep(d)
}

// requireTool is nil when the tool is on PATH.
func (t *Tmux) requireTool(name string) error {
	look := exec.LookPath
	if t.LookPath != nil {
		look = t.LookPath
	}
	if _, err := look(name); err != nil {
		return fmt.Errorf("find %s: %w", name, err)
	}
	return nil
}
