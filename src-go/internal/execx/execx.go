// Package execx is the one seam through which every tool runs another program. Production
// code takes a Runner; tests pass a Fake, which records each call and answers it from a
// script, so no flow needs the real gum, fzf, sudo or systemd to be tested.
package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Cmd is one program run: argv, extra environment, stdin and working directory. Stdout and
// Stderr nil means captured into the Result; set, they stream there (a terminal UI, a
// play's progress) and the Result holds nothing for them.
type Cmd struct {
	Name   string
	Args   []string
	Env    []string // KEY=VALUE added to the parent's environment
	Unset  []string // variable names removed from the child's environment
	Dir    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Interrupt is the signal a cancelled context sends the child; SIGTERM when zero. A
	// recorder that flushes its buffers on SIGINT (pw-record) is stopped with SIGINT.
	Interrupt syscall.Signal
	// ExtraFiles are open files the child inherits from descriptor 3 on (a held lock
	// delegated to a child).
	ExtraFiles []*os.File
	// NewSession runs the child in a session of its own, for a command whose children must
	// not outlive it (the status line's inner): see runSession. Interrupt is not used.
	NewSession bool
	// Credential, when set, runs the child as its uid, gid and exact supplementary group list
	// (an empty list clears them); NoSetGroups is refused, as is a credential given to Exec.
	Credential *syscall.Credential
	// CleanEnv makes Env the child's whole environment: nothing of the parent's is inherited,
	// an empty Env is an empty environment, and Unset beside it is refused.
	CleanEnv bool
}

// Line is the argv as one space-joined string, for logs and test matching.
func (c Cmd) Line() string {
	return strings.Join(c.argv(), " ")
}

func (c Cmd) argv() []string {
	return append([]string{c.Name}, c.Args...)
}

// ErrBadCmd is a Cmd whose fields contradict each other; no runner starts it.
var ErrBadCmd = errors.New("execx: contradictory command")

// check refuses a Cmd no runner can honour as written.
func (c Cmd) check() error {
	if c.CleanEnv && len(c.Unset) > 0 {
		return fmt.Errorf("%w: %s has CleanEnv and Unset", ErrBadCmd, c.Name)
	}
	if c.Credential != nil && c.Credential.NoSetGroups {
		return fmt.Errorf("%w: %s has a credential that keeps the parent's groups", ErrBadCmd, c.Name)
	}
	return nil
}

// environ is the child's environment as exec.Cmd takes it: nil inherits the parent's.
func (c Cmd) environ() []string {
	switch {
	case c.CleanEnv:
		return append([]string{}, c.Env...)
	case len(c.Env) > 0 || len(c.Unset) > 0:
		return ChildEnv(os.Environ(), c.Env, c.Unset)
	}
	return nil
}

// sysProcAttr is the child's start attributes: a session of its own, the credential, or nil.
func (c Cmd) sysProcAttr(setsid bool) *syscall.SysProcAttr {
	if !setsid && c.Credential == nil {
		return nil
	}
	return &syscall.SysProcAttr{Setsid: setsid, Credential: c.Credential}
}

// Result is what a finished command produced.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// ExitError is a command that ran and exited non-zero, or was ended by a signal (Signal set,
// Code -1). A command that could not start at all is a different error, so a caller can tell
// "refused" from "not installed".
type ExitError struct {
	Name   string
	Code   int
	Signal syscall.Signal
	Stderr string
}

func (e *ExitError) Error() string {
	msg := fmt.Sprintf("%s exited %d", e.Name, e.Code)
	if e.Signal != 0 {
		msg = fmt.Sprintf("%s killed by %s", e.Name, unix.SignalName(e.Signal))
	}
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += ": " + s
	}
	return msg
}

// Runner runs a command to completion.
type Runner interface {
	Run(ctx context.Context, c Cmd) (Result, error)
}

// DefaultGrace is how long a cancelled child gets to exit after SIGTERM before it is killed.
const DefaultGrace = 3 * time.Second

// OS runs commands for real. Grace overrides DefaultGrace when set.
type OS struct {
	Grace time.Duration
}

