// Package sshkey is the ssh-with-key family: ssh-with-key, scp-with-key and
// ssh-copy-id-with-key, which log in to a host offering one private key and nothing else (no
// agent key, no other key in ~/.ssh or in ssh's configuration, no password), and for ssh and
// scp can forward a fresh agent made for the one run. The specification is
// CLAUDE/Plan/00192-.../SPEC-ssh-with-key.md; the expectation numbers (E1...) and departures
// from the bash (B1...) the comments cite are its.
package sshkey

import (
	"context"
	_ "embed" // the usage texts are embedded verbatim
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

var (
	//go:embed usage-ssh.txt
	sshUsage string
	//go:embed usage-scp.txt
	scpUsage string
	//go:embed usage-copy-id.txt
	copyIDUsage string
)

// kind is which program a tool wraps, for the parts of the grammar that differ.
type kind int

const (
	kindSSH kind = iota
	kindSCP
	kindCopyID
)

// Tool is one of the three commands.
type Tool struct {
	// Name is the command's name, the prefix of its messages.
	Name string
	// Program is what it runs.
	Program string
	// KeyFlag gives the login key; it is recognised only as the first argument (E3).
	KeyFlag string
	kind    kind
	usage   string
}

// The programs the tools wrap.
const (
	progSSH    = "ssh"
	progSCP    = "scp"
	progCopyID = "ssh-copy-id"
)

// The three commands (D1).
var (
	SSH    = Tool{Name: "ssh-with-key", Program: progSSH, KeyFlag: "-i", kind: kindSSH, usage: sshUsage}
	SCP    = Tool{Name: "scp-with-key", Program: progSCP, KeyFlag: "-i", kind: kindSCP, usage: scpUsage}
	CopyID = Tool{Name: "ssh-copy-id-with-key", Program: progCopyID, KeyFlag: "--auth-key", kind: kindCopyID, usage: copyIDUsage}
)

// forwards reports whether the tool can forward an agent (ssh-copy-id cannot, E13).
func (t Tool) forwards() bool { return t.kind != kindCopyID }

// Env is everything a run touches outside its own memory and the home directory.
type Env struct {
	Runner       execx.Runner
	Execer       execx.Execer
	Foregrounder execx.Foregrounder
	Stdout       io.Writer
	Stderr       io.Writer
	Getenv       func(string) string
	Getpid       func() int
	// Self is this program's path, which the forwarding agent runs again (E14).
	Self func() (string, error)
	// OpenTTY opens the controlling terminal the menus read from.
	OpenTTY func() (io.ReadCloser, error)
	// ParentOf is a process's parent's pid, as /proc says.
	ParentOf   func(pid int) (int, error)
	IsTerminal func(any) bool
	// Columns is the terminal's width for the headings, 0 when unknown.
	Columns func() int
}

// NewEnv is the production environment.
func NewEnv() Env {
	return Env{
		Runner: execx.OS{}, Execer: execx.OS{}, Foregrounder: execx.OS{},
		Stdout: os.Stdout, Stderr: os.Stderr,
		Getenv: os.Getenv, Getpid: os.Getpid, Self: os.Executable,
		OpenTTY: func() (io.ReadCloser, error) {
			return os.Open("/dev/tty")
		},
		ParentOf:   procParent,
		IsTerminal: term.FileIsTerminal,
		Columns:    func() int { return term.FileColumns(os.Stderr) },
	}
}

// App is one run of a tool.
type App struct {
	Tool Tool
	Env  Env
}

// run is the state of one invocation.
type run struct {
	tool Tool
	env  Env
	home string
	out  term.Printer
}

// Run is the whole command. A run that said nothing it could not write keeps its code; one
// whose messages could not be written does not report success.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	r := &run{tool: a.Tool, env: a.Env, home: a.Env.Getenv("HOME")}
	code := r.main(ctx, args)
	if r.out.Err() != nil && code == term.ExitOK {
		return term.ExitFailure
	}
	return code
}

func (r *run) main(ctx context.Context, args []string) term.ExitCode {
	if len(args) == 0 {
		return r.usageError(errors.New("no arguments"))
	}
	if args[0] == "-h" || args[0] == "--help" {
		r.out.Print(r.env.Stdout, r.tool.usage)
		return term.ExitOK
	}
	inv, err := r.tool.parse(args)
	if err != nil {
		return r.usageError(err)
	}
	if inv.forward && r.env.Getenv(markerVar) != "" {
		return r.inAgent(ctx, inv)
	}
	key, code := r.loginKey(ctx, inv.key)
	if code != term.ExitOK {
		return code
	}
	inv.key = key
	if !r.probe(ctx, inv) {
		return term.ExitFailure
	}
	if inv.forward {
		return r.startAgent(inv)
	}
	return r.replace(r.tool.command(append(r.ownOptions(key, true), inv.passed...)))
}

// command is the tool's program run with args, each program named by a constant so its owner
// can be judged (project_seam_commands).
func (t Tool) command(args []string) execx.Cmd {
	switch t.kind {
	case kindSCP:
		return execx.Cmd{Name: progSCP, Args: args}
	case kindCopyID:
		return execx.Cmd{Name: progCopyID, Args: args}
	case kindSSH:
	}
	return execx.Cmd{Name: progSSH, Args: args}
}

// replace hands the process to c (D2); it returns only when that could not be done.
func (r *run) replace(c execx.Cmd) term.ExitCode {
	if err := r.env.Execer.Exec(c); err != nil {
		r.startFailed(c.Name, err)
		return term.ExitFailure
	}
	return term.ExitOK
}

// ownOptions are the tool's options, which come before the passed ones so ssh's first value
// wins (E10). noAgent adds IdentityAgent=none, left out when an agent is being forwarded (E18).
func (r *run) ownOptions(key string, noAgent bool) []string {
	if r.tool.kind == kindCopyID {
		return []string{
			"-o", "IdentityFile=" + key, "-o", "IdentitiesOnly=yes",
			"-o", "IdentityAgent=none", "-o", "PreferredAuthentications=publickey",
		}
	}
	opts := []string{"-i", key, "-o", "IdentitiesOnly=yes", "-o", "PreferredAuthentications=publickey"}
	if noAgent {
		opts = append(opts, "-o", "IdentityAgent=none")
	}
	return opts
}

// usageError is E2-E4 and E13's refusals: said with the pointer to --help, exit 64.
func (r *run) usageError(err error) term.ExitCode {
	r.failf("%v — see '%s --help'.", err, r.tool.Name)
	return term.ExitUsage
}

// startFailed says why a program could not be started (E12).
func (r *run) startFailed(program string, err error) {
	if execx.IsNotInstalled(err) {
		r.failf("%s is not installed.", program)
		return
	}
	r.failf("%v", err)
}

// cancelled ends a run the person left (E7, E16): nothing was run.
func (r *run) cancelled(ctx context.Context) term.ExitCode {
	r.say("Cancelled; nothing was run.")
	if se, ok := errors.AsType[*execx.SignalError](context.Cause(ctx)); ok {
		return term.ExitCode(se.ExitCode())
	}
	return term.ExitCancelled
}

func (r *run) sshDir() string { return filepath.Join(r.home, ".ssh") }

// show is a path as the person reads it, the home directory abbreviated.
func (r *run) show(path string) string { return term.AbbrevHome(path, r.home) }

func (r *run) say(text string) { r.out.Print(r.env.Stderr, text+"\n") }

func (r *run) failf(format string, args ...any) {
	r.say(r.tool.Name + ": " + fmt.Sprintf(format, args...))
}
