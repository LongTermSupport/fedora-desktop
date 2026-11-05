package keeper

// AS5 (SPEC-n3 6.1): what a claude launch and a plain launch hand CMD and leave on disk, pinned
// byte for byte, so that the agent seam is seen to move nothing.

import (
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
)

// logTap opens keeper.log as soon as the run under its staging name has one, so the log is
// read whole after the keeper has ended, even when the end removed the run. With plant, it
// also puts a directory where the run's settings file goes, so that the write fails.
type logTap struct {
	Proc

	w     *world
	plant bool
	f     *os.File
}

func (p *logTap) StartTicks(pid int) (int64, error) {
	if pid == os.Getpid() && p.f == nil {
		stage := p.w.layout.Staging(os.Getpid(), 7)
		if f, err := os.Open(stage.KeeperLog()); err == nil {
			p.f = f
			if p.plant {
				mustDo(p.w.t, os.Mkdir(stage.Settings(), 0o700))
			}
		}
	}
	return p.Proc.StartTicks(pid)
}

// read is keeper.log whole; the keeper must have ended.
func (p *logTap) read(t *testing.T) string {
	t.Helper()
	if p.f == nil {
		t.Fatal("keeper.log was never tapped")
	}
	data, err := io.ReadAll(p.f)
	mustDo(t, err)
	mustDo(t, p.f.Close())
	return string(data)
}

// tapped is w's config with keeper.log tapped.
func (w *world) tapped(plant bool) (Config, *logTap) {
	cfg := w.config()
	tap := &logTap{Proc: cfg.Proc, w: w, plant: plant}
	cfg.Proc = tap
	return cfg, tap
}

// norm writes the test's own directories as placeholders: <run> for the session's runtime
// directory, <dir> for the test's directory.
func (w *world) norm(s string) string {
	run := regexp.MustCompile(regexp.QuoteMeta(w.layout.RunRoot()) + `/[0-9a-f]{16}`)
	return strings.ReplaceAll(run.ReplaceAllString(s, "<run>"), w.dir, "<dir>")
}

func (w *world) normAll(list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = w.norm(s)
	}
	return out
}

// goldenRun is what a launch that ran handed CMD and wrote.
type goldenRun struct {
	launch   string
	argv     []string
	env      []string
	settings string
	log      string
}

// runGolden runs w's launch to CMD's start and its end by CMD's exit 0.
func (w *world) runGolden(claude bool) goldenRun {
	t := w.t
	t.Helper()
	cfg, tap := w.tapped(false)
	res := w.startWith(cfg)
	l := w.link()
	m := l.expect(t, wire.KindLaunch)
	w.ready(l)
	c := w.cmd()
	l.expect(t, wire.KindPID)
	pid := w.sessionPID()
	if pid.Launch != m.Launch.Launch {
		t.Fatalf("session.pid's launch %q, the frame's %q", pid.Launch, m.Launch.Launch)
	}
	got := goldenRun{
		launch: m.Launch.Launch,
		argv:   w.normAll(append([]string{c.cmd.Name}, c.cmd.Args...)),
		env:    w.normAll(c.cmd.Env),
	}
	data, err := os.ReadFile(w.runDir().Settings())
	switch {
	case err == nil:
		got.settings = string(data)
	case !os.IsNotExist(err):
		t.Fatal(err)
	}
	if claude {
		w.sessionStarted(l)
	}
	c.code(0)
	ended(t, l, res, wire.EndExit, 0, 0)
	got.log = w.norm(tap.read(t))
	return got
}

// refusedGolden runs w's launch to its refusal: 78, stderr's error object and keeper.log.
func (w *world) refusedGolden(plant bool) (string, string) {
	t := w.t
	t.Helper()
	cfg, tap := w.tapped(plant)
	exitOf(t, w.startWith(cfg), exits.ExitConfig)
	if len(w.starter.Starts()) != 0 {
		t.Fatal("a link or CMD was started")
	}
	return w.norm(w.stderr.String()), w.norm(tap.read(t))
}

func checkLaunched(t *testing.T, got, want goldenRun) {
	t.Helper()
	if got.launch != want.launch {
		t.Errorf("launch %q, want %q", got.launch, want.launch)
	}
	if !slices.Equal(got.argv, want.argv) {
		t.Errorf("argv %q\nwant %q", got.argv, want.argv)
	}
	if !slices.Equal(got.env, want.env) {
		t.Errorf("env %q\nwant %q", got.env, want.env)
	}
	if got.settings != want.settings {
		t.Errorf("settings %q\nwant %q", got.settings, want.settings)
	}
	if got.log != want.log {
		t.Errorf("keeper.log %q\nwant %q", got.log, want.log)
	}
}

// The environment every launch gives CMD.
var goldenEnv = []string{
	"TEAMCHAT_RUNTIME=<run>",
	"PATH=<dir>/kits/" + digest + "/bin:/usr/bin",
	"TEAMCHAT_KIT=<dir>/kits/" + digest,
}

const (
	goldenPlugin      = "<dir>/kits/" + digest + "/plugin/teamchat"
	goldenKitSettings = "<dir>/kits/" + digest + "/settings.json"
)

