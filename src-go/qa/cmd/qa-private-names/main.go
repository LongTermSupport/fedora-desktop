// Command qa-private-names is the guard against install-specific names (agent teams, seats,
// containers, hosts, client projects) reaching this public repository (Plan 00245 Phase 3). It is
// a QA tool, not deployed anywhere; scripts/qa-private-names.bash builds and runs it for the
// pre-commit and pre-push hooks and for the QA gate.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/privatenames"
)

// containerRoot is where a ccy container mounts the checkout (CLAUDE/ContainerRules.md).
const containerRoot = "/workspace"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(privatenames.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(privatenames.ExitCannotRun)
	}
	env := privatenames.Env{
		Stdout:       os.Stdout,
		Stderr:       os.Stderr,
		Stdin:        os.Stdin,
		Cwd:          cwd,
		Run:          proc.OS{},
		Container:    inContainer(cwd),
		Hostnames:    hostnames(),
		Context:      os.Getenv("PRIVATE_NAMES_CONTEXT") != "",
		ReadFile:     os.ReadFile,
		WriteFile:    writePrivate,
		ReplaceFile:  replacePrivate,
		CreateFile:   createPrivate,
		EvalSymlinks: filepath.EvalSymlinks,
	}
	return int(privatenames.Main(args, env))
}

// inContainer is whether the checkout is the container's /workspace, or a work tree under it.
func inContainer(cwd string) bool {
	return cwd == containerRoot || strings.HasPrefix(cwd, containerRoot+"/")
}

// hostnames are this machine's name (a container's own id) and the host's, when the launcher says it.
func hostnames() []string {
	var names []string
	if h, err := os.Hostname(); err == nil {
		names = append(names, h)
	}
	if h := os.Getenv("CCY_HOST_HOSTNAME"); h != "" {
		names = append(names, h)
	}
	return names
}

// writePrivate writes the list for its owner alone: the terms are the one thing in the checkout
// that must not be read by others.
func writePrivate(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// replacePrivate replaces a file whole: an owner-only sibling (CreateTemp's mode is 0600) renamed
// over it, so a reader sees the old list or the new one, never a part, and a looser mode the old
// file had goes with it.
func replacePrivate(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create a sibling of %s: %w", path, err)
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", tmp, err), f.Close(), os.Remove(tmp))
	}
	if err := f.Close(); err != nil {
		return errors.Join(fmt.Errorf("close %s: %w", tmp, err), os.Remove(tmp))
	}
	if err := os.Rename(tmp, path); err != nil {
		return errors.Join(fmt.Errorf("replace %s: %w", path, err), os.Remove(tmp))
	}
	return nil
}

// createPrivate writes a new file for its owner alone and refuses an existing path (fs.ErrExist),
// a link included: the denylist holds the terms and never replaces a file.
func createPrivate(path string, data []byte) error {
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", path, err), f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}
