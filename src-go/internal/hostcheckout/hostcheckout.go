// Package hostcheckout says which git checkout a folder is in, as a place a host or podman
// session may run. A checkout inside an LXC container's tree (its root filesystem seen from the
// host under root's LXC path) is the container's, not the host's: a session started there would
// run an agent on the container's files from the host side, as the host user. ccy-teams and
// ccy-sessions ask here, never git alone, wherever they would treat a folder as a checkout.
package hostcheckout

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
)

// LxcPather reads where root's LXC keeps its containers (lxcfreeze.Lister is the real one).
type LxcPather interface {
	LxcPath(ctx context.Context) (string, error)
}

// Finder answers Find. The LXC path is asked once, and only when a folder is in a checkout.
type Finder struct {
	Git *gitx.Git
	// LookPath finds lxc-config: with none installed this host has no LXC containers.
	LookPath func(string) (string, error)
	Lxc      LxcPather

	asked    bool
	lxcPaths []string // as configured, and resolved when that differs; none without LXC
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
// an LXC path that cannot be read while LXC is installed, is an error: a checkout is never taken
// for the host's when that could not be told.
func (f *Finder) Find(ctx context.Context, dir string) (Checkout, error) {
	inTree, err := f.Git.ProjectHere(ctx, dir)
	if err != nil || !inTree {
		return Checkout{}, err
	}
	top, err := f.Git.TopLevel(ctx, dir)
	if err != nil {
		return Checkout{}, err
	}
	if err := f.askLxc(ctx); err != nil {
		return Checkout{}, fmt.Errorf("whether %s is an LXC container's checkout could not be told: %w. "+
			"play-claude-yolo.yml installs the sudoers rule that lets it be read without a password; until then run sudo -v first", top, err)
	}
	return Checkout{Top: top, Container: f.containerOf(top)}, nil
}

func (f *Finder) askLxc(ctx context.Context) error {
	if f.asked {
		return nil
	}
	if !f.lxcInstalled() {
		f.asked = true // there is no container tree to be in
		return nil
	}
	path, err := f.Lxc.LxcPath(ctx)
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

// lxcInstalled reports whether this host has LXC: lxc-config is on PATH.
func (f *Finder) lxcInstalled() bool {
	_, err := f.LookPath("lxc-config")
	return err == nil
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
