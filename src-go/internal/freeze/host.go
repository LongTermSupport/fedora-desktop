package freeze

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ttyecho"
)

// The files a container runtime drops inside a container. Both are fields of Env so a test can
// point them elsewhere, but never reachable from the command line or the environment: nothing a
// caller can pass loosens the host guard.
const (
	ContainerEnvPath = "/run/.containerenv"
	DockerEnvPath    = "/.dockerenv"
	ttyPath          = "/dev/tty"
)

// Env is everything a run touches outside itself, so tests fill fakes and production fills the
// real thing.
type Env struct {
	Out, Err   io.Writer
	Getenv     func(string) string
	Stat       func(string) (os.FileInfo, error)
	LookPath   func(string) (string, error)
	OpenTTY    func() (io.ReadCloser, error) // the terminal to read numbered replies from
	IsTerminal func(any) bool
	Runner     execx.Runner
	// Echo holds the terminal's echo off for the run, so keys typed between two screens are not
	// echoed over the next (ttyecho); nil holds nothing. A tool's NewApp runs Runner under it.
	Echo             *ttyecho.Hold
	ContainerEnvPath string
	DockerEnvPath    string
}

// NewEnv fills the production seams.
func NewEnv(out, stderr io.Writer, runner execx.Runner) Env {
	return Env{
		Out: out, Err: stderr,
		Getenv: os.Getenv, Stat: os.Stat, LookPath: exec.LookPath,
		OpenTTY: openTTY, IsTerminal: term.FileIsTerminal, Runner: runner,
		ContainerEnvPath: ContainerEnvPath, DockerEnvPath: DockerEnvPath,
	}
}

// NewTerminalEnv is NewEnv for a tool run by a person: the real commands, run under a hold on
// the echo of the terminal the keys are read from (stdin), which the run starts and releases.
func NewTerminalEnv(out, stderr io.Writer) Env {
	echo := ttyecho.New(os.Stdin)
	env := NewEnv(out, stderr, echo.Runner(execx.OS{}))
	env.Echo = echo
	return env
}

// openTTY opens the controlling terminal, failing when there is none (a readable /dev/tty
// device node is not proof of one).
func openTTY() (io.ReadCloser, error) {
	f, err := os.Open(ttyPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", ttyPath, err)
	}
	return f, nil
}

// AssertOnHost refuses to run inside a container: the host's engine is not reachable from in
// there, so every container would look absent. The signals are a container marker file, a docker
// marker file, or a non-empty `container` variable.
func AssertOnHost(cfg Config, env Env) error {
	for _, path := range []string{env.ContainerEnvPath, env.DockerEnvPath} {
		found, err := isFile(env, path)
		if err != nil {
			return err
		}
		if found {
			return refuseContainer(cfg)
		}
	}
	if env.Getenv("container") != "" {
		return refuseContainer(cfg)
	}
	return nil
}

func refuseContainer(cfg Config) error {
	return Die("this is a container — run %s on the HOST.\n%s", cfg.Tool, cfg.HostOnlyNote)
}

// isFile is whether path is a regular file. Not being there is an answer; being unable to tell
// is not, and fails rather than guess that this is a host.
func isFile(env Env, path string) (bool, error) {
	info, err := env.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cannot tell whether %s exists, so cannot tell whether this is a container: %w", path, err)
	}
	return info.Mode().IsRegular(), nil
}
