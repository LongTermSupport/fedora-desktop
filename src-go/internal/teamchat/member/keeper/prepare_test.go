package keeper

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/agent"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const otherDigest = "fedcba9876543210fedcba9876543210"

// plainLaunch is a host launch of a plain command with no team.
var plainLaunch = []string{"--plain", "--kind", "host", "--", "sleep", "60"}

// refused runs a keeper that must refuse its launch with 78, its stderr naming code.
func (w *world) refused(code string) {
	w.t.Helper()
	exitOf(w.t, w.start(), exits.ExitConfig)
	if !strings.Contains(w.stderr.String(), `"code":"`+code+`"`) {
		w.t.Fatalf("stderr %q, want code %s", w.stderr.String(), code)
	}
	if len(w.starter.Starts()) != 0 {
		w.t.Fatal("a link or CMD was started")
	}
}

// RN1: a runtime parent that is not the user's own, mode 0700, is 78 runtime.
func TestRuntimeParentMustBePrivate(t *testing.T) {
	w := newWorld(t, plainLaunch...)
	mustDo(t, os.Chmod(w.rt, 0o755))
	w.refused(exits.CodeRuntime)
}

// ccy's run root is made without a parent, and must still be the user's own, mode 0700.
func TestRunRootWithoutParent(t *testing.T) {
	t.Run("refused", func(t *testing.T) {
		w := newWorld(t, plainLaunch...)
		mustDo(t, os.MkdirAll(w.layout.RunRoot(), 0o755))
		mustDo(t, os.Chmod(w.layout.RunRoot(), 0o755))
		cfg := w.config()
		cfg.RuntimeParent = ""
		exitOf(t, w.startWith(cfg), exits.ExitConfig)
	})
	t.Run("made", func(t *testing.T) {
		w := newWorld(t, plainLaunch...)
		cfg := w.config()
		cfg.RuntimeParent = ""
		w.startPlain(cfg).finish(0)
	})
	t.Run("cannot be made", func(t *testing.T) {
		w := newWorld(t, plainLaunch...)
		mustDo(t, os.MkdirAll(filepath.Dir(w.layout.RunRoot()), 0o700))
		mustDo(t, os.WriteFile(w.layout.RunRoot(), nil, 0o600))
		cfg := w.config()
		cfg.RuntimeParent = ""
		exitOf(t, w.startWith(cfg), exits.ExitConfig)
	})
	t.Run("parent cannot hold it", func(t *testing.T) {
		w := newWorld(t, plainLaunch...)
		mustDo(t, os.MkdirAll(filepath.Dir(w.layout.RunRoot()), 0o700))
		mustDo(t, os.WriteFile(w.layout.RunRoot(), nil, 0o600))
		w.refused(exits.CodeRuntime)
	})
}

// RN1: --kit is used as given; the current link is never read.
func TestKitFlagNeverReadsCurrent(t *testing.T) {
	w := newWorld(t, plainLaunch...)
	mustDo(t, os.Remove(w.kitRoot+"/current"))
	mustDo(t, os.Symlink(otherDigest, w.kitRoot+"/current"))
	w.parse(append([]string{"--kit", w.kit.String()}, plainLaunch...)...)
	p := w.startPlain(w.config())
	if v := p.c.env(paths.EnvKit); v != w.kit.String() {
		t.Fatalf("TEAMCHAT_KIT %q", v)
	}
	if v := p.c.env("PATH"); v != w.kit.String()+"/bin:/usr/bin" {
		t.Fatalf("PATH %q", v)
	}
	p.c.code(3)
	if end := p.l.expect(t, wire.KindEnd); end.End.How != wire.EndExit || end.End.Status != 3 {
		t.Fatalf("end %+v", end.End)
	}
	p.l.code(0)
	exitOf(t, p.res, 3)
}

// RN1: a kit of another version is re-executed with --kit and the same arguments.
func TestOtherKitIsReexecuted(t *testing.T) {
	w := newWorld(t, plainLaunch...)
	cfg := w.config()
	cfg.Digest = otherDigest
	exitOf(t, w.startWith(cfg), term.ExitOK)
	execs := w.execer.Execs()
	want := append([]string{w.kit.Binary(), "run", "--kit", w.kit.String()}, plainLaunch...)
	if len(execs) != 1 || !hasArgs(execs[0], want...) {
		t.Fatalf("execs %v", execs)
	}
	if !w.runGone() {
		t.Fatal("a run was made before the re-exec")
	}
}

