package link

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/reexec"
)

const (
	helperHold = "hold"
	// helperLink is `teamchat link` in a process of its own: its arguments are the home, the
	// run directory, and the failpoints to arm (may be empty).
	helperLink = "link"
	// failpointsEnv is the failpoint package's list of armed points.
	failpointsEnv = "TEAMCHAT_FAILPOINTS"
)

var helpers = reexec.Helpers{
	helperHold: func(h *reexec.Helper) int {
		if err := h.Ready(); err != nil {
			return 2
		}
		return 0
	},
	helperLink: runLinkHelper,
}

func TestMain(m *testing.M) { os.Exit(reexec.Main(m, helpers)) }

// runLinkHelper runs Main over teams whose re-fetch delivers, while its ready line waits for
// a release it never needs: the link's own exit ends the process.
func runLinkHelper(h *reexec.Helper) int {
	home, dir, armed := h.Args[0], h.Args[1], h.Args[2]
	if armed != "" {
		if err := os.Setenv(failpointsEnv, armed); err != nil {
			return 2
		}
	}
	go func() {
		if err := h.Ready(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()
	inv, err := cli.Parse([]string{"link", "--run", dir})
	if err != nil {
		return 2
	}
	env := map[string]string{"HOME": home, "XDG_RUNTIME_DIR": home + "/r"}
	w := Wiring{
		NewTeam: func(cfg engine.TeamConfig) (team.Team, error) {
			return &fakeTeam{notices: &fakeNotices{self: agent(cfg.Team)}, deliver: true}, nil
		},
		NewEnds: func(engine.EndsConfig) team.Ends { return &fakeEnds{} },
	}
	e := cli.Env{Stdout: os.Stdout, Stderr: os.Stderr, Digest: kitHex, Getenv: func(k string) string { return env[k] }}
	return int(Main(w)(e, inv))
}

// startProcess is a link in a process of its own, given linkEnd as its control socket.
func (w *linkWorld) startProcess(linkEnd *os.File, armed string) *reexec.Process {
	w.t.Helper()
	p := reexec.StartFiles(w.t, []*os.File{linkEnd}, helperLink, w.dir, w.layout.Run(w.run).Dir(), armed)
	check(w.t, linkEnd.Close())
	return p
}

// released lets p's ready line go and is closed once p has exited 0.
func released(p *reexec.Process) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Release()
	}()
	return done
}

// boot is the real boot clock, which a link in its own process judges deadlines by.
func boot() time.Duration { return clock.System().Boot() }

func (w *linkWorld) await(done <-chan struct{}) {
	w.t.Helper()
	select {
	case <-done:
	case <-w.t.Context().Done():
		w.t.Fatal("the link process never exited")
	}
}

func TestProcIgnoresStopSignals(t *testing.T) {
	w, linkEnd := newKeeperWorld(t)
	p := w.startProcess(linkEnd, "")
	done := released(p)
	w.launch(0)
	w.next(wire.KindReady)
	for _, sig := range []unix.Signal{unix.SIGTERM, unix.SIGINT, unix.SIGHUP} {
		check(t, unix.Kill(p.Pid, sig))
	}
	if s := w.request(wire.OpStatus, boot()+2*time.Second); !strings.Contains(s, `"kind":"runtime"`) {
		t.Fatalf("status %q", s)
	}
	w.send(wire.Msg{Kind: wire.KindEnd, End: &wire.End{How: wire.EndStop}}, nil)
	w.await(done)
}

func TestProcEndsWithKeeper(t *testing.T) {
	w, linkEnd := newKeeperWorld(t)
	p := w.startProcess(linkEnd, "")
	done := released(p)
	w.launch(1)
	w.add(tid(t, "alpha"))
	w.next(wire.KindReady)
	check(t, w.conn.Close())
	w.await(done)
	if !w.linkLockFree() {
		t.Fatal("link.lock is held after the link exited")
	}
}
