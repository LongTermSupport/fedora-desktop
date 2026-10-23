package link

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// controlFD is the keeper's control socket, the link's descriptor 3 (B 2.2.1).
const controlFD = 3

const msgRunDir exits.Template = "teamchat link --run takes a session's runtime directory; the keeper starts the link"

// Wiring is what the link runs its teams with, built outside this package: a team and the
// session's ends.
type Wiring struct {
	NewTeam func(engine.TeamConfig) (team.Team, error)
	NewEnds func(engine.EndsConfig) team.Ends
}

// Main is `teamchat link --run DIR`, started by the keeper only: its control socket on fd 3,
// whose peer must be the parent; SIGTERM, SIGINT and SIGHUP ignored, so only end, the
// keeper's death or SIGKILL ends it; stdout and stderr are link.log.
func Main(w Wiring) cli.RunFunc {
	return func(e cli.Env, inv cli.Invocation) term.ExitCode {
		signal.Ignore(syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
		dir, given := inv.Flag("run")
		stateDir := ""
		if s, ok := inv.Flag("state"); ok {
			stateDir = s
		}
		layout, run, err := layoutFor(dir, stateDir, e.Getenv)
		if !given {
			err = exits.New(exits.CodeUsage, nil, msgRunDir)
		}
		if err != nil {
			return exits.Write(e.Stderr, err)
		}
		kit, err := paths.ParseDigest(e.Digest)
		if err != nil {
			return refuse(e.Stderr, "the kit's digest", err)
		}
		conn, err := control(os.NewFile(controlFD, "control"), os.Getppid())
		if err != nil {
			return refuse(e.Stderr, "the control socket", err)
		}
		stateRoot, err := homefs.Open(layout.State(), homefs.Private)
		if err != nil {
			return refuse(e.Stderr, "open the state root", errors.Join(err, conn.Close()))
		}
		runRoot, err := homefs.Open(layout.RunRoot(), homefs.Private)
		if err != nil {
			return refuse(e.Stderr, "open the run root", errors.Join(err, conn.Close(), stateRoot.Close()))
		}
		code := Run(context.Background(), Config{
			StateRoot: stateRoot, RunRoot: runRoot, Layout: layout, Run: run, Control: conn,
			Clock: clock.System(), Proc: OSProc{Dir: "/proc"}, Parent: OpenParent, UID: os.Getuid(),
			Kit: kit, RuntimeDir: e.Getenv("XDG_RUNTIME_DIR"), Home: e.Getenv("HOME"), Log: e.Stderr,
			Engine: engine.New, NewTeam: w.NewTeam, NewEnds: w.NewEnds,
		})
		if err := errors.Join(stateRoot.Close(), runRoot.Close()); err != nil {
			return refuse(e.Stderr, "close the roots", err)
		}
		return code
	}
}

// layoutFor is the layout whose runtime directory for its base name is dir: a ccy session's,
// else the home member's under $HOME and $XDG_RUNTIME_DIR; with state ("" for none) as its
// state root, run's --state the keeper passes on.
func layoutFor(dir, state string, getenv func(string) string) (paths.Layout, paths.RunID, error) {
	l, run, err := runLayout(dir, getenv)
	if err != nil || state == "" {
		return l, run, err
	}
	if l, err = l.WithState(state); err != nil {
		return paths.Layout{}, paths.RunID{}, exits.New(exits.CodeUsage, nil, msgRunDir)
	}
	return l, run, nil
}

func runLayout(dir string, getenv func(string) string) (paths.Layout, paths.RunID, error) {
	run, err := paths.ParseRunID(filepath.Base(dir))
	if err != nil {
		return paths.Layout{}, paths.RunID{}, exits.New(exits.CodeUsage, nil, msgRunDir)
	}
	layouts := []paths.Layout{paths.CCYLayout()}
	if home, homeErr := paths.HomeLayout(getenv("HOME"), getenv("XDG_RUNTIME_DIR")); homeErr == nil {
		layouts = append(layouts, home)
	}
	for _, l := range layouts {
		if l.Run(run).Dir() == dir {
			return l, run, nil
		}
	}
	return paths.Layout{}, paths.RunID{}, exits.New(exits.CodeUsage, nil, msgRunDir)
}

// control is f as the control socket, whose peer must be ppid: a link is the keeper's child.
func control(f *os.File, ppid int) (*wire.Conn, error) {
	c, err := wire.NewConn(f)
	if err != nil {
		return nil, err
	}
	peer, err := c.PeerPID()
	if err == nil && peer != ppid {
		err = fmt.Errorf("link: the control socket's peer is %d, not the parent %d", peer, ppid)
	}
	if err != nil {
		return nil, errors.Join(err, c.Close())
	}
	return c, nil
}
