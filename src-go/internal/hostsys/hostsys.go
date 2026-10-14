// Package hostsys is the installer's view of the host's system tools: rpm and dnf for the
// packages, getent and useradd for the service account, uname for the architecture and zstd
// for the release asset.
package hostsys

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// notFound is getent's exit status for a key that is not in the database.
const notFound = 2

// Sys runs the system tools through a Runner. Chatter receives what dnf, useradd and zstd
// print (nil keeps it for the error text).
type Sys struct {
	Runner  execx.Runner
	Chatter io.Writer
}

// Missing is the packages, in order, that rpm does not have installed. Any status of rpm
// is an answer; only an rpm that cannot run is an error.
func (s *Sys) Missing(ctx context.Context, packages []string) ([]string, error) {
	var missing []string
	for _, pkg := range packages {
		_, err := s.Runner.Run(ctx, execx.Cmd{Name: "rpm", Args: []string{"-q", "--quiet", pkg}})
		if err == nil {
			continue
		}
		if _, refused := execx.ExitOf(err); !refused {
			return nil, fmt.Errorf("rpm -q %s: %w", pkg, err)
		}
		missing = append(missing, pkg)
	}
	return missing, nil
}

// Install installs the packages with one dnf run.
func (s *Sys) Install(ctx context.Context, packages []string) error {
	return s.run(ctx, execx.Cmd{Name: "dnf", Args: append([]string{"install", "-y"}, packages...)})
}

// UserExists is whether the account is in the password database.
func (s *Sys) UserExists(ctx context.Context, name string) (bool, error) {
	_, err := s.Runner.Run(ctx, execx.Cmd{Name: "getent", Args: []string{"passwd", name}})
	if err == nil {
		return true, nil
	}
	if code, refused := execx.ExitOf(err); refused && code == notFound {
		return false, nil
	}
	return false, fmt.Errorf("getent passwd %s: %w", name, err)
}

// AddServiceUser creates a system account with its own group, no login shell and no home
// directory created.
func (s *Sys) AddServiceUser(ctx context.Context, name, home, comment string) error {
	return s.run(ctx, execx.Cmd{Name: "useradd", Args: []string{
		"--system", "--user-group", "--no-create-home",
		"--home-dir", home, "--shell", "/usr/sbin/nologin", "--comment", comment, name,
	}})
}

// Machine is the hardware name (`uname -m`).
func (s *Sys) Machine(ctx context.Context) (string, error) {
	res, err := s.Runner.Run(ctx, execx.Cmd{Name: "uname", Args: []string{"-m"}})
	if err != nil {
		return "", fmt.Errorf("uname -m: %w", err)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// Decompress writes the zstd file in as out, replacing out.
func (s *Sys) Decompress(ctx context.Context, in, out string) error {
	return s.run(ctx, execx.Cmd{Name: "zstd", Args: []string{"-q", "-d", "-f", "-o", out, "--", in}})
}

func (s *Sys) run(ctx context.Context, cmd execx.Cmd) error {
	cmd.Stdout, cmd.Stderr = s.Chatter, s.Chatter
	_, err := s.Runner.Run(ctx, cmd)
	if err != nil {
		return fmt.Errorf("%s %s: %w", cmd.Name, strings.Join(cmd.Args, " "), err)
	}
	return nil
}
