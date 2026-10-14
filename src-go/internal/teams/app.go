// Package teams is ccy-teams: the host command that creates, applies and removes agent teams.
package teams

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

//go:embed usage.txt
var usage string

// The two flags of ccy and ccy-teams that several flows spell.
const (
	flagTeams = "--teams"
	flagAll   = "--all"
)

var errCheckoutGone = errors.New("the fedora-desktop checkout has gone")

// App is one run of ccy-teams. Every external effect goes through Runner, Stdout and Stderr,
// and each tool is reached through an adapter built over them. A test sets Runner (an
// execx.Fake) and the streams, may replace any adapter, and wire fills in the rest.
type App struct {
	Config

	Runner execx.Runner
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string

	Term    *term.Term
	Sudo    *sudo.Sudo
	Systemd *systemd.Systemd
	Tmux    *tmuxccy.Tmux
	Git     *gitx.Git
	Declare *declare.Client
	Bus     *agentbus.Client
	// Registry finds the session registry, which depends on the environment.
	Registry func() (*registry.Store, error)

	// Gum asks the questions, Picker is the fzf list every choice is made in.
	Gum    *ui.Gum
	Picker *ui.Picker
	// Ops is the flows the picker calls into; they are written apart from it.
	Ops teamOperator

	// LookPath finds a tool on PATH. OpenTTY opens the terminal, which a picker reads after
	// fzf has had the screen. Exec replaces this process with the launcher, in dir.
	LookPath func(string) (string, error)
	OpenTTY  func() (io.ReadCloser, error)
	Exec     func(path string, argv []string, dir string) error

	inContainer func() bool
}

// teamOperator is the flows the picker hands off to: each takes the arguments the command line
// would, and answers the exit code that command would. applyTeams brings the named teams to
// what is declared; runElementPlay installs Element and writes the teams' profiles.
type teamOperator interface {
	create(ctx context.Context, args []string) int
	apply(ctx context.Context, args []string) int
	remove(ctx context.Context, args []string) int
	applyTeams(ctx context.Context, teams []string) error
	runElementPlay(ctx context.Context, teams []string) error
}

// NewApp is the production App: the real OS and the process's own streams.
func NewApp(cfg Config) *App {
	a := &App{
		Config: cfg, Runner: execx.OS{}, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		inContainer: func() bool {
			info, err := os.Stat("/workspace")
			return err == nil && info.IsDir()
		},
	}
	a.wire()
	return a
}

// Run dispatches args (without the program name) and returns the exit code.
func (a *App) Run(args []string) int {
	a.wire()
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "create", "apply", "remove", "backup", "restore":
	case "-l", "--list":
		if len(args) > 1 {
			return a.usageError(fmt.Sprintf("'%s' takes no further arguments.", sub))
		}
	case "-h", "--help":
		a.print(a.Stdout, usage)
		return term.ExitOK
	default:
		return a.usageError(fmt.Sprintf("unknown option '%s'.", sub))
	}
	if !a.ccyInstalled() {
		return term.ExitFailure
	}
	return a.dispatch(context.Background(), sub, args[min(1, len(args)):])
}

// wire builds every adapter that is not set yet over Runner and the streams. Tools are found
// by bare name on PATH; only the two sudo targets are absolute.
func (a *App) wire() {
	if a.Runner == nil {
		a.Runner = execx.OS{}
	}
	if a.Stdin == nil {
		a.Stdin = os.Stdin
	}
	if a.Stdout == nil {
		a.Stdout = os.Stdout
	}
	if a.Stderr == nil {
		a.Stderr = os.Stderr
	}
	if a.Getenv == nil {
		a.Getenv = os.Getenv
	}
	if a.Term == nil {
		a.Term = a.freshTerm()
	}
	if a.Sudo == nil {
		a.Sudo = &sudo.Sudo{Runner: a.Runner, Term: a.Term, Stdin: a.Stdin, Stdout: a.Stdout}
	}
	if a.Systemd == nil {
		a.Systemd = &systemd.Systemd{Runner: a.Runner}
	}
	if a.Tmux == nil {
		a.Tmux = &tmuxccy.Tmux{Runner: a.Runner, Term: a.Term, Stdin: a.Stdin, Stdout: a.Stdout, Stderr: a.Stderr}
	}
	if a.Git == nil {
		a.Git = gitx.New(a.Runner)
	}
	if a.Declare == nil {
		a.Declare = &declare.Client{Runner: a.Runner, Root: a.RootDir, File: a.HostVars, Err: a.Stderr}
	}
	if a.Bus == nil {
		a.Bus = &agentbus.Client{Runner: a.Runner, Root: a.Sudo, Err: a.Stderr}
	}
	if a.Registry == nil {
		a.Registry = func() (*registry.Store, error) { return registry.Locate(a.Getenv) }
	}
	a.wireAsking()
}

