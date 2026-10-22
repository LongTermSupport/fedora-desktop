package keeper

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/reexec"
)

// runMain is the "main" helper: Main over the helper's launch line and the real process,
// which it changes (signals, the collector), so it runs apart from the tests.
func runMain(h *reexec.Helper) int {
	l, err := cli.ParseLaunch(h.Args)
	if err != nil {
		return 2
	}
	e := cli.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Digest: digest, Getenv: os.Getenv}
	return int(Main(e, cli.Invocation{Launch: &l, Args: h.Args}))
}

// Main refuses a launch with no runtime directory, or one whose kit cannot be resolved, with
// 78 runtime on stderr.
func TestMainRefuses(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"/home", "/rt"} {
		mustDo(t, os.Mkdir(dir+d, 0o700))
	}
	for name, xdg := range map[string]string{"no kit": dir + "/rt", "no runtime directory": ""} {
		t.Run(name, func(t *testing.T) {
			c := reexec.Command(t, "main", plainLaunch...)
			c.Env = append(c.Env, "HOME="+dir+"/home", "XDG_RUNTIME_DIR="+xdg)
			res, err := execx.OS{}.Run(t.Context(), c)
			var exited *execx.ExitError
			if err != nil && !errors.As(err, &exited) {
				t.Fatal(err)
			}
			if res.ExitCode != int(exits.ExitConfig) || !strings.Contains(string(res.Stderr), `"code":"runtime"`) {
				t.Fatalf("exit %d, stderr %s", res.ExitCode, res.Stderr)
			}
		})
	}
}

// production is the layout of the launch's kind, with --state and --desired applied.
func TestProduction(t *testing.T) {
	env := map[string]string{envHome: "/home/u", envXDG: "/run/user/1"}
	e := cli.Env{Getenv: func(k string) string { return env[k] }, Digest: digest}
	launch := func(l cli.Launch) cli.Invocation { return cli.Invocation{Launch: &l} }
	cfg, err := production(e, launch(cli.Launch{Kind: cli.KindHost, State: "/srv/state", Desired: "/srv/desired", Cmd: []string{"sleep"}}))
	mustDo(t, err)
	if cfg.RuntimeParent != "/run/user/1" || cfg.Layout.State() != "/srv/state" || cfg.Layout.Desired() != "/srv/desired" ||
		cfg.Digest != digest || cfg.Cwd == "" {
		t.Fatalf("config %+v", cfg)
	}
	run, kit := cfg.Layout.Run(mustRunID(t)), w0Kit(t)
	if c := cfg.LinkCmd(run, kit); !hasArgs(c, kit.Binary(), "link", "--run", run.Dir()) {
		t.Fatalf("link %v", c.Line())
	}
	ccy, err := production(e, launch(cli.Launch{Kind: cli.KindCCY, Cmd: []string{"sleep"}}))
	mustDo(t, err)
	if ccy.RuntimeParent != "" || ccy.Layout.RunRoot() != paths.CCYLayout().RunRoot() {
		t.Fatalf("ccy config %+v", ccy)
	}
	refused := map[string]struct {
		inv  cli.Invocation
		home string
		code string
	}{
		"no launch":        {cli.Invocation{}, "/home/u", exits.CodeInternal},
		"relative home":    {launch(cli.Launch{Kind: cli.KindHost}), "home", exits.CodeRuntime},
		"relative state":   {launch(cli.Launch{Kind: cli.KindHost, State: "state"}), "/home/u", exits.CodeRuntime},
		"relative desired": {launch(cli.Launch{Kind: cli.KindHost, Desired: "desired"}), "/home/u", exits.CodeMembership},
	}
	for name, tc := range refused {
		t.Run(name, func(t *testing.T) {
			env[envHome] = tc.home
			_, err := production(e, tc.inv)
			if xe, ok := errors.AsType[*exits.Error](err); !ok || xe.Code != tc.code {
				t.Fatalf("error %v, want %s", err, tc.code)
			}
		})
	}
	env[envXDG] = ""
	if _, err := production(e, launch(cli.Launch{Kind: cli.KindHost})); err == nil {
		t.Fatal("a host launch with no runtime directory")
	}
}

