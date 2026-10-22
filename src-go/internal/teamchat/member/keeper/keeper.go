// Package keeper is `teamchat run`, the session's keeper (Plan 00209 B 2.4.1, 2.4.2, 2.10;
// SPEC-n3 RN1-RN3, KP1-KP3, RC1, LN8's keeper half): the parent of CMD for the session's
// life. It resolves the kit, sweeps the runtimes left behind, makes the session's runtime
// directory, holds the seat locks, starts and supervises the link, follows the desired
// membership, forwards a stop to Claude Code and ends the session.
//
// The keeper makes no network call and reads no token (B14), so a homeserver that cannot be
// reached, a team that is down or a peer that is offline never reaches it; each of its waits is
// bounded. It learns of CMD's end as its parent and of Claude Code's by a pidfd, never by a pid
// compared across namespaces or PR_SET_PDEATHSIG (B15). The inbox socket token the link hands
// it lives only in its memory (B11).
//
// Steps 1-5 of B 2.4.1 report a failure on stderr as an error object; from CMD's start on the
// keeper writes nothing to the terminal, only to keeper.log (B47). The loop is one goroutine:
// each child's wait, the link's reader and every timer post a closure to it.
package keeper

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Proc reads a process's start time, field 22 of /proc/<pid>/stat, so a reused pid is never
// taken for the process (B 2.4.1 step 4).
type Proc interface {
	StartTicks(pid int) (int64, error)
}

// Pidfd is a process held by a pidfd: a signal reaches that process or none, and Exited is
// closed once it has ended.
type Pidfd interface {
	Signal(sig syscall.Signal) error
	Exited() <-chan struct{}
	Close() error
}

// Pidfds opens a pidfd on a process.
type Pidfds interface {
	Open(pid int) (Pidfd, error)
}

// WatchEvent is one inotify event of the desired directory: the name it is about and its mask.
type WatchEvent struct {
	Name string
	Op   uint32
}

// Watcher watches the desired directory; the channel closes when the watch ends.
type Watcher interface {
	Watch(dir string) (<-chan WatchEvent, error)
	Close() error
}

// Config is what one keeper is given: the launch line, the layout it runs in, and every seam.
type Config struct {
	Launch cli.Launch
	// Layout has --state and --desired applied.
	Layout paths.Layout
	// RuntimeParent is $XDG_RUNTIME_DIR, opened Private; "" for ccy's container-private /tmp.
	RuntimeParent string
	// Digest is this binary's source digest.
	Digest string
	// Args are run's arguments after the verb, for the re-exec.
	Args []string
	Env  func(string) string
	// Cwd is run's working directory: the launch's cwd and the transcript's project directory.
	Cwd string
	// Stdin is CMD's.
	Stdin   io.Reader
	Clock   clock.Clock
	Locks   lock.Clock
	Runner  execx.Runner
	Execer  execx.Execer
	Starter execx.Starter
	// LinkCmd is the link's command: <kit>/bin/teamchat link --run <dir> [--state DIR] in
	// production.
	LinkCmd func(run paths.Run, kit paths.Kit) execx.Cmd
	Proc    Proc
	Pidfds  Pidfds
	Signals <-chan os.Signal
	Watcher Watcher
}

// The keeper's bounds (B 2.4.1, 2.4.2, 2.10; KP1-KP3, LN8).
const (
	readyWait     = 30 * time.Second
	removeWait    = 10 * time.Second
	owedWait      = 30 * time.Second
	endWait       = 5 * time.Second
	stopTermAfter = 5 * time.Second
	stopKillAfter = 7 * time.Second
	orphanTerm    = 3 * time.Second
	orphanKill    = 5 * time.Second
	stableRun     = 10 * time.Minute
	failedAfter   = 6
	maxSettings   = 64 << 10
)

const (
	envPath    = "PATH"
	envHome    = "HOME"
	envXDG     = "XDG_RUNTIME_DIR"
	envConfig  = "CLAUDE_CONFIG_DIR"
	inboundKey = "crossSessionInbound"
	exitNoCmd  = 127
	exitReexec = 0
)

// The keeper's messages; each names only grammar-checked values.
const (
	msgRuntimeParent exits.Template = "the runtime directory {} is missing or not this user's own, mode 0700"
	msgRunRoot       exits.Template = "the runtime root {} cannot be made or opened as this user's own, mode 0700"
	msgRunDir        exits.Template = "the session's runtime directory {} cannot be made"
	msgKit           exits.Template = "the kit {} cannot be resolved"
	msgKitVersion    exits.Template = "the kit {} does not answer --version with its digest"
	msgKitMismatch   exits.Template = "the kit {} is not this binary's version, and --kit names it"
	msgReexec        exits.Template = "the kit's teamchat {} cannot be run"
	msgNoDesired     exits.Template = "a {} launch needs --desired"
	msgDesired       exits.Template = "the desired membership {} is missing or cannot be read"
	msgWatch         exits.Template = "the desired directory {} cannot be watched"
	msgSettings      exits.Template = "the kit's settings {} are not a JSON object without the inbound key"
	msgSeatHeld      exits.Template = "the seat of team {} is held by another session"
	msgLinkDown      exits.Template = "the link did not come up; see {}"
	msgInternal      exits.Template = "the keeper could not {}"
)

// errExeced is the re-exec's return in a test, whose Exec returns.
var errExeced = errors.New("keeper: re-executed")

// Run is the keeper: B 2.4.1's steps, then the loop until CMD and Claude Code have ended and
// the link has had the end. Its exit is CMD's status, 78 for a launch failure.
func Run(cfg Config, stdout, stderr io.Writer) term.ExitCode {
	k := newKeeper(cfg, stdout, stderr)
	if err := k.prepare(); err != nil {
		if errors.Is(err, errExeced) {
			k.close(false)
			return exitReexec
		}
		k.logf("launch refused: %v", err)
		k.close(false)
		return exits.Write(stderr, err)
	}
	return k.loop()
}