// AS5: a claude launch, fresh, resumed and with each reason a resume is not made that a launch
// can meet. The record that names no session id is not one: DecodeConversation refuses it first,
// so it reads as the conversation record's error.
func TestGoldenClaudeLaunch(t *testing.T) {
	ranLog := "link 4001 started\nthe link is ready\nCMD 4002 started\nClaude Code's first SessionStart\n" +
		"CMD exited 0\nthe session ended: exit, status 0\nthe link exited 0\n"
	cases := map[string]struct {
		args  []string
		setup func(w *world)
		want  goldenRun
	}{
		"fresh": {
			args:  []string{"--claude", "--kind", "host", "--desired", "DESIRED", "--", "claude", "--verbose"},
			setup: func(w *world) { w.writeDesired(desiredJSON(1, state.InboundAccept)) },
			want: goldenRun{
				launch:   state.LaunchClaude,
				argv:     []string{"claude", "--verbose", "--plugin-dir", goldenPlugin, "--settings", "<run>/settings.json"},
				settings: `{"crossSessionInbound":"accept","statusLine":{"type":"command"}}`,
				log:      ranLog,
			},
		},
		"resumed": {
			args:  resumeLaunch,
			setup: func(w *world) { w.recorded(w.env[envHome] + "/.claude") },
			want: goldenRun{
				launch:   state.LaunchClaude,
				argv:     []string{"claude", "--resume", sessionID, "--plugin-dir", goldenPlugin, "--settings", "<run>/settings.json"},
				settings: `{"crossSessionInbound":"refuse","statusLine":{"type":"command"}}`,
				log:      ranLog,
			},
		},
		"no state root": {
			args:  resumeLaunch,
			setup: func(w *world) { mustDo(w.t, os.RemoveAll(w.layout.State())) },
			want: goldenRun{
				launch:   state.LaunchClaude,
				argv:     []string{"claude", "--plugin-dir", goldenPlugin, "--settings", "<run>/settings.json"},
				settings: `{"crossSessionInbound":"refuse","statusLine":{"type":"command"}}`,
				log:      "no resume: the state root: homefs: <dir>/state: no such file or directory\n" + ranLog,
			},
		},
		"no conversation record": {
			args:  resumeLaunch,
			setup: func(*world) {},
			want: goldenRun{
				launch:   state.LaunchClaude,
				argv:     []string{"claude", "--plugin-dir", goldenPlugin, "--settings", "<run>/settings.json"},
				settings: `{"crossSessionInbound":"refuse","statusLine":{"type":"command"}}`,
				log:      "no resume: the conversation record: homefs: <dir>/state/conversation.json: no such file or directory\n" + ranLog,
			},
		},
		"no transcript": {
			args:  resumeLaunch,
			setup: func(w *world) { w.recorded("") },
			want: goldenRun{
				launch:   state.LaunchClaude,
				argv:     []string{"claude", "--plugin-dir", goldenPlugin, "--settings", "<run>/settings.json"},
				settings: `{"crossSessionInbound":"refuse","statusLine":{"type":"command"}}`,
				log: "no resume: the transcript <dir>/home/.claude/projects/-work-project-a/" + sessionID +
					".jsonl is not a regular file (no such file or directory)\n" + ranLog,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, tc.args...)
			tc.setup(w)
			tc.want.env = goldenEnv
			checkLaunched(t, w.runGolden(true), tc.want)
		})
	}
}

// AS5: a claude launch refused at its settings: the kit's that cannot be used, and a write that
// fails.
func TestGoldenClaudeRefused(t *testing.T) {
	cases := map[string]struct {
		plant       bool
		settings    string
		stderr, log string
	}{
		"kit settings": {
			settings: `{"crossSessionInbound":"accept"}`,
			stderr: `{"kind":"error","v":1,"exit":78,"code":"runtime","team":null,` +
				`"message":"the kit's settings ` + goldenKitSettings + ` are not a JSON object without the inbound key"}` + "\n",
			log: "read " + goldenKitSettings + ": <nil>\n" +
				"launch refused: the kit's settings " + goldenKitSettings + " are not a JSON object without the inbound key\n",
		},
		"settings write": {
			plant: true,
			stderr: `{"kind":"error","v":1,"exit":78,"code":"runtime","team":null,` +
				`"message":"the session's runtime directory <run> cannot be made"}` + "\n",
			log: "write <run>/settings.json: homefs: <run>/settings.json: file exists\n" +
				"launch refused: the session's runtime directory <run> cannot be made\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, claudeLaunch...)
			w.writeDesired(desiredJSON(1, state.InboundAccept))
			if tc.settings != "" {
				mustDo(t, os.WriteFile(w.kit.Settings(), []byte(tc.settings), 0o644))
			}
			stderr, log := w.refusedGolden(tc.plant)
			if stderr != tc.stderr {
				t.Errorf("stderr %q\nwant %q", stderr, tc.stderr)
			}
			if log != tc.log {
				t.Errorf("keeper.log %q\nwant %q", log, tc.log)
			}
		})
	}
}

// AS5: a plain launch hands CMD its own argv and writes no settings, with or without a desired
// membership.
func TestGoldenPlainLaunch(t *testing.T) {
	ranLog := "link 4001 started\nthe link is ready\nCMD 4002 started\nCMD exited 0\nthe session ended: exit, status 0\nthe link exited 0\n"
	cases := map[string]struct {
		args  []string
		setup func(w *world)
	}{
		"no desired": {[]string{"--plain", "--kind", "host", "--", "sleep", "60"}, func(*world) {}},
		"desired": {
			[]string{"--plain", "--kind", "host", "--desired", "DESIRED", "--", "sleep", "60"},
			func(w *world) { w.writeDesired(desiredJSON(1, state.InboundAccept)) },
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, tc.args...)
			tc.setup(w)
			checkLaunched(t, w.runGolden(false), goldenRun{
				launch: state.LaunchPlain, argv: []string{"sleep", "60"}, env: goldenEnv, log: ranLog,
			})
		})
	}
}