func mustRunID(t *testing.T) paths.RunID {
	t.Helper()
	id, err := paths.ParseRunID("0123456789abcdef")
	mustDo(t, err)
	return id
}

func w0Kit(t *testing.T) paths.Kit {
	t.Helper()
	kit, err := paths.KitAt("/kits/" + digest)
	mustDo(t, err)
	return kit
}

// resumeLaunch is a --claude launch that asks to resume the recorded conversation.
var resumeLaunch = []string{"--claude", "--kind", "host", "--resume-recorded", "--", "claude"}

const sessionID = "01234567-89ab-cdef-0123-456789abcdef"

// recorded writes the conversation record and, under configDir, its transcript.
func (w *world) recorded(configDir string) {
	w.t.Helper()
	rec := `{"v":1,"run":"0123456789abcdef","session_id":"` + sessionID + `","source":"resume","written_ms":1}`
	mustDo(w.t, os.WriteFile(w.layout.ConversationFile(), []byte(rec), 0o600))
	if configDir == "" {
		return
	}
	project := configDir + "/projects/-work-project-a"
	mustDo(w.t, os.MkdirAll(project, 0o700))
	mustDo(w.t, os.WriteFile(project+"/"+sessionID+".jsonl", nil, 0o600))
}

// B 2.4.1 step 5: --resume <id> is added when the record names a transcript in the project
// directory; else nothing, and keeper.log says why.
func TestResume(t *testing.T) {
	cases := map[string]struct {
		setup  func(w *world)
		resume bool
		why    string
	}{
		"home":           {func(w *world) { w.recorded(w.env[envHome] + "/.claude") }, true, ""},
		"config dir":     {func(w *world) { w.env[envConfig] = w.dir + "/cfg"; w.recorded(w.dir + "/cfg") }, true, ""},
		"no record":      {func(*world) {}, false, "no resume: the conversation record"},
		"no transcript":  {func(w *world) { w.recorded("") }, false, "is not a regular file"},
		"no state root":  {func(w *world) { mustDo(w.t, os.RemoveAll(w.layout.State())) }, false, "no resume: the state root"},
		"relative confg": {func(w *world) { w.env[envConfig] = "cfg"; w.recorded(w.env[envHome] + "/.claude") }, true, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, resumeLaunch...)
			tc.setup(w)
			res := w.start()
			l := w.link()
			m := l.expect(t, wire.KindLaunch)
			if (m.Launch.ClaudeConfigDir != nil) != (w.env[envConfig] == w.dir+"/cfg") {
				t.Fatalf("launch %+v", m.Launch)
			}
			w.ready(l)
			c := w.cmd()
			l.expect(t, wire.KindPID)
			if got := len(c.cmd.Args) > 1 && c.cmd.Args[0] == "--resume" && c.cmd.Args[1] == sessionID; got != tc.resume {
				t.Fatalf("CMD argv %q", c.cmd.Line())
			}
			if tc.why != "" && !contains(w.log(), tc.why) {
				t.Fatalf("keeper.log %q", w.log())
			}
			w.sessionStarted(l)
			c.code(0)
			ended(t, l, res, wire.EndExit, 0, 0)
		})
	}
}

// The owner rule: a seat that cannot be taken for a reason but another session's hold is
// logged and the launch goes on without it.
func TestSeatThatCannotBeTaken(t *testing.T) {
	t.Run("no seat directory", func(t *testing.T) {
		w := newWorld(t, claudeLaunch...)
		w.writeDesired(desiredJSON(1, state.InboundAccept, "alpha/1"))
		l, c, res := w.running(0)
		if !contains(w.log(), "seat alpha not held") {
			t.Fatalf("keeper.log %q", w.log())
		}
		c.code(0)
		ended(t, l, res, wire.EndExit, 0, 0)
	})
	t.Run("no state root", func(t *testing.T) {
		w := newWorld(t, claudeLaunch...)
		w.writeDesired(desiredJSON(1, state.InboundAccept, "alpha/1"))
		mustDo(t, os.RemoveAll(w.layout.State()))
		l, c, res := w.running(0)
		if !contains(w.log(), "the state root") {
			t.Fatalf("keeper.log %q", w.log())
		}
		c.code(0)
		ended(t, l, res, wire.EndExit, 0, 0)
	})
}
