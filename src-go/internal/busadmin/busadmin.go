// Package busadmin runs the Python half of the agent bus from the installer: the admin
// tool (render, bootstrap) through its root wrapper, and the zipapp bundler. Both stay Python
// for now; this is the one place the installer names them.
package busadmin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// Admin runs the admin wrapper at Tool. Stdout is where bootstrap's own report goes (the
// installer passes it through to its caller); Stderr is where every command's complaint goes.
type Admin struct {
	Runner execx.Runner
	Tool   string
	Stdout io.Writer
	Stderr io.Writer
}

// Check validates a team file and returns its canonical JSON. previous, when not empty, is
// the installed team.json the new file must stay compatible with. A refusal keeps the
// admin tool's exit status in the error (see execx.ExitOf).
func (a *Admin) Check(ctx context.Context, teamFile []byte, previous string) ([]byte, error) {
	args := []string{"check"}
	if previous != "" {
		args = append(args, "--previous="+previous)
	}
	return a.render(ctx, teamFile, args...)
}

// Toml renders tuwunel.toml from the canonical team JSON.
func (a *Admin) Toml(ctx context.Context, team []byte) ([]byte, error) {
	return a.render(ctx, team, "toml")
}

// Dropin renders the unit drop-in; each interface is "address=interface".
func (a *Admin) Dropin(ctx context.Context, team []byte, interfaces []string) ([]byte, error) {
	args := make([]string, 0, 1+len(interfaces))
	args = append(args, "dropin")
	for _, pair := range interfaces {
		args = append(args, "--interface="+pair)
	}
	return a.render(ctx, team, args...)
}

// Stamp renders the applied stamp of a team from the software that applied it.
func (a *Admin) Stamp(ctx context.Context, team []byte, zipapp, tuwunel, installerSHA256 string) ([]byte, error) {
	return a.render(ctx, team, "stamp", "--zipapp", zipapp, "--tuwunel", tuwunel, "--installer-sha256", installerSHA256)
}

// Bootstrap runs the account operations of a team. Its report passes through to Stdout, and
// a failure keeps the admin tool's exit status.
func (a *Admin) Bootstrap(ctx context.Context, team string) error {
	_, err := a.Runner.Run(ctx, execx.Cmd{Name: a.Tool, Args: []string{"bootstrap", team}, Stdout: a.Stdout, Stderr: a.Stderr})
	if err != nil {
		return fmt.Errorf("agent-bus bootstrap %s: %w", team, err)
	}
	return nil
}

// render runs `agent-bus render <args>` with stdin and returns what it printed.
func (a *Admin) render(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	res, err := a.Runner.Run(ctx, execx.Cmd{
		Name:   a.Tool,
		Args:   append([]string{"render"}, args...),
		Stdin:  bytes.NewReader(stdin),
		Stderr: a.Stderr,
	})
	if err != nil {
		return nil, fmt.Errorf("agent-bus render %s: %w", args[0], err)
	}
	return res.Stdout, nil
}

// Bundler builds the reproducible zipapps with helpers.pingbus.bundle.
type Bundler struct {
	Runner execx.Runner
	Stderr io.Writer
}

// pythonEnvironment is what makes an interpreter load code from somewhere the checkout does
// not name; the bundler runs as root and must not honour any of it.
var pythonEnvironment = []string{"PYTHONPATH", "PYTHONHOME", "PYTHONSTARTUP", "PYTHONUSERBASE", "PYTHONPLATLIBDIR"}

// Build runs the bundler for app from the checkout at source into the file out, and returns
// the marker line it printed (BUNDLE-CHANGED or BUNDLE-UNCHANGED, then the path).
func (b *Bundler) Build(ctx context.Context, source, out, app string) (string, error) {
	res, err := b.Runner.Run(ctx, execx.Cmd{
		Name:   "python3",
		Args:   []string{"-B", "-s", "-m", "helpers.pingbus.bundle", "--source", source, "--out", out, app},
		Dir:    source,
		Unset:  pythonEnvironment,
		Stderr: b.Stderr,
	})
	if err != nil {
		return "", fmt.Errorf("python3 -m helpers.pingbus.bundle %s: %w", app, err)
	}
	return strings.TrimRight(string(res.Stdout), "\n"), nil
}
