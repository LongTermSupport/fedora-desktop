package teamchatlaunch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// fakeForeground is the session cc-run starts as its child: the command it was given, the
// extra settings file as it stood then, and the exit it answers.
type fakeForeground struct {
	cmds  []execx.Cmd
	extra []string
	exit  execx.Exit
}

func (f *fakeForeground) Foreground(c execx.Cmd) (execx.Exit, error) {
	f.cmds = append(f.cmds, c)
	if i := slices.Index(c.Args, "--extra-settings"); i >= 0 {
		data, err := os.ReadFile(c.Args[i+1])
		f.extra = append(f.extra, string(data)+"|"+err2s(err))
	}
	return f.exit, nil
}

func err2s(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}

const denyJSON = `{"permissions":{"deny":["Read(~/.config/Element/**)"]}}`

// ccWorld is a cc-run world: verbsWorld with the files a host cc launch reads, in a scratch
// tree, and the session's private directory.
type ccWorld struct {
	*verbsWorld
	fg  *fakeForeground
	tmp string
	cc  CC
}

func newCC(t *testing.T) *ccWorld {
	t.Helper()
	w := &ccWorld{verbsWorld: newVerbs(t), fg: &fakeForeground{}, tmp: t.TempDir()}
	dir := t.TempDir()
	w.cc = CC{
		V1Kit: filepath.Join(dir, "agent-bus/kit"), DenySettings: filepath.Join(dir, "cc-teams-settings.json"),
		CCClaude: "/var/local/claude-code/cc-claude", TeamchatKit: filepath.Join(dir, "teamchat/kit/current"),
	}
	for name, body := range map[string]string{
		w.cc.V1Kit + "/pingbus": "", w.cc.V1Kit + "/pingbus-status-line": "", w.cc.V1Kit + "/settings.json": `{"a":1}`,
		w.cc.V1Kit + "/plugin/pingbus/.claude-plugin/plugin.json": "{}", w.cc.DenySettings: denyJSON,
		w.cc.TeamchatKit + "/settings.json": "{}", w.cc.TeamchatKit + "/plugin/teamchat/.claude-plugin/plugin.json": "{}",
	} {
		writeTestFile(t, name, body)
	}
	w.verbs.CC, w.verbs.Foreground = w.cc, w.fg
	return w
}

func writeTestFile(t *testing.T, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (w *ccWorld) ccRun(session, seats, bus string, claudeArgs ...string) term.ExitCode {
	return w.run(append([]string{"cc-run", w.tmp, w.checkout, session, "4242", seats, bus, "--"}, claudeArgs...)...)
}

// (b) H1's line: the seats taken through Launch.Run with no mounts, desired.json written, then
// teamchat run as a child with cc's deny rules as extra settings and the teamchat prompt line.
func TestCCRunOnTeamchatRunsTheSessionAsAChild(t *testing.T) {
	w := newCC(t)
	w.pool("alpha", "repo.1+site.host")
	if code := w.ccRun("cc-s1", "1@alpha", "false", "--model", "opus"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err)
	}
	session := mustSession(t, "cc-s1")
	if len(w.taker.took) != 1 || w.taker.took[0].Session != session || w.initArgs != nil {
		t.Fatalf("took %+v, init %q", w.taker.took, w.initArgs)
	}
	if _, err := os.Stat(w.store.DesiredFile(session)); err != nil {
		t.Fatalf("desired.json: %v", err)
	}
	extra := w.tmp + "/cc-deny.json"
	want := []string{
		"run", "--claude", "--kind", "host", "--state", w.store.Session(session), "--desired", w.store.Control(session),
		"--extra-settings", extra, "--", w.cc.CCClaude, "--", "claude", "--append-system-prompt", TeamchatPrompt, "--model", "opus",
	}
	if len(w.fg.cmds) != 1 || w.fg.cmds[0].Name != "teamchat" || !slices.Equal(w.fg.cmds[0].Args, want) {
		t.Fatalf("started %+v\nwant teamchat %q", w.fg.cmds, want)
	}
	if !slices.Equal(w.fg.cmds[0].Env, []string{"PINGBUS_MEMBERSHIP=off"}) {
		t.Errorf("env %q", w.fg.cmds[0].Env)
	}
	if !slices.Equal(w.fg.extra, []string{denyJSON + "|"}) {
		t.Errorf("extra settings %q", w.fg.extra)
	}
	// The session ended with a status: its record is removed, then its seats are released.
	if !slices.Equal(w.forgot, []string{"cc-s1"}) || len(w.exec.Execs()) != 0 {
		t.Errorf("forgot %q, execs %v", w.forgot, w.exec.Execs())
	}
	if !strings.Contains(TeamchatPrompt, "teamchat") || strings.Contains(TeamchatPrompt, "pingbus") {
		t.Errorf("prompt %q", TeamchatPrompt)
	}
}

// (b) A status ends the session whatever it is; the tool's own code is the documented one.
func TestCCRunOnTeamchatEndsOnAnyStatus(t *testing.T) {
	w := newCC(t)
	w.pool("alpha", "repo.1+site.host")
	w.holder.holdings = []seat.Holding{holding(t, "cc-s1", "repo.1+site.host")}
	w.fg.exit = execx.Exit{Code: 3}
	if code := w.ccRun("cc-s1", "1@alpha", "false"); code != term.ExitFailure {
		t.Fatalf("exit %d: %s", code, w.err)
	}
	if !slices.Equal(w.holder.calls, []string{"forget cc-s1", "release repo.1+site.host cc-s1"}) || !w.holder.closed {
		t.Errorf("calls %q, closed %v", w.holder.calls, w.holder.closed)
	}
}

