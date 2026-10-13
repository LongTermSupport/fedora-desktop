package tmuxccy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
)

// StartSpec is a session to start detached: its name, working directory, the command its pane
// runs (the launcher and its arguments, including any `env VAR=value` in front), and the
// registry record its trampoline removes when the launcher returns, with the launcher prefix
// (ccy or cc) its hold line names. Pid is the process the scope's unit name carries; 0 means
// this process.
type StartSpec struct {
	Name       string
	Dir        string
	Command    []string
	RecordPath string
	Prefix     string
	Pid        int
}

func (s StartSpec) validate() error {
	switch {
	case s.Name == "":
		return errors.New("start requires a session name")
	case s.Dir == "":
		return errors.New("start requires a directory")
	case len(s.Command) == 0:
		return errors.New("start requires a launcher")
	case s.RecordPath == "":
		return errors.New("start requires the session's record path")
	case s.Prefix == "":
		return errors.New("start requires a launcher prefix")
	}
	return nil
}

// StartDetached starts a tmux session on the server running spec.Command in spec.Dir, detached,
// and returns. Shared by the interactive start and by the boot-time restore, so both produce the
// same session: same scope, same hook, same trampoline, and therefore the same record lifecycle.
//
// The scope keeps the server out of the caller's cgroup (a terminal tab's, or the restore
// unit's, which would otherwise take the server with it when the oneshot ended); --collect lets
// systemd forget the scope once empty, whatever its exit status. The session is created
// detached and the single-attach hook installed in one server round trip, so no client can
// reach the session before the hook exists. The pane runs the registry's trampoline.
func (t *Tmux) StartDetached(ctx context.Context, spec StartSpec) error {
	if err := spec.validate(); err != nil {
		return err
	}
	if err := t.requireTool(tmuxBinary); err != nil {
		return fmt.Errorf("tmux is not installed, so session '%s' cannot be started. Deploy it with playbooks/imports/play-tmux-sessions.yml (part of playbook-main.yml). (%w)", spec.Name, err)
	}
	for _, tool := range []string{"systemd-run", "systemd-escape"} {
		if err := t.requireTool(tool); err != nil {
			return fmt.Errorf("%s is not available, so session '%s' cannot be started. It ships with systemd, which this host is expected to run; a user manager is what keeps the session out of the terminal's cgroup: %w", tool, spec.Name, err)
		}
	}
	escaped, err := execx.Combined(ctx, t.Runner, execx.Cmd{Name: "systemd-escape", Args: []string{"--", spec.Name}})
	if err != nil {
		return fmt.Errorf("could not escape the session name '%s' for a unit name: %s: %w", spec.Name, strings.TrimSpace(escaped), err)
	}
	pid := spec.Pid
	if pid == 0 {
		pid = os.Getpid()
	}
	// The unit name carries the session name (escaped) as well as the pid: a restore starts
	// several sessions from one process, and two scopes may not share a name.
	args := slices.Concat([]string{
		"--user", "--scope", "--quiet", "--collect",
		"--unit", fmt.Sprintf("ccy-tmux-%d-%s", pid, strings.TrimRight(escaped, "\n")),
		"--description", "CCY tmux session " + spec.Name,
		"--", tmuxBinary, "-L", t.socket(),
		"new-session", "-d", "-s", spec.Name, "-c", spec.Dir, "--",
		"bash", "-c", registry.Trampoline(spec.RecordPath, spec.Prefix), "ccy-tmux",
	}, spec.Command, []string{";", "set-hook", "-g", "client-attached", SingleAttachHookFor(t.socket())})
	out, err := execx.Combined(ctx, t.Runner, execx.Cmd{Name: "systemd-run", Args: args})
	if err != nil {
		return fmt.Errorf("could not start session '%s': %s: %w", spec.Name, strings.TrimSpace(out), err)
	}
	return nil
}
