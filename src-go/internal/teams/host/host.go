// Package host is the machine ccy-teams runs on, as the flows see it: the streams, the
// environment and one adapter for each tool, all built over a single execx.Runner so a test
// swaps the whole machine by giving it a Fake. The flows (the table, the picker, the wizard,
// apply and remove) each take the few adapters they use from here; none of them knows of the
// others.
package host

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccyenv"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/hostcheckout"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/config"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// ErrCheckoutGone is NeedCheckout's refusal: it has already said why.
var ErrCheckoutGone = errors.New("the fedora-desktop checkout has gone")

// Host is one run of ccy-teams on a machine. Every external effect goes through Runner,
// Stdout and Stderr, and each tool is reached through an adapter built over them. A test sets
// Runner (an execx.Fake) and the streams, may replace any adapter, and Wire fills in the rest.
type Host struct {
	config.Config

	Runner execx.Runner
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Payload is where a command prints the one value its caller captures. It is Stdout unless the
	// host was diverted (NewDiverted), when every other flow's output goes to Stderr instead.
	Payload io.Writer
	Getenv  func(string) string
	// IsTerminal says whether a stream is a terminal: where colour is used and questions asked.
	IsTerminal func(any) bool
	// Columns is how many columns wide a terminal stream is, 0 when it is not one or does not say.
	Columns func(any) int
	// Out keeps the first output that could not be written, so the run does not end in success.
	Out *term.Printer

	Term    *term.Term
	Sudo    *sudo.Sudo
	Systemd *systemd.Systemd
	Tmux    *tmuxccy.Tmux
	Git     *gitx.Git
	// Checkouts says which checkout a folder is in, and whether a host or podman session may run
	// there (not in an LXC container's files): ask it, never Git alone, before offering one.
	Checkouts *hostcheckout.Finder
	Declare   *declare.Client
	Bus       *agentbus.Client
	// Registry finds the session registry, which depends on the environment.
	Registry func() (*registry.Store, error)

	// Gum asks the questions, Picker is the fzf list every choice is made in.
	Gum    *ui.Gum
	Picker *ui.Picker

	// LookPath finds a tool on PATH. OpenTTY opens the terminal, which a picker reads after
	// fzf has had the screen. Exec replaces this process with the launcher, in dir.
	LookPath func(string) (string, error)
	OpenTTY  func() (io.ReadCloser, error)
	Exec     func(path string, argv []string, dir string) error
	// InContainer says whether this is a CCY container, where ccy-teams has no business.
	InContainer func() bool
}