// A kit named by its digest must print that digest; a mismatch under --kit is 78, never a loop.
// hangingRunner is a kit whose --version never answers: Run returns only when ctx ends.
type hangingRunner struct{}

func (hangingRunner) Run(ctx context.Context, _ execx.Cmd) (execx.Result, error) {
	<-ctx.Done()
	return execx.Result{ExitCode: -1}, ctx.Err()
}

func TestKitVersionChecks(t *testing.T) {
	t.Run("named kit prints another", func(t *testing.T) {
		w := newWorld(t, plainLaunch...)
		w.runner.On([]string{w.kit.Binary(), "--version"}, execx.Result{Stdout: []byte(otherDigest + "\n")})
		w.refused(exits.CodeRuntime)
	})
	t.Run("kit flag of another version", func(t *testing.T) {
		w := newWorld(t, plainLaunch...)
		w.parse(append([]string{"--kit", w.kit.String()}, plainLaunch...)...)
		cfg := w.config()
		cfg.Digest = otherDigest
		exitOf(t, w.startWith(cfg), exits.ExitConfig)
		if len(w.execer.Execs()) != 0 {
			t.Fatal("re-executed under --kit")
		}
	})
	for name, out := range map[string]string{
		"two lines": digest + "\n" + digest + "\n", "no digest": "teamchat\n", "no newline": digest,
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, plainLaunch...)
			w.runner.On([]string{w.kit.Binary(), "--version"}, execx.Result{Stdout: []byte(out)})
			w.refused(exits.CodeRuntime)
		})
	}
	t.Run("version exits non-zero", func(t *testing.T) {
		w := newWorld(t, plainLaunch...)
		w.runner.On([]string{w.kit.Binary(), "--version"}, execx.Result{Stdout: []byte(digest + "\n"), ExitCode: 1})
		w.refused(exits.CodeRuntime)
	})
	t.Run("version hangs", func(t *testing.T) {
		w := newWorld(t, plainLaunch...)
		cfg := w.config()
		cfg.Runner = hangingRunner{}
		res := w.startWith(cfg)
		mustDo(t, w.clk.AwaitDue(t.Context(), versionWait))
		w.clk.Advance(versionWait)
		exitOf(t, res, exits.ExitConfig)
		if !strings.Contains(w.stderr.String(), `"code":"`+exits.CodeRuntime+`"`) {
			t.Fatalf("stderr %q", w.stderr.String())
		}
	})
	t.Run("version cannot run", func(t *testing.T) {
		w := newWorld(t, plainLaunch...)
		w.runner.OnError([]string{w.kit.Binary(), "--version"}, errors.New("no binary"))
		w.refused(exits.CodeRuntime)
	})
	t.Run("current is not a digest under the root", func(t *testing.T) {
		w := newWorld(t, plainLaunch...)
		mustDo(t, os.Remove(w.kitRoot+"/current"))
		mustDo(t, os.Symlink("../elsewhere/"+digest, w.kitRoot+"/current"))
		w.refused(exits.CodeRuntime)
	})
	t.Run("current names no digest", func(t *testing.T) {
		w := newWorld(t, plainLaunch...)
		mustDo(t, os.Remove(w.kitRoot+"/current"))
		mustDo(t, os.Symlink("latest", w.kitRoot+"/current"))
		w.refused(exits.CodeRuntime)
	})
	t.Run("current missing", func(t *testing.T) {
		w := newWorld(t, plainLaunch...)
		mustDo(t, os.Remove(w.kitRoot+"/current"))
		w.refused(exits.CodeRuntime)
	})
	t.Run("current absolute", func(t *testing.T) {
		w := newWorld(t, plainLaunch...)
		mustDo(t, os.Remove(w.kitRoot+"/current"))
		mustDo(t, os.Symlink(w.kit.String(), w.kitRoot+"/current"))
		w.startPlain(w.config()).finish(0)
	})
	t.Run("reexec fails", func(t *testing.T) {
		w := newWorld(t, plainLaunch...)
		w.execer.ExecError(errors.New("exec refused"))
		cfg := w.config()
		cfg.Digest = otherDigest
		exitOf(t, w.startWith(cfg), exits.ExitConfig)
	})
}

// ccy's one kit is its kit root, named by no digest: no current link is read.
func TestOneKitLayout(t *testing.T) {
	w := newWorld(t, plainLaunch...)
	layout, err := w.layout.WithKitRoot(w.kit.String())
	mustDo(t, err)
	w.layout = layout
	w.parse(plainLaunch...)
	if w.layout.CurrentKit().String() == w.layout.KitRoot() {
		w.startPlain(w.config()).finish(0)
		return
	}
	exitOf(t, w.start(), exits.ExitConfig)
}