// wireAsking builds what the questions and the picker are asked through: gum, fzf, the
// operator the picker hands flows to, and the seams to the terminal and the launcher.
func (a *App) wireAsking() {
	if a.Gum == nil {
		a.Gum = &ui.Gum{Runner: a.Runner, Stdin: a.Stdin, Stderr: a.Stderr}
	}
	if a.Picker == nil {
		a.Picker = &ui.Picker{Runner: a.Runner, Stderr: a.Stderr}
	}
	if a.Ops == nil {
		a.Ops = liveOps{app: a}
	}
	if a.LookPath == nil {
		a.LookPath = exec.LookPath
	}
	if a.OpenTTY == nil {
		a.OpenTTY = openTerminal
	}
	if a.Exec == nil {
		a.Exec = execLauncher
	}
}

// dispatch hands the validated subcommand to the file that implements it.
func (a *App) dispatch(ctx context.Context, sub string, rest []string) int {
	switch sub {
	case "create":
		return a.create(ctx, rest)
	case "apply":
		return a.apply(ctx, rest)
	case "remove":
		return a.remove(ctx, rest)
	case "backup":
		return a.backup(ctx, rest)
	case "restore":
		return a.restore(ctx, rest)
	case "-l", "--list":
		return a.list(ctx)
	default:
		return a.picker(ctx)
	}
}

// freshTerm is a Term over the App's streams with no steps counted yet: a flow that numbers
// its steps has one of its own.
func (a *App) freshTerm() *term.Term {
	return &term.Term{Err: a.Stderr, IsTerminal: term.FileIsTerminal, Getenv: a.Getenv}
}

// reportErr prints why something failed, as the tool that failed said it: a "declare: ..."
// line stays that line, so the "(the reason is above)" of the message after it is true.
func (a *App) reportErr(err error) {
	a.print(a.Stderr, err.Error()+"\n")
}

func (a *App) usageError(msg string) int {
	a.print(a.Stderr, "ccy-teams: "+msg+" See ccy-teams --help.\n")
	return term.ExitUsage
}

// ccyInstalled refuses to go on without the CCY lib, saying why: inside a container ccy-teams
// is simply in the wrong place.
func (a *App) ccyInstalled() bool {
	lib := filepath.Join(a.CcyLib, "tmux-session.bash")
	if _, err := os.Stat(lib); err == nil {
		return true
	}
	if a.inContainer() {
		a.print(a.Stderr, "ccy-teams runs on the host, not inside a CCY container.\n")
		return false
	}
	a.print(a.Stderr, "CCY is not installed here ("+lib+" missing). Run playbooks/imports/play-claude-yolo.yml.\n")
	return false
}

// needCheckout refuses when the checkout ccy-teams was built from has moved or gone.
func (a *App) needCheckout() error {
	if _, err := os.Stat(filepath.Join(a.RootDir, "helpers", "agent_bus", "declare.py")); err == nil {
		return nil
	}
	a.print(a.Stderr, "ERROR: the fedora-desktop checkout ccy-teams was deployed from ("+a.RootDir+
		") has gone; re-run playbooks/imports/play-claude-yolo.yml from where it is now.\n")
	return errCheckoutGone
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

// launcherVia enters dir and replaces the process through x, the one seam that may. argv
// starts with the launcher's own name, which x supplies from path.
func launcherVia(x execx.Execer, path string, argv []string, dir string) error {
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("enter %s: %w", dir, err)
	}
	var args []string
	if len(argv) > 1 {
		args = argv[1:]
	}
	if err := x.Exec(execx.Cmd{Name: path, Args: args}); err != nil {
		return fmt.Errorf("start the launcher: %w", err)
	}
	return nil
}

// print writes to a terminal stream; a failed write there has nowhere better to be reported.
func (a *App) print(w io.Writer, s string) {
	if _, err := io.WriteString(w, s); err != nil {
		return
	}
}