// Run implements Runner. Cancelling ctx sends the child SIGTERM (or c.Interrupt), not SIGKILL: fzf and gum
// restore the terminal (raw mode, alternate screen) from their TERM handler, and a kill that
// lands first leaves the tty wedged. SIGTERM rather than SIGINT because the terminal driver
// has already delivered SIGINT to the foreground group on Ctrl-C, and a second SIGINT would
// read to some programs as "abort now". WaitDelay bounds the wait: a child that ignores TERM,
// or leaves a grandchild holding its output pipe, is killed and its pipes closed after Grace.
// Grace also bounds the drain of the pipes after a normal exit, so it must exceed the time
// the copy takes on a loaded host; a Grace of milliseconds can cut a finished command short.
func (o OS) Run(ctx context.Context, c Cmd) (Result, error) {
	if err := c.check(); err != nil {
		return Result{}, err
	}
	if c.NewSession {
		return o.runSession(ctx, c)
	}
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.SysProcAttr = c.sysProcAttr(false)
	interrupt := syscall.SIGTERM
	if c.Interrupt != 0 {
		interrupt = c.Interrupt
	}
	cmd.Cancel = func() error { return cmd.Process.Signal(interrupt) }
	cmd.WaitDelay = DefaultGrace
	if o.Grace > 0 {
		cmd.WaitDelay = o.Grace
	}
	cmd.Dir = c.Dir
	cmd.Env = c.environ()
	cmd.Stdin = c.Stdin
	cmd.ExtraFiles = c.ExtraFiles
	var out, errBuf bytes.Buffer
	cmd.Stdout = pick(c.Stdout, &out)
	cmd.Stderr = pick(c.Stderr, &errBuf)
	err := cmd.Run()
	res := Result{Stdout: out.Bytes(), Stderr: errBuf.Bytes()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return res, nil
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
		e := &ExitError{Name: c.Name, Code: res.ExitCode, Stderr: errBuf.String()}
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			e.Signal = status.Signal()
		}
		return res, e
	case errors.Is(err, exec.ErrWaitDelay):
		// The command ended but a child still held its pipe: what it wrote is complete, so
		// the Result is returned with the error.
		return res, fmt.Errorf("run %s: a process it started still held its output: %w", c.Name, err)
	case errors.Is(err, exec.ErrNotFound):
		return res, &NotInstalledError{Name: c.Name}
	default:
		return res, fmt.Errorf("run %s: %w", c.Name, err)
	}
}

func pick(given io.Writer, capture *bytes.Buffer) io.Writer {
	if given != nil {
		return given
	}
	return capture
}

// ErrUnexpected is a Fake asked to run a command its script does not cover: a test that
// did not expect the call fails loudly instead of being answered with a default.
var ErrUnexpected = errors.New("execx.Fake: unscripted command")

type rule struct {
	prefix []string
	result Result
	err    error // when set, the command fails to start with this error
	// fn, when set, answers each matching command itself, so a script can change its answer
	// from one call to the next.
	fn func(Cmd) (Result, error)
}

// Fake is a scripted, recording Runner for tests. Safe for concurrent use.
type Fake struct {
	mu      sync.Mutex
	rules   []rule
	calls   []Cmd
	execs   []Cmd
	execErr error
}

// On answers every command whose argv starts with prefix. Later rules win over earlier
// ones, so a test can override a default it set up in a helper.
func (f *Fake) On(prefix []string, r Result) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rules = append(f.rules, rule{prefix: prefix, result: r})
}

// OnFunc answers every command whose argv starts with prefix by calling fn, which sees the
// command and may answer differently each time. An error from fn is the command failing to
// start; a Result with a non-zero ExitCode is a command that ran and refused. fn runs with the
// Fake locked, so it must not call back into it.
func (f *Fake) OnFunc(prefix []string, fn func(Cmd) (Result, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rules = append(f.rules, rule{prefix: prefix, fn: fn})
}

// Run implements Runner.
func (f *Fake) Run(_ context.Context, c Cmd) (Result, error) {
	if err := c.check(); err != nil {
		return Result{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	argv := c.argv()
	for _, r := range slices.Backward(f.rules) {
		if hasPrefix(argv, r.prefix) {
			return r.answer(c)
		}
	}
	return Result{}, fmt.Errorf("%w: %s", ErrUnexpected, c.Line())
}

// answer is the rule's reply to a command it matched: the output streamed to the command's own
// writers, and a non-zero exit as an ExitError.
func (r rule) answer(c Cmd) (Result, error) {
	if r.err != nil {
		return Result{}, r.err
	}
	res := r.result
	if r.fn != nil {
		var err error
		if res, err = r.fn(c); err != nil {
			return Result{}, err
		}
	}
	if c.Stdout != nil {
		if _, err := c.Stdout.Write(res.Stdout); err != nil {
			return res, fmt.Errorf("fake stdout: %w", err)
		}
	}
	if c.Stderr != nil {
		if _, err := c.Stderr.Write(res.Stderr); err != nil {
			return res, fmt.Errorf("fake stderr: %w", err)
		}
	}
	if res.ExitCode != 0 {
		return res, &ExitError{Name: c.Name, Code: res.ExitCode, Stderr: string(res.Stderr)}
	}
	return res, nil
}

// Calls is every command run so far, in order.
func (f *Fake) Calls() []Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Cmd(nil), f.calls...)
}

// Called reports whether any recorded command's Line starts with prefix.
func (f *Fake) Called(prefix string) bool {
	for _, c := range f.Calls() {
		if strings.HasPrefix(c.Line(), prefix) {
			return true
		}
	}
	return false
}

func hasPrefix(argv, prefix []string) bool {
	if len(prefix) > len(argv) {
		return false
	}
	for i, p := range prefix {
		if argv[i] != p {
			return false
		}
	}
	return true
}
