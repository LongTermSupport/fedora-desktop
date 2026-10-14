package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
)

// NotInstalledError is a program that could not be started because it is not on PATH. It
// is a different failure from a program that ran and refused (ExitError), so a caller can
// word "install it" apart from "it said no". It matches exec.ErrNotFound under errors.Is.
type NotInstalledError struct {
	Name string
}

func (e *NotInstalledError) Error() string { return e.Name + " is not installed" }

// Unwrap lets errors.Is(err, exec.ErrNotFound) match.
func (e *NotInstalledError) Unwrap() error { return exec.ErrNotFound }

// IsNotInstalled reports whether err is a program missing from PATH.
func IsNotInstalled(err error) bool {
	target, ok := errors.AsType[*NotInstalledError](err)
	return ok && target != nil
}

// Combined runs c with stdout and stderr merged into one stream, as a shell's 2>&1 does, and
// returns that text. A command that exits non-zero returns the text together with an
// *ExitError whose Stderr is the merged text, so a caller matching on tmux's or an engine's
// wording finds it wherever the program wrote it.
func Combined(ctx context.Context, r Runner, c Cmd) (string, error) {
	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf
	_, err := r.Run(ctx, c)
	out := buf.String()
	if exitErr, ok := errors.AsType[*ExitError](err); ok {
		exitErr.Stderr = out
	}
	return out, err
}

// ChildEnv is the environment of a child: base with every variable named in unset removed,
// then the KEY=VALUE entries of add appended, so an added entry wins over an inherited one.
func ChildEnv(base, add, unset []string) []string {
	env := make([]string, 0, len(base)+len(add))
	for _, kv := range base {
		if !slices.Contains(unset, strings.SplitN(kv, "=", 2)[0]) {
			env = append(env, kv)
		}
	}
	return append(env, add...)
}

// Execer replaces the running process with a program. A successful Exec does not return; it
// returns only the reason it could not.
type Execer interface {
	Exec(c Cmd) error
}

// syscallExec is the system call behind OS.Exec, a variable so a test can observe the call
// without losing the test process.
var syscallExec = syscall.Exec

// Exec implements Execer: it looks the program up on PATH (a name with a slash is used as
// given) and replaces this process with it, in the environment c describes.
func (OS) Exec(c Cmd) error {
	path := c.Name
	if !strings.Contains(path, "/") {
		found, err := exec.LookPath(c.Name)
		if err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				return &NotInstalledError{Name: c.Name}
			}
			return fmt.Errorf("find %s: %w", c.Name, err)
		}
		path = found
	}
	if c.Dir != "" {
		// The program is already resolved, so a relative Name was looked up from the old cwd.
		if err := os.Chdir(c.Dir); err != nil {
			return fmt.Errorf("enter %s: %w", c.Dir, err)
		}
	}
	if err := syscallExec(path, c.argv(), ChildEnv(os.Environ(), c.Env, c.Unset)); err != nil {
		return fmt.Errorf("exec %s: %w", path, err)
	}
	return nil
}

// Exec implements Execer for tests: it records the command and returns the error set by
// ExecError, nil by default.
func (f *Fake) Exec(c Cmd) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs = append(f.execs, c)
	return f.execErr
}

// ExecError sets what every later Exec returns.
func (f *Fake) ExecError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execErr = err
}

// Execs is every command handed to Exec so far, in order.
func (f *Fake) Execs() []Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Cmd(nil), f.execs...)
}

// OnError makes every command whose argv starts with prefix fail with err, as a program
// that cannot start does, instead of producing a Result.
func (f *Fake) OnError(prefix []string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rules = append(f.rules, rule{prefix: prefix, err: err})
}
