// Package teams is ccy-teams: the host command that creates, applies and removes agent teams.
package teams

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

//go:embed usage.txt
var usage string

var errCheckoutGone = errors.New("the fedora-desktop checkout has gone")

// App is one run of ccy-teams. Every external effect goes through Runner, Stdout and Stderr.
type App struct {
	Config

	Runner      execx.Runner
	Stdout      io.Writer
	Stderr      io.Writer
	inContainer func() bool
}

// NewApp is the production App: the real OS and the process's own streams.
func NewApp(cfg Config) *App {
	return &App{
		Config: cfg, Runner: execx.OS{}, Stdout: os.Stdout, Stderr: os.Stderr,
		inContainer: func() bool {
			info, err := os.Stat("/workspace")
			return err == nil && info.IsDir()
		},
	}
}

// Run dispatches args (without the program name) and returns the exit code.
func (a *App) Run(args []string) int {
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
	return term.ExitOK
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

// print writes to a terminal stream; a failed write there has nowhere better to be reported.
func (a *App) print(w io.Writer, s string) {
	if _, err := io.WriteString(w, s); err != nil {
		return
	}
}
