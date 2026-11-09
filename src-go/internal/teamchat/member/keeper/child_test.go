package keeper

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/reexec"
)

// childCmdPID is the pid the child keeper's in-process CMD claims: none that runs.
const childCmdPID = 4194000

// runKeeperChild is the "keeper" helper: a keeper in a process of its own over the world a
// test made at Args[0], its link and CMD in-process fakes, so an armed failpoint (Args[1])
// can SIGKILL it as a crash would. The link reports a team once ready and exits on the end;
// CMD ends at once for the end's point and never for the others.
func runKeeperChild(h *reexec.Helper) int {
	cfg, err := childConfig(h.Args[0], failpoint.Name(h.Args[1]))
	if err != nil {
		return 2
	}
	return int(Run(cfg, os.Stdout, os.Stderr))
}

func childConfig(dir string, point failpoint.Name) (Config, error) {
	launch, err := cli.ParseLaunch(plainLaunch)
	if err != nil {
		return Config{}, err
	}
	layout, err := paths.HomeLayout(dir+"/home", dir+"/rt")
	if err == nil {
		layout, err = layout.WithKitRoot(dir + "/kits")
	}
	if err == nil {
		layout, err = layout.WithState(dir + "/state")
	}
	if err != nil {
		return Config{}, err
	}
	kit, err := paths.KitAt(dir + "/kits/" + digest)
	if err != nil {
		return Config{}, err
	}
	runner, starter := &execx.Fake{}, &execx.Fake{}
	runner.On([]string{kit.Binary(), "--version"}, execx.Result{Stdout: []byte(digest + "\n")})
	starter.OnStart([]string{linkName}, childLink)
	starter.OnStart([]string{launch.Cmd[0]}, func(execx.Cmd) (execx.Started, error) {
		c := &child{pid: childCmdPID, sigs: make(chan syscall.Signal, 16), done: make(chan struct{})}
		if point == failpoint.KeeperBeforeEnd {
			c.code(0)
		}
		return c, nil
	})
	return Config{
		Launch: launch, Layout: layout, RuntimeParent: dir + "/rt", Digest: digest, Args: plainLaunch,
		Env: os.Getenv, Cwd: "/work/project-a", Clock: clock.System(), Locks: lock.SystemClock(),
		Runner: runner, Execer: &execx.Fake{}, Starter: starter,
		LinkCmd: func(run paths.Run, _ paths.Kit) execx.Cmd {
			return execx.Cmd{Name: linkName, Args: []string{"link", "--run", run.Dir()}}
		},
		Proc: procStat{}, Pidfds: osPidfds{}, Signals: make(chan os.Signal),
	}, nil
}

// childLink is the child keeper's link: ready once launched, then a report of a team.
func childLink(c execx.Cmd) (execx.Started, error) {
	if len(c.ExtraFiles) != 1 {
		return nil, errors.New("the link has no control socket")
	}
	fd, err := unix.Dup(int(c.ExtraFiles[0].Fd()))
	if err != nil {
		return nil, err
	}
	conn, err := wire.NewConn(os.NewFile(uintptr(fd), "link-end"))
	if err != nil {
		return nil, err
	}
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		return nil, err
	}
	l := &child{pid: childCmdPID + 1, sigs: make(chan syscall.Signal, 16), done: make(chan struct{})}
	l.onEx = func() { mustClose(conn) }
	go func() {
		if _, _, rerr := conn.Recv(); rerr != nil {
			return
		}
		ready := conn.Send(wire.Msg{Kind: wire.KindReady, Ready: &wire.Ready{}}, nil)
		st := wire.State{Team: team, Handle: "repo.1+desk.host", Delivery: state.DeliverySocket, Store: state.StoreOK}
		if ready != nil || conn.Send(wire.Msg{Kind: wire.KindState, State: &st}, nil) != nil {
			return
		}
		for {
			if m, _, rerr := conn.Recv(); rerr != nil || m.Kind == wire.KindEnd {
				l.code(0)
				return
			}
		}
	}()
	return l, nil
}
