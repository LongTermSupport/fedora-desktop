package badrun

import (
	"context"

	"fix/internal/sudo"
)

// A package that does not own the lxc tools or nmcli runs them as root through the sudo
// seam, which builds no execx.Cmd here: the seam's methods are judged like a Cmd.

func ls(ctx context.Context, s *sudo.Sudo) (string, error) {
	return s.Capture(ctx, "lxc-ls", "-1") // want `"lxc-ls" is owned by internal/lxcfreeze`
}

func freezeIt(ctx context.Context, s *sudo.Sudo, name string) (string, error) {
	return s.AsRoot(ctx, "lxc-freeze", "-n", name) // want `"lxc-freeze" is owned by internal/lxcfreeze`
}

func nmcliRun(ctx context.Context, s *sudo.Sudo) error {
	return s.Run(ctx, "nmcli", "device", "status") // want `"nmcli" is owned by internal/lxcfreeze`
}

func attach(ctx context.Context, s *sudo.Sudo, name string) (string, error) {
	return s.Capture(ctx, "lxc-attach", "-n", name, "--", "nmcli", "device") // want `"lxc-attach" is owned by internal/lxcfreeze`
}

func byPath(ctx context.Context, s *sudo.Sudo) (string, error) {
	return s.Capture(ctx, "/usr/sbin/lxc-info", "-n", "c") // want `"lxc-info" is owned by internal/lxcfreeze`
}

func wrapped(ctx context.Context, s *sudo.Sudo) (string, error) {
	return s.Capture(ctx, "env", "A=1", "git", "log") // want `"git" is owned by internal/gitx`
}

func shellString(ctx context.Context, s *sudo.Sudo) (string, error) {
	return s.Capture(ctx, "sh", "-c", "lxc-ls") // want `sh -c is a shell string`
}

func chosenAtRunTime(ctx context.Context, s *sudo.Sudo, prog string) (string, error) {
	return s.Capture(ctx, prog, "-1") // want `Name is not a compile-time constant`
}

func spreadParameter(ctx context.Context, s *sudo.Sudo, argv ...string) (string, error) {
	return s.Capture(ctx, argv...) // want `Name is not a compile-time constant`
}

func spreadWithLiteralHead(ctx context.Context, s *sudo.Sudo, rest []string) (string, error) {
	return s.Capture(ctx, append([]string{"nmcli"}, rest...)...) // want `"nmcli" is owned by internal/lxcfreeze`
}

func noProgram(ctx context.Context, s *sudo.Sudo) (string, error) {
	return s.Capture(ctx) // want `names no program`
}

// A method value hides the program from every later call, so it is flagged where it is taken.
func methodValue(ctx context.Context, s *sudo.Sudo) (string, error) {
	f := s.Capture // want `sudo seam method used as a value`
	return f(ctx, "lxc-ls")
}

func methodExpression(ctx context.Context, s *sudo.Sudo) (string, error) {
	return (*sudo.Sudo).Capture(s, ctx, "lxc-ls") // want `sudo seam method used as a value`
}

// Unowned programs are free, and so is the agent-bus path.
func clean(ctx context.Context, s *sudo.Sudo) error {
	if _, err := s.Capture(ctx, "ls", "-l"); err != nil {
		return err
	}
	if _, err := s.AsRoot(ctx, "/usr/local/bin/agent-bus", "list"); err != nil {
		return err
	}
	return s.Run(ctx, "env", "A=1", "id")
}
