// Package sudo gets and uses root for the few commands the ccy tools need it for. Ready
// obtains sudo's credential once, with a reason; AsRoot and Run then use "sudo -n" only, so
// an expired credential fails loudly instead of prompting with no explanation.
package sudo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The two root-owned programs ccy-teams runs through sudo, by absolute path. The sudoers rule
// (read-only agent-bus commands) matches the exact argv of AgentBus only: InstallTool is reached
// through password sudo, so a run with no terminal and a lapsed credential fails with sudo's words.
const (
	AgentBus    = "/usr/local/bin/agent-bus"
	InstallTool = "/usr/local/sbin/agent-bus-install"
)

const cmd = "sudo"

var (
	// ErrNotReady is sudo not being given: no terminal to ask on, or the password was
	// refused or cancelled. The reason has already been printed.
	ErrNotReady = errors.New("sudo is not ready")
	// ErrNeedsPassword is "sudo -n" wanting a password first; nothing was printed.
	ErrNeedsPassword = errors.New("sudo wants a password first")
)

// Sudo runs commands as root. Stdin is where a password would be typed; Term.Err is where
// it is asked for. Stdout is where Run streams a root command's own output: that output is
// the command's result for the caller, so it is not mixed into the terminal lines.
type Sudo struct {
	Runner execx.Runner
	Term   *term.Term
	Stdin  io.Reader
	Stdout io.Writer
	// OpenTTY opens the controlling terminal. sudo asks for its password there whatever
	// stdin and stderr are, so whether it opens is whether asking is possible. Nil means
	// no terminal to ask on.
	OpenTTY func() (io.ReadCloser, error)
}

// New is a Sudo on the process's stdin and stdout, asking on /dev/tty.
func New(r execx.Runner, t *term.Term) *Sudo {
	return &Sudo{Runner: r, Term: t, Stdin: os.Stdin, Stdout: os.Stdout, OpenTTY: openDevTTY}
}

func openDevTTY() (io.ReadCloser, error) {
	f, err := os.Open("/dev/tty")
	if err != nil {
		return nil, fmt.Errorf("open /dev/tty: %w", err)
	}
	return f, nil
}

// Ready makes sudo's credential available for the root commands that follow. Passwordless
// or cached sudo asks nothing; otherwise why is said, then sudo asks for the login
// password. Failure returns ErrNotReady after printing the reason, and the caller must not
// carry on as if the root command had run.
func (s *Sudo) Ready(ctx context.Context, why string) error {
	probe, err := s.Runner.Run(ctx, execx.Cmd{Name: cmd, Args: []string{"-n", "-v"}})
	if err == nil {
		return nil
	}
	// "-v" wants authentication unless every sudoers entry is NOPASSWD, so on a host with some
	// NOPASSWD entries it fails while root commands run without a password.
	if _, rootErr := s.Runner.Run(ctx, execx.Cmd{Name: cmd, Args: []string{"-n", "true"}}); rootErr == nil {
		return nil
	}
	said := strings.TrimRight(string(probe.Stdout)+string(probe.Stderr), "\n")
	if noTTY := s.noTerminal(); noTTY != nil {
		s.Term.PrintError(why + " That needs sudo, and with no terminal there is nowhere to ask for your password (sudo said: " + said + "). Run sudo -v first.")
		return fmt.Errorf("%w: no terminal: %w", ErrNotReady, noTTY)
	}
	s.Term.Printf("\n")
	s.Term.Say(why)
	s.Term.Printf("sudo asks for your login password.\n")
	_, err = s.Runner.Run(ctx, execx.Cmd{Name: cmd, Args: []string{"-v"}, Stdin: s.Stdin, Stdout: s.Term.Err, Stderr: s.Term.Err})
	if err != nil {
		s.Term.PrintError("sudo was not given your password (three wrong tries, or cancelled).")
		return fmt.Errorf("%w: %w", ErrNotReady, err)
	}
	return nil
}

// Capture runs a root command through "sudo -n" and hands back both streams unprinted, for a
// caller that must keep the command's stderr (it is the text of a failure, not noise). On a
// non-zero exit the Result still carries the streams and the error is the *execx.ExitError;
// sudo wanting a password is additionally ErrNeedsPassword.
func (s *Sudo) Capture(ctx context.Context, argv ...string) (execx.Result, error) {
	res, err := s.Runner.Run(ctx, execx.Cmd{Name: cmd, Args: append([]string{"-n"}, argv...)})
	if err != nil && strings.HasPrefix(string(res.Stderr), "sudo: a password is required") {
		return res, fmt.Errorf("%w: %w", ErrNeedsPassword, err)
	}
	return res, err
}

// AsRoot runs a root command through "sudo -n", never prompting, and returns its stdout.
// ErrNeedsPassword means sudo wanted a password first (exit 1 with sudo's own words on
// stderr), and nothing is printed. Any other failure copies the command's stderr to the
// terminal and returns the error.
func (s *Sudo) AsRoot(ctx context.Context, argv ...string) (string, error) {
	res, err := s.Runner.Run(ctx, execx.Cmd{Name: cmd, Args: append([]string{"-n"}, argv...)})
	if err == nil {
		return string(res.Stdout), nil
	}
	var exitErr *execx.ExitError
	if !errors.As(err, &exitErr) {
		return "", err
	}
	if exitErr.Code == 1 && asksForPassword(string(res.Stderr)) {
		return "", ErrNeedsPassword
	}
	if _, werr := s.Term.Err.Write(res.Stderr); werr != nil {
		return "", errors.Join(err, werr)
	}
	return "", err
}

// passwordRequired is the line sudo -n prints when it would have had to prompt. Other sudo
// failures (unknown command, unresolvable host, PAM, not permitted) also exit 1 behind a
// "sudo: " prefix and must not be reported as a missing credential.
const passwordRequired = "sudo: a password is required"

// asksForPassword reports whether stderr carries sudo's own refusal line. Only a whole line
// counts, so a root command that prints similar words behind its own prefix is not mistaken.
func asksForPassword(stderr string) bool {
	return slices.Contains(strings.Split(stderr, "\n"), passwordRequired)
}

// Run runs a root command through "sudo -n", streaming its stdout to Stdout and its stderr
// to the terminal as it happens, for use after Ready.
func (s *Sudo) Run(ctx context.Context, argv ...string) error {
	_, err := s.Runner.Run(ctx, execx.Cmd{
		Name: cmd, Args: append([]string{"-n"}, argv...),
		Stdin: s.Stdin, Stdout: s.Stdout, Stderr: s.Term.Err,
	})
	return err
}

// noTerminal is why sudo cannot ask for a password here, or nil when it can: sudo asks on the
// controlling terminal, so whether /dev/tty opens is the test, whatever stdin and stderr are.
func (s *Sudo) noTerminal() error {
	if s.OpenTTY == nil {
		return errors.New("no terminal seam")
	}
	tty, err := s.OpenTTY()
	if err != nil {
		return err
	}
	return tty.Close()
}