// (b) SIGHUP or SIGTERM reached cc-run (a killed pane, a reboot): forwarded to the child by
// Foreground, and then the seats are announced and kept, the record left for a restore.
func TestCCRunOnTeamchatKeepsTheSeatsOnASignal(t *testing.T) {
	for _, sig := range []os.Signal{syscall.SIGHUP, syscall.SIGTERM} {
		w := newCC(t)
		w.pool("alpha", "repo.1+site.host")
		w.holder.holdings = []seat.Holding{holding(t, "cc-s1", "repo.1+site.host")}
		w.fg.exit = execx.Exit{Code: 129, Received: []os.Signal{sig}}
		w.ccRun("cc-s1", "1@alpha", "false")
		if !slices.Equal(w.holder.calls, []string{"announce repo.1+site.host cc-s1"}) || len(w.forgot) != 0 {
			t.Errorf("%v: calls %q, forgot %q", sig, w.holder.calls, w.forgot)
		}
	}
}

// (b, H2) A teamchat launch with no registry session name (cc outside ccy's tmux) is refused
// before anything is taken or started.
func TestCCRunOnTeamchatNeedsASessionName(t *testing.T) {
	w := newCC(t)
	w.pool("alpha", "repo.1+site.host")
	if code := w.ccRun("", "1@alpha", "false"); code != term.ExitConfig {
		t.Fatalf("exit %d: %s", code, w.err)
	}
	if w.taker.took != nil || len(w.fg.cmds) != 0 || !strings.Contains(w.err.String(), "tmux") {
		t.Errorf("took %+v, started %v, said %q", w.taker.took, w.fg.cmds, w.err)
	}
}

// The kit check, per system: a missing file is 78, naming the play, before any seat is taken.
func TestCCRunRefusesAMissingKit(t *testing.T) {
	for name, c := range map[string]struct{ seats, bus, missing, play string }{
		"teamchat settings": {"1@alpha", "false", "teamchat/kit/current/settings.json", "play-teamchat.yml"},
		"teamchat plugin":   {"1@alpha", "false", "teamchat/kit/current/plugin/teamchat/.claude-plugin/plugin.json", "play-teamchat.yml"},
		"teamchat deny":     {"1@alpha", "false", "cc-teams-settings.json", "play-claude-yolo.yml"},
		"v1 pingbus":        {"1@v1team", "false", "agent-bus/kit/pingbus", "play-agent-bus.yml"},
		"v1 deny, --bus":    {"", "true", "cc-teams-settings.json", "play-claude-yolo.yml"},
	} {
		t.Run(name, func(t *testing.T) {
			w := newCC(t)
			w.pool("alpha", "repo.1+site.host")
			if err := os.Remove(filepath.Join(filepath.Dir(filepath.Dir(w.cc.V1Kit)), c.missing)); err != nil {
				t.Fatal(err)
			}
			if code := w.ccRun("cc-s1", c.seats, c.bus); code != term.ExitConfig {
				t.Fatalf("exit %d: %s", code, w.err)
			}
			if w.taker.took != nil || w.initArgs != nil || len(w.fg.cmds) != 0 || len(w.exec.Execs()) != 0 || !strings.Contains(w.err.String(), c.play) {
				t.Errorf("took %+v, init %q, said %q", w.taker.took, w.initArgs, w.err)
			}
		})
	}
}

// (c) A list on both systems is system.One's refusal, 64.
func TestCCRunRefusesAMixedList(t *testing.T) {
	w := newCC(t)
	w.pool("alpha", "repo.1+site.host")
	if code := w.ccRun("cc-s1", "1@alpha,1@v1team", "false"); code != term.ExitUsage {
		t.Fatalf("exit %d: %s", code, w.err)
	}
	if w.taker.took != nil || len(w.fg.cmds) != 0 || len(w.exec.Execs()) != 0 {
		t.Errorf("took %+v, started %v", w.taker.took, w.fg.cmds)
	}
}

// (d) A relaunch of the same session (a restore) hands the store the same session's takes, so
// the store keeps the seats it already holds (L5, TestStoreTakerKeepsTheSeatsTheSessionHolds).
func TestCCRunRelaunchTakesForTheSameSession(t *testing.T) {
	w := newCC(t)
	w.pool("alpha", "repo.1+site.host")
	w.fg.exit = execx.Exit{Received: []os.Signal{syscall.SIGHUP}}
	var sessions []paths.SessionName
	for range 2 {
		if code := w.ccRun("cc-s1", "1@alpha", "false"); code != term.ExitOK {
			t.Fatalf("exit %d: %s", code, w.err)
		}
		sessions = append(sessions, w.taker.took[0].Session)
	}
	if sessions[0] != sessions[1] || len(w.fg.cmds) != 2 {
		t.Fatalf("sessions %v, started %d", sessions, len(w.fg.cmds))
	}
}

func TestCCRunUsage(t *testing.T) {
	w := newCC(t)
	for _, args := range [][]string{
		{"cc-run"},
		{"cc-run", w.tmp, w.checkout, "s", "1", "1@alpha", "false"},
		{"cc-run", w.tmp, w.checkout, "s", "1", "1@alpha", "maybe", "--"},
		{"cc-run", w.tmp, w.checkout, "s", "x", "1@alpha", "false", "--"},
		{"cc-run", "rel", w.checkout, "s", "1", "1@alpha", "false", "--"},
	} {
		if code := w.run(args...); code != term.ExitUsage {
			t.Errorf("%q: exit %d", args, code)
		}
	}
}