// New is the production Host: the real OS and the process's own streams.
func New(cfg config.Config) *Host {
	h := &Host{Config: cfg, Runner: execx.OS{}, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
	h.Wire()
	return h
}

// NewDiverted is a Host for a command whose stdout is a value its caller captures: the process's
// stdout is kept for Payload, and everything the flows and the tools they run print on stdout
// (an Ansible play's output among it) goes to stderr.
func NewDiverted(cfg config.Config) *Host {
	h := &Host{Config: cfg, Runner: execx.OS{}, Stdin: os.Stdin, Stdout: os.Stderr, Stderr: os.Stderr, Payload: os.Stdout}
	h.Wire()
	return h
}

// Wire builds every adapter that is not set yet over Runner and the streams. Tools are found
// by bare name on PATH; only the two sudo targets are absolute.
func (h *Host) Wire() {
	if h.Runner == nil {
		h.Runner = execx.OS{}
	}
	if h.Stdin == nil {
		h.Stdin = os.Stdin
	}
	if h.Stdout == nil {
		h.Stdout = os.Stdout
	}
	if h.Stderr == nil {
		h.Stderr = os.Stderr
	}
	if h.Payload == nil {
		h.Payload = h.Stdout
	}
	if h.Getenv == nil {
		h.Getenv = os.Getenv
	}
	if h.IsTerminal == nil {
		h.IsTerminal = term.FileIsTerminal
	}
	if h.Columns == nil {
		h.Columns = term.FileColumns
	}
	if h.Out == nil {
		h.Out = &term.Printer{}
	}
	if h.Term == nil {
		h.Term = h.FreshTerm()
	}
	h.wireTools()
	h.wireAsking()
}

// FreshTerm is a Term over the host's streams with no steps counted yet: a flow that numbers
// its steps has one of its own.
func (h *Host) FreshTerm() *term.Term {
	return &term.Term{Err: h.Stderr, IsTerminal: h.IsTerminal, Getenv: h.Getenv}
}

// Print writes to a stream. The first failed write is kept in Out and the run turns it into
// a failure exit, so output nobody could read is never reported as success.
func (h *Host) Print(w io.Writer, s string) { h.Out.Print(w, s) }

// ReportErr prints why something failed, as the tool that failed said it: a "declare: ..."
// line stays that line, so the "(the reason is above)" of the message after it is true.
func (h *Host) ReportErr(err error) {
	h.Print(h.Stderr, err.Error()+"\n")
}

// Here is the folder the command was run in.
func (h *Host) Here() string {
	if pwd := h.Getenv("PWD"); pwd != "" {
		return pwd
	}
	wd, err := os.Getwd()
	if err != nil {
		return "the current folder"
	}
	return wd
}

// CcyInstalled refuses to go on without the CCY lib, saying why: inside a container ccy-teams
// is simply in the wrong place.
func (h *Host) CcyInstalled() bool {
	msg := ccyenv.Missing("ccy-teams", h.CcyLib, h.InContainer)
	if msg != "" {
		h.Print(h.Stderr, msg)
	}
	return msg == ""
}

// NeedCheckout refuses when the checkout ccy-teams was built from has moved or gone.
func (h *Host) NeedCheckout() error {
	if _, err := os.Stat(filepath.Join(h.RootDir, "helpers", "agent_bus", "declare.py")); err == nil {
		return nil
	}
	h.Print(h.Stderr, "ERROR: the fedora-desktop checkout ccy-teams was deployed from ("+h.RootDir+
		") has gone; re-run playbooks/imports/play-claude-yolo.yml from where it is now.\n")
	return ErrCheckoutGone
}

// wireTools builds the adapter of each tool.
func (h *Host) wireTools() {
	if h.Sudo == nil {
		h.Sudo = &sudo.Sudo{
			Runner: h.Runner, Term: h.Term, Stdin: h.Stdin, Stdout: h.Stdout,
			OpenTTY: func() (io.ReadCloser, error) { return h.OpenTTY() },
		}
	}
	if h.Systemd == nil {
		h.Systemd = &systemd.Systemd{Runner: h.Runner}
	}
	if h.Tmux == nil {
		h.Tmux = &tmuxccy.Tmux{Runner: h.Runner, Term: h.Term, Stdin: h.Stdin, Stdout: h.Stdout, Stderr: h.Stderr}
	}
	if h.Git == nil {
		h.Git = gitx.New(h.Runner)
	}
	if h.Checkouts == nil {
		h.Checkouts = &hostcheckout.Finder{Git: h.Git, ReadFile: lxcConfReader(h.Getenv)}
	}
	if h.Declare == nil {
		h.Declare = &declare.Client{Runner: h.Runner, Root: h.RootDir, File: h.HostVars, Err: h.Stderr}
	}
	if h.Bus == nil {
		h.Bus = &agentbus.Client{Runner: h.Runner, Root: h.Sudo, Err: h.Stderr}
	}
	if h.Registry == nil {
		h.Registry = func() (*registry.Store, error) { return registry.Locate(h.Getenv) }
	}
}

// wireAsking builds what the questions and the picker are asked through, and the seams to the
// terminal and the launcher.
func (h *Host) wireAsking() {
	if h.Gum == nil {
		h.Gum = &ui.Gum{Runner: h.Runner, Stdin: h.Stdin, Stderr: h.Stderr, Plain: !h.Term.ColourOn(h.Stderr)}
	}
	if h.Picker == nil {
		h.Picker = &ui.Picker{Runner: h.Runner, Stderr: h.Stderr, Plain: !h.Term.ColourOn(h.Stderr)}
	}
	if h.LookPath == nil {
		h.LookPath = exec.LookPath
	}
	if h.OpenTTY == nil {
		h.OpenTTY = openTerminal
	}
	if h.Exec == nil {
		h.Exec = execLauncher
	}
	if h.InContainer == nil {
		h.InContainer = ccyenv.InContainer
	}
}

// openTerminal is the controlling terminal, which is read for a keypress after fzf has drawn
// over the screen; stdin may be a pipe.
func openTerminal() (io.ReadCloser, error) {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return nil, fmt.Errorf("open the terminal: %w", err)
	}
	return tty, nil
}

// execLauncher replaces this process with the launcher, started in dir: it does not return
// unless the launcher cannot be started.
func execLauncher(path string, argv []string, dir string) error {
	return launcherVia(execx.OS{}, path, argv, dir)
}

// launcherVia replaces the process through x, the one seam that may. argv
// starts with the launcher's own name, which x supplies from path.
func launcherVia(x execx.Execer, path string, argv []string, dir string) error {
	var args []string
	if len(argv) > 1 {
		args = argv[1:]
	}
	if err := x.Exec(execx.Cmd{Name: path, Args: args, Dir: dir}); err != nil {
		return fmt.Errorf("start the launcher: %w", err)
	}
	return nil
}