// B 2.4.1 step 5's settings: the kit's, with the launch's inbound decision always written.
func TestSettingsCarryInbound(t *testing.T) {
	w := newWorld(t, claudeLaunch...)
	w.writeDesired(desiredJSON(1, state.InboundRefuse))
	l, c, res := w.running(0)
	data, err := os.ReadFile(w.runDir().Settings())
	mustDo(t, err)
	if string(data) != `{"crossSessionInbound":"refuse","statusLine":{"type":"command"}}` {
		t.Fatalf("settings %s", data)
	}
	info, err := os.Stat(w.runDir().Settings())
	mustDo(t, err)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("settings mode %v", info.Mode())
	}
	c.code(0)
	l.expect(t, wire.KindEnd)
	l.code(0)
	exitOf(t, res, term.ExitOK)
}

func TestSettingsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"has the key":  `{"crossSessionInbound":"accept"}`,
		"not object":   `[1]`,
		"null":         `null`,
		"not json":     `{`,
		"over the cap": `{"a":"` + strings.Repeat("x", agent.MaxSettings) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, claudeLaunch...)
			w.writeDesired(desiredJSON(1, state.InboundAccept))
			mustDo(t, os.WriteFile(w.kit.Settings(), []byte(body), 0o644))
			w.refused(exits.CodeRuntime)
			if !w.runGone() {
				t.Fatal("the refused launch left its run")
			}
		})
	}
	t.Run("group writable", func(t *testing.T) {
		w := newWorld(t, claudeLaunch...)
		w.writeDesired(desiredJSON(1, state.InboundAccept))
		mustDo(t, os.Chmod(w.kit.Settings(), 0o664))
		w.refused(exits.CodeRuntime)
	})
	t.Run("a link", func(t *testing.T) {
		w := newWorld(t, claudeLaunch...)
		w.writeDesired(desiredJSON(1, state.InboundAccept))
		mustDo(t, os.Rename(w.kit.Settings(), w.dir+"/real.json"))
		mustDo(t, os.Symlink(w.dir+"/real.json", w.kit.Settings()))
		w.refused(exits.CodeRuntime)
	})
	t.Run("a directory", func(t *testing.T) {
		w := newWorld(t, claudeLaunch...)
		w.writeDesired(desiredJSON(1, state.InboundAccept))
		mustDo(t, os.Remove(w.kit.Settings()))
		mustDo(t, os.Mkdir(w.kit.Settings(), 0o755))
		w.refused(exits.CodeRuntime)
	})
}

// The desired membership: missing, undecodable, or not given to a ccy or lxc launch is 78
// membership; a host launch without it has inbound refuse.
func TestDesiredAtLaunch(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		w := newWorld(t, claudeLaunch...)
		w.refused(exits.CodeMembership)
		if len(w.watch.dirs) != 1 || w.watch.dirs[0] != w.desired {
			t.Fatalf("watched %v", w.watch.dirs)
		}
	})
	t.Run("undecodable", func(t *testing.T) {
		w := newWorld(t, claudeLaunch...)
		w.writeDesired(`{"v":1}`)
		w.refused(exits.CodeMembership)
	})
	t.Run("watch fails", func(t *testing.T) {
		w := newWorld(t, claudeLaunch...)
		w.watch.fail(errors.New("no inotify"))
		w.refused(exits.CodeRuntime)
	})
	for kind, extra := range map[string][]string{"ccy": nil, "lxc": {"--key", "0123456789abcdef"}} {
		t.Run(kind+" without desired", func(t *testing.T) {
			args := append([]string{"--plain", "--kind", kind}, extra...)
			w := newWorld(t, append(args, "--", "sleep", "1")...)
			w.refused(exits.CodeMembership)
		})
	}
	t.Run("host without desired", func(t *testing.T) {
		w := newWorld(t, "--claude", "--kind", "host", "--", "claude")
		l, c, res := w.running(0)
		data, err := os.ReadFile(w.runDir().Settings())
		mustDo(t, err)
		if !strings.Contains(string(data), `"crossSessionInbound":"refuse"`) {
			t.Fatalf("settings %s", data)
		}
		pid := w.sessionPID()
		if pid.Inbound != state.InboundRefuse || pid.Launch != state.LaunchClaude || pid.Session == nil ||
			pid.Session.PID != c.pid || pid.Keeper.StartTicks != 7 {
			t.Fatalf("session.pid %+v", pid)
		}
		if k := w.keeperJSON(); k.Session != nil || k.Kind != state.KindHost {
			t.Fatalf("keeper.json %+v", k)
		}
		c.code(0)
		l.expect(t, wire.KindEnd)
		l.code(0)
		exitOf(t, res, term.ExitOK)
	})
}

