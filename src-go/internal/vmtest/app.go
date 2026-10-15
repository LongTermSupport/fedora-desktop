// Package vmtest is the host CLI of the VM lifecycle acceptance lab (Plan 00110): it fetches and
// verifies published Fedora media, builds base images, boots throwaway guests from them, drives
// run.bash and the in-guest checks over SSH, and hands every decision (the manifest, freshness,
// the base record, the verdict, retention) to the Python helpers the lab play deploys, through
// one seam (Lab.helper). The specification is CLAUDE/Plan/00192-.../SPEC-vmtest.md; the
// expectation numbers (E1...) and departures from the bash (B1...) the comments cite are its.
package vmtest

import (
	"context"
	"crypto/rand"
	_ "embed" // usage.txt is embedded verbatim
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

//go:embed usage.txt
var usageText string

// DefaultGPGKeyDir is where distribution-gpg-keys installs the Fedora release keys.
const DefaultGPGKeyDir = "/usr/share/distribution-gpg-keys/fedora"

// Env is everything a run touches outside its own memory; every field is a seam a test fills.
type Env struct {
	Runner execx.Runner
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	// Home is the user's home directory (the default lab and the bridge's config live under it).
	Home string
	Now  func() time.Time
	// Sleep waits d or until ctx ends, whichever is first.
	Sleep func(ctx context.Context, d time.Duration) error
	// FreePort is a loopback TCP port nothing listens on at the moment of asking.
	FreePort func() (int, error)
	// Random is the source of the throwaway secrets a desktop base is built with.
	Random    io.Reader
	GPGKeyDir string
	// TempDir is where the probe's private temporary file goes; empty is the system default.
	TempDir string
}

// NewEnv is the production environment on the process's own streams.
func NewEnv() Env {
	return Env{
		Runner:    execx.OS{},
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Getenv:    os.Getenv,
		Home:      os.Getenv("HOME"),
		Now:       time.Now,
		Sleep:     sleepContext,
		FreePort:  freeLoopbackPort,
		Random:    rand.Reader,
		GPGKeyDir: DefaultGPGKeyDir,
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("sleep: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// freeLoopbackPort asks the kernel for a free port and releases it for virt-install to take.
// The race between the release and the take is the bash's too (SPEC section 4).
func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find a free port: %w", err)
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if cerr := l.Close(); cerr != nil {
		return 0, fmt.Errorf("release the free port: %w", cerr)
	}
	if !ok {
		return 0, errors.New("find a free port: not a TCP address")
	}
	return addr.Port, nil
}

// InterruptError is the cause a context is cancelled with when the process gets a signal (E4).
type InterruptError struct {
	Signal syscall.Signal
}

func (i InterruptError) Error() string { return "interrupted by " + i.Signal.String() }

// failure is a step that failed and says why: `ERROR: <msg>`, exit 1.
type failure struct{ msg string }

func (f *failure) Error() string { return f.msg }

func failf(format string, args ...any) error { return &failure{msg: fmt.Sprintf(format, args...)} }

// usageError is a command line that cannot be run: `ERROR: <msg>`, exit 64 (E2).
type usageError struct{ msg string }

func (u *usageError) Error() string { return u.msg }

func usagef(format string, args ...any) error { return &usageError{msg: fmt.Sprintf(format, args...)} }

// exitStatusError ends the command with its own status and no message of its own (member-ssh,
// freshness-status, a run whose verdict is not pass).
type exitStatusError struct{ code term.ExitCode }

func (e *exitStatusError) Error() string { return fmt.Sprintf("exit %d", e.code) }

// App is one vmtest invocation.
type App struct {
	Env Env
	// tweak, set by tests only, adjusts the run's state before it starts.
	tweak func(*cli)
}

// Run executes args (without the program name) and returns the exit status.
func (a *App) Run(ctx context.Context, args []string) term.ExitCode {
	c := newCLI(a.Env)
	if a.tweak != nil {
		a.tweak(c)
	}
	err := c.dispatch(ctx, args)
	return c.finish(ctx, err)
}

type command func(c *cli, ctx context.Context, args []string) error

var commands = map[string]command{
	"fetch":            (*cli).cmdFetch,
	"build-base":       (*cli).cmdBuildBase,
	"run":              (*cli).cmdRun,
	"verify":           (*cli).cmdVerify,
	"refresh-base":     (*cli).cmdRefreshBase,
	"freshness-status": (*cli).cmdFreshnessStatus,
	"sweep":            (*cli).cmdSweep,
	"nightly":          (*cli).cmdNightly,
	"ensure-base":      (*cli).cmdEnsureBase,
	"member-up":        (*cli).cmdMemberUp,
	"member-ssh":       (*cli).cmdMemberSSH,
	"member-down":      (*cli).cmdMemberDown,
}

func (c *cli) dispatch(ctx context.Context, args []string) error {
	name := "help"
	if len(args) > 0 {
		name = args[0]
		args = args[1:]
	}
	switch name {
	case "help", "-h", "--help":
		c.p.Print(c.env.Stdout, usageText)
		return nil
	}
	cmd, ok := commands[name]
	if !ok {
		c.p.Print(c.env.Stderr, usageText)
		return usagef("unknown command: %s", name)
	}
	return cmd(c, ctx, args)
}

// finish turns the command's error into its exit status and its one ERROR line.
func (c *cli) finish(ctx context.Context, err error) term.ExitCode {
	if in, ok := errors.AsType[InterruptError](context.Cause(ctx)); ok {
		c.errorLine(in.Error())
		return term.ExitCode(128 + int(in.Signal))
	}
	code := term.ExitOK
	if status, ok := errors.AsType[*exitStatusError](err); ok {
		code = status.code
	} else if usage, ok := errors.AsType[*usageError](err); ok {
		c.errorLine(usage.msg)
		code = term.ExitUsage
	} else if err != nil {
		c.errorLine(err.Error())
		code = term.ExitFailure
	}
	if code == term.ExitOK && c.p.Err() != nil {
		return term.ExitFailure
	}
	return code
}

// cli is one run's state shared by the commands: the streams and the lab's paths.
type cli struct {
	env   Env
	p     term.Printer
	paths paths
	git   *gitx.Git
	// build is build-base for one key on a loaded lab; ensure-base and refresh-base call it.
	build func(ctx context.Context, l *lab, key string) error
	// bound is the deadline put on a bounded remote command (timeout(1) in the bash).
	bound func(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc)
}

func newCLI(env Env) *cli {
	c := &cli{env: env, paths: newPaths(env), git: gitx.New(env.Runner), bound: context.WithTimeout}
	c.build = c.buildBase
	return c
}

// logf is a progress line on stderr (E3).
func (c *cli) logf(format string, args ...any) {
	c.p.Print(c.env.Stderr, "==> "+fmt.Sprintf(format, args...)+"\n")
}

// sayf is a marker line on stdout, the command's payload (E3).
func (c *cli) sayf(format string, args ...any) {
	c.p.Print(c.env.Stdout, fmt.Sprintf(format, args...)+"\n")
}

func (c *cli) errorLine(msg string) {
	c.p.Print(c.env.Stderr, "ERROR: "+msg+"\n")
}