// Main is the `run` verb: the production seams around Run.
func Main(e cli.Env, inv cli.Invocation) term.ExitCode {
	cfg, err := production(e, inv)
	if err != nil {
		return exits.Write(e.Stderr, err)
	}
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT)
	defer signal.Stop(sigs)
	cfg.Signals = sigs
	w, err := newInotify()
	if err != nil {
		return exits.Write(e.Stderr, fault(exits.CodeRuntime, msgInternal, exits.Words("watch files")))
	}
	cfg.Watcher = w
	// The keeper idles for the session's life: no collection cycle, and a small heap cap.
	debug.SetGCPercent(-1)
	debug.SetMemoryLimit(16 << 20)
	return Run(cfg, e.Stdout, e.Stderr)
}

// production is Main's Config without its signals and watcher.
func production(e cli.Env, inv cli.Invocation) (Config, error) {
	if inv.Launch == nil {
		return Config{}, fault(exits.CodeInternal, msgInternal, exits.Words("read the launch line"))
	}
	l := *inv.Launch
	layout, parent := paths.CCYLayout(), ""
	if l.Kind != cli.KindCCY {
		parent = e.Getenv(envXDG)
		if parent == "" {
			return Config{}, fault(exits.CodeRuntime, msgRuntimeParent, exits.Name(envXDG))
		}
		var err error
		if layout, err = paths.HomeLayout(e.Getenv(envHome), parent); err != nil {
			return Config{}, fault(exits.CodeRuntime, msgRuntimeParent, exits.Name(parent))
		}
	}
	layout, err := applyDirs(layout, l)
	if err != nil {
		return Config{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return Config{}, fault(exits.CodeRuntime, msgInternal, exits.Words("read the working directory"))
	}
	state := ""
	if l.State != "" {
		state = layout.State()
	}
	return Config{
		Launch: l, Layout: layout, RuntimeParent: parent, Digest: e.Digest, Args: inv.Args,
		Env: e.Getenv, Cwd: cwd, Stdin: e.Stdin, Clock: clock.System(), Locks: lock.SystemClock(),
		Runner: execx.OS{}, Execer: execx.OS{}, Starter: execx.OS{}, LinkCmd: linkCmd(state),
		Proc: procStat{}, Pidfds: osPidfds{},
	}, nil
}

// applyDirs is the layout with run's --state and --desired.
func applyDirs(layout paths.Layout, l cli.Launch) (paths.Layout, error) {
	var err error
	if l.State != "" {
		if layout, err = layout.WithState(l.State); err != nil {
			return layout, fault(exits.CodeRuntime, msgRunRoot, exits.Name(l.State))
		}
	}
	if l.Desired != "" {
		if layout, err = layout.WithDesired(l.Desired); err != nil {
			return layout, fault(exits.CodeMembership, msgDesired, exits.Name(l.Desired))
		}
	}
	return layout, nil
}

// linkCmd is the link's command in production: with run's --state passed on, so the link
// reads the seats the keeper holds.
func linkCmd(state string) func(run paths.Run, kit paths.Kit) execx.Cmd {
	return func(run paths.Run, kit paths.Kit) execx.Cmd {
		c := execx.Cmd{Name: kit.Binary(), Args: []string{"link", "--run", run.Dir()}}
		if state != "" {
			c.Args = append(c.Args, "--state", state)
		}
		return c
	}
}

// fault is an error object of code with a filled template.
func fault(code string, t exits.Template, fills ...exits.Fill) *exits.Error {
	return exits.New(code, nil, t, fills...)
}

// logf writes a line to keeper.log, or holds it until keeper.log is open. A failed write is
// lost: the log is the keeper's only channel once CMD runs.
func (k *keeper) logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...) + "\n"
	if k.log == nil {
		k.early = append(k.early, line)
		return
	}
	if _, err := io.WriteString(k.log, line); err != nil {
		k.logErr = err
	}
}

// openRunRoot is step 1: the root of the runtime directories, the user's own, mode 0700.
func (k *keeper) openRunRoot() error {
	rr := k.cfg.Layout.RunRoot()
	if k.cfg.RuntimeParent == "" {
		if err := os.MkdirAll(rr, 0o700); err != nil {
			k.logf("make %s: %v", rr, err)
			return fault(exits.CodeRuntime, msgRunRoot, exits.Name(rr))
		}
		root, err := homefs.Open(rr, homefs.Private)
		if err != nil {
			k.logf("open %s: %v", rr, err)
			return fault(exits.CodeRuntime, msgRunRoot, exits.Name(rr))
		}
		k.runRoot = root
		return nil
	}
	parent, err := homefs.Open(k.cfg.RuntimeParent, homefs.Private)
	if err != nil {
		k.logf("open %s: %v", k.cfg.RuntimeParent, err)
		return fault(exits.CodeRuntime, msgRuntimeParent, exits.Name(k.cfg.RuntimeParent))
	}
	defer k.closeQuietly(parent)
	if merr := parent.MkdirAll(rr); merr != nil {
		k.logf("make %s: %v", rr, merr)
		return fault(exits.CodeRuntime, msgRunRoot, exits.Name(rr))
	}
	if k.runRoot, err = parent.Sub(rr, homefs.Private); err != nil {
		k.logf("open %s: %v", rr, err)
		return fault(exits.CodeRuntime, msgRunRoot, exits.Name(rr))
	}
	return nil
}

// closeQuietly closes c, logging a failure.
func (k *keeper) closeQuietly(c io.Closer) {
	if err := c.Close(); err != nil {
		k.logf("close: %v", err)
	}
}