// T8: a runtime left behind is removed when its keeper.lock is free and its keeper and session
// are gone, or it has no session.pid; one whose keeper.lock is held, whose keeper or session
// lives, or whose session.pid does not decode is left.
func TestLeftoverSweep(t *testing.T) {
	w := newWorld(t, plainLaunch...)
	rr := w.layout.RunRoot()
	mustDo(t, os.MkdirAll(rr, 0o700))
	mk := func(id string, pid string) string {
		dir := rr + "/" + id
		mustDo(t, os.Mkdir(dir, 0o700))
		if pid != "" {
			mustDo(t, os.WriteFile(dir+"/session.pid", []byte(pid), 0o600))
		}
		return dir
	}
	spid := func(id string, keeper, session string) string {
		return `{"v":1,"run":"` + id + `","keeper":` + keeper + `,"session":` + session +
			`,"kit":"` + digest + `","launch":"plain","inbound":"refuse","kind":"host"}`
	}
	w.proc.set(900, 90)
	w.proc.set(901, 91)
	gone := mk("aaaaaaaaaaaaaaaa", spid("aaaaaaaaaaaaaaaa", `{"pid":800,"start_ticks":1}`, `{"pid":900,"start_ticks":5}`))
	bare := mk("bbbbbbbbbbbbbbbb", "")
	alive := mk("cccccccccccccccc", spid("cccccccccccccccc", `{"pid":800,"start_ticks":1}`, `{"pid":901,"start_ticks":91}`))
	bad := mk("dddddddddddddddd", "{")
	held := mk("eeeeeeeeeeeeeeee", spid("eeeeeeeeeeeeeeee", `{"pid":800,"start_ticks":1}`, "null"))
	keeperAlive := mk("ffffffffffffffff", spid("ffffffffffffffff", `{"pid":900,"start_ticks":90}`, "null"))
	mustDo(t, os.WriteFile(rr+"/not-a-run", nil, 0o600))
	mustDo(t, os.Mkdir(rr+"/not-a-run-id", 0o700))
	// A run being built is a staging directory named by its keeper: removed once that keeper
	// is gone, left while it lives.
	buildGone := mk(".building-800-1", "")
	buildAlive := mk(".building-901-91", "")
	holder := holdFile(t, rr, held+"/keeper.lock")
	p := w.startPlain(w.config())
	holder.release(t)
	for dir, want := range map[string]bool{
		gone: false, bare: false, alive: true, bad: true, held: true, keeperAlive: true,
		buildGone: false, buildAlive: true,
	} {
		if exists(t, dir) != want {
			t.Errorf("%s: exists %v, want %v", dir, !want, want)
		}
	}
	if !strings.Contains(w.logOf(p.run), "dddddddddddddddd") {
		t.Fatalf("keeper.log %q", w.logOf(p.run))
	}
	// The keeper's own run was built under its staging name and renamed into place whole.
	if exists(t, w.layout.Staging(os.Getpid(), 7).Dir()) || !exists(t, p.run.PID()) {
		t.Fatal("the run was not renamed into place with its session.pid")
	}
	p.finish(0)
}

// sessionPID is the run's session.pid.
func (w *world) sessionPID() state.SessionPID {
	w.t.Helper()
	data, err := os.ReadFile(w.runDir().PID())
	mustDo(w.t, err)
	p, err := state.DecodeSessionPID(data)
	mustDo(w.t, err)
	return p
}

// A plain CMD that cannot be started ends as exit 127.
func TestPlainCmdThatCannotStart(t *testing.T) {
	w := newWorld(t, plainLaunch...)
	w.starter.OnStart([]string{"sleep"}, func(execx.Cmd) (execx.Started, error) { return nil, errors.New("not found") })
	res := w.start()
	l := w.up(0)
	end := l.expect(t, wire.KindEnd)
	if end.End.How != wire.EndExit || end.End.Status != exitNoCmd {
		t.Fatalf("end %+v", end.End)
	}
	l.code(0)
	exitOf(t, res, exitNoCmd)
}
