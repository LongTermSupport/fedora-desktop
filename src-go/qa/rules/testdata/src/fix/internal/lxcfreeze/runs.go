package lxcfreeze

import (
	"context"

	"fix/internal/sudo"
)

// The owner runs its own tools through the sudo seam, and what lxc-attach runs inside the
// container is judged as the program it is.

func ownLs(ctx context.Context, s *sudo.Sudo) (string, error) {
	return s.Capture(ctx, "lxc-ls", "-1")
}

func ownNmcliInside(ctx context.Context, s *sudo.Sudo, name string) (string, error) {
	return s.Capture(ctx, "lxc-attach", "-n", name, "--", "nmcli", "device", "status")
}

func insideOther(ctx context.Context, s *sudo.Sudo, name string) (string, error) {
	return s.Capture(ctx, "lxc-attach", "-n", name, "--", "git", "log") // want `"git" is owned by internal/gitx`
}

func insideShell(ctx context.Context, s *sudo.Sudo, name string) (string, error) {
	return s.Capture(ctx, "lxc-attach", "-n", name, "--", "sh", "-c", "x") // want `sh -c is a shell string`
}

func insideHidden(ctx context.Context, s *sudo.Sudo, name, prog string) (string, error) {
	return s.Capture(ctx, "lxc-attach", "-n", name, "--", prog) // want `cannot see which program "lxc-attach" runs: its arguments are not a literal`
}

func insideNothing(ctx context.Context, s *sudo.Sudo) (string, error) {
	return s.Capture(ctx, "lxc-attach", "-n", "c") // want `cannot see which program "lxc-attach" runs: its arguments end`
}
