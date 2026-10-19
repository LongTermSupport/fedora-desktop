// Package hostcheckout says which git checkout a folder is in, as a place a host or podman
// session may run. A checkout inside an LXC container's tree (its root filesystem seen from the
// host under root's LXC path) is the container's, not the host's: a session started there would
// run an agent on the container's files from the host side, as the host user. ccy-teams and
// ccy-sessions ask here, never git alone, wherever they would treat a folder as a checkout.
package hostcheckout

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
)

// LxcConf is root's LXC configuration, world-readable; its lxc.lxcpath is where root's LXC keeps
// its containers, DefaultLxcPath (LXC's compiled-in default) when it sets none or is absent. An
// unprivileged lxc-config answers for the caller's own LXC instead, so the file is read.
const (
	LxcConf        = "/etc/lxc/lxc.conf"
	DefaultLxcPath = "/var/lib/lxc"
	lxcPathKey     = "lxc.lxcpath"
)

// Finder answers Find. The LXC path is read once, and only when a folder is in a checkout.
type Finder struct {
	Git *gitx.Git
	// ReadFile reads LxcConf (os.ReadFile).
	ReadFile func(string) ([]byte, error)

	asked    bool
	lxcPaths []string // as configured, and resolved when that differs
}

// Checkout is where a folder is: Top is its work tree's root, "" when it is in none; Container
// is the LXC container whose tree Top is in, "" for the host's own checkout.
type Checkout struct {
	Top       string
	Container string
}

// Host reports whether a host or podman session may run in the checkout.
func (c Checkout) Host() bool { return c.Top != "" && c.Container == "" }

// Refusal is why no host or podman session is offered in a container's checkout, with what to do
// instead; shown is Top as the person should read it.
func (c Checkout) Refusal(shown string) string {
	return shown + " is LXC container " + c.Container + "'s checkout, seen from the host through the container's " +
		"files: a host or podman session must not run there (it would work on the container's files as the host " +
		"user). Add the container's agent to a team with ccy-teams lxc-add <team>, or run agent-bus-claude (or ccy) " +
		"inside the container."
}

// Find is the checkout dir is in ("" is the folder the command runs in). Git that cannot run, or
// an LxcConf that cannot be read or parsed, is an error: a checkout is never taken for the
// host's when that could not be told.
func (f *Finder) Find(ctx context.Context, dir string) (Checkout, error) {
	inTree, err := f.Git.ProjectHere(ctx, dir)
	if err != nil || !inTree {
		return Checkout{}, err
	}
	top, err := f.Git.TopLevel(ctx, dir)
	if err != nil {
		return Checkout{}, err
	}
	if err := f.readLxcPath(); err != nil {
		return Checkout{}, fmt.Errorf("whether %s is an LXC container's checkout could not be told: %w", top, err)
	}
	return Checkout{Top: top, Container: f.containerOf(top)}, nil
}

func (f *Finder) readLxcPath() error {
	if f.asked {
		return nil
	}
	path, err := lxcPath(f.ReadFile)
	if err != nil {
		return err
	}
	f.lxcPaths = []string{filepath.Clean(path)}
	// A path git prints is resolved; one that does not resolve (absent, or closed to this user)
	// cannot hold a folder this user is in by any other name.
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != f.lxcPaths[0] {
		f.lxcPaths = append(f.lxcPaths, resolved)
	}
	f.asked = true
	return nil
}

// lxcPath is root's LXC path from LxcConf: its last lxc.lxcpath, else the default.
func lxcPath(read func(string) ([]byte, error)) (string, error) {
	text, err := read(LxcConf)
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultLxcPath, nil
	}
	if err != nil {
		return "", fmt.Errorf("%s could not be read: %w", LxcConf, err)
	}
	path := DefaultLxcPath
	for n, line := range strings.Split(string(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return "", fmt.Errorf("%s line %d is not KEY = VALUE: %q", LxcConf, n+1, line)
		}
		if strings.TrimSpace(key) != lxcPathKey {
			continue
		}
		path = strings.TrimSpace(value)
		if !filepath.IsAbs(path) {
			return "", fmt.Errorf("%s line %d sets %s to %q, which is not an absolute path", LxcConf, n+1, lxcPathKey, path)
		}
	}
	return path, nil
}

// containerOf is the container whose tree top is in: the first folder under an LXC path.
func (f *Finder) containerOf(top string) string {
	for _, root := range f.lxcPaths {
		rel, err := filepath.Rel(root, filepath.Clean(top))
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
			continue
		}
		return strings.Split(rel, string(filepath.Separator))[0]
	}
	return ""
}
