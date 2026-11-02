package kit_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/client"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/hook"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/hooktext"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/kit"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/statusline"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/reexec"
)

// The kit's binary in these tests is this test binary: run as the helper below, it is the
// member binary with the two verbs the kit names wired in, as cmd/teamchat wires them.
const (
	binaryHelper = "teamchat"
	testDigest   = "0123456789abcdef0123456789abcdef"
	testRunID    = "00112233445566ff"
)

func TestMain(m *testing.M) {
	os.Exit(reexec.Main(m, reexec.Helpers{binaryHelper: runBinary}))
}

func runBinary(*reexec.Helper) int {
	return int(cli.Run(cli.Env{
		Args: os.Args[1:], Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		Digest: testDigest, Getenv: os.Getenv,
		Verbs: map[string]cli.RunFunc{"hook": hook.Main, "status-line": statusline.Main},
	}))
}

// usedKit is a kit assembled at a temporary path with this test binary as bin/teamchat, and
// the member's home and runtime directory beside it.
type usedKit struct {
	t     *testing.T
	kit   paths.Kit
	home  string
	xdg   string
	run   paths.Run
	extra []string // the helper's environment, from reexec
}

func newUsedKit(t *testing.T) *usedKit {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "kit")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	k, err := paths.KitAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	if err = errors.Join(kit.Assemble(dir, k, bin), bin.Close()); err != nil {
		t.Fatal(err)
	}
	home, xdg := filepath.Join(base, "h"), filepath.Join(base, "x")
	if err = errors.Join(os.Mkdir(home, 0o700), os.Mkdir(xdg, 0o700)); err != nil {
		t.Fatal(err)
	}
	layout, err := paths.HomeLayout(home, xdg)
	if err != nil {
		t.Fatal(err)
	}
	id, err := paths.ParseRunID(testRunID)
	if err != nil {
		t.Fatal(err)
	}
	return &usedKit{t: t, kit: k, home: home, xdg: xdg, run: layout.Run(id), extra: reexec.Command(t, binaryHelper).Env}
}

// makeRun makes the session's runtime directory, with no keeper and no link in it.
func (u *usedKit) makeRun() {
	u.t.Helper()
	if err := os.MkdirAll(u.run.Dir(), 0o700); err != nil {
		u.t.Fatal(err)
	}
}

// command is the command a kit file names under key in its JSON, read from the file.
func (u *usedKit) command(file string, keys ...string) string {
	u.t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		u.t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		u.t.Fatal(err)
	}
	for _, k := range keys {
		switch node := v.(type) {
		case map[string]any:
			v = node[k]
		case []any:
			if len(node) != 1 || k != "0" {
				u.t.Fatalf("%s: %v at %s", file, node, k)
			}
			v = node[0]
		}
	}
	s, ok := v.(string)
	if !ok {
		u.t.Fatalf("%s %v: %v", file, keys, v)
	}
	return s
}

// shell runs a command as Claude Code runs a hook's or the status line's: a shell's -c string,
// stdin the payload, in an environment of the given variables and the helper's alone.
func (u *usedKit) shell(command, stdin string, withRuntime bool) execx.Result {
	u.t.Helper()
	// A -cover test binary warns on stderr when it has nowhere to write its counters.
	cover := os.Getenv("GOCOVERDIR")
	if cover == "" {
		cover = u.t.TempDir()
	}
	env := append([]string{
		"PATH=/usr/bin:/bin", "HOME=" + u.home, "XDG_RUNTIME_DIR=" + u.xdg, "TEAMCHAT_STATUS_LINE_INNER=1", "GOCOVERDIR=" + cover,
	}, u.extra...)
	if withRuntime {
		env = append(env, paths.EnvRuntime+"="+u.run.Dir())
	}
	res, err := execx.OS{}.Run(u.t.Context(), execx.Cmd{
		Name: "/bin/sh", Args: []string{"-c", command}, Env: env, CleanEnv: true, Stdin: strings.NewReader(stdin),
	})
	if err != nil {
		u.t.Fatalf("%s: %v", command, err)
	}
	if res.ExitCode != 0 || len(res.Stderr) != 0 {
		u.t.Fatalf("%s: exit %d, stderr %q", command, res.ExitCode, res.Stderr)
	}
	return res
}

// PK1: each hooks.json command, run as written, is the hook: {} with no TEAMCHAT_RUNTIME, and
// with a run directory and no link the recovery on SessionStart and UserPromptSubmit and {} on
// Stop and SessionEnd; every exit 0.
func TestEveryHookCommandRunsAsWritten(t *testing.T) {
	u := newUsedKit(t)
	u.makeRun()
	recovery := hooktext.Recovery(client.ReadDown(u.run).Error(nil, clock.System().Boot()).Message)
	if strings.Contains(recovery, "next try") {
		t.Fatalf("a run with no keeper read as restarting: %s", recovery)
	}
	for _, h := range kit.Hooks {
		command := u.command(u.kit.PluginHooks(), "hooks", h.Event, "0", "hooks", "0", "command")
		if command != u.kit.Binary()+" hook "+h.Arg {
			t.Fatalf("%s: %q", h.Event, command)
		}
		payload := `{"session_id":"s1","hook_event_name":"` + h.Event + `"}`
		if got := string(u.shell(command, payload, false).Stdout); got != "{}\n" {
			t.Errorf("%s with no runtime: %q", h.Event, got)
		}
		want := string(hooktext.Context(h.Event, recovery).Line) + "\n"
		if h.Event == "Stop" || h.Event == "SessionEnd" {
			want = "{}\n"
		}
		if got := string(u.shell(command, payload, true).Stdout); got != want {
			t.Errorf("%s with no link: %q, want %q", h.Event, got, want)
		}
	}
}

// OF1 through the kit: with a link that answers one team stopped beside a running one, the
// prompt hook prints that answer whole and exits 0.
func TestOF1HookFromKit(t *testing.T) {
	u := newUsedKit(t)
	u.makeRun()
	alpha, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	// alpha has stopped; the pending counts are the running team's.
	text := strings.Join([]string{
		hooktext.TeamFailed(alpha, state.FailedForbidden), hooktext.Pending(hooktext.Counts{Total: 2, Humans: 1, Agents: 1}),
	}, "\n")
	answer := string(hooktext.Context("UserPromptSubmit", text).Line)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: u.run.LinkSock(), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- serveOnce(ln, answer) }()
	command := u.command(u.kit.PluginHooks(), "hooks", "UserPromptSubmit", "0", "hooks", "0", "command")
	if got := string(u.shell(command, `{"prompt":"go on"}`, true).Stdout); got != answer+"\n" {
		t.Errorf("%q, want %q", got, answer)
	}
	if err = errors.Join(<-served, ln.Close()); err != nil {
		t.Fatal(err)
	}
}

// serveOnce is a link's answer to one hook request: the answer line, then a done of exit 0.
func serveOnce(ln *net.UnixListener, answer string) error {
	c, err := ln.AcceptUnix()
	if err != nil {
		return err
	}
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err == nil {
		_, err = c.Write([]byte(answer + "\n" + `{"done":{"exit":0}}` + "\n"))
	}
	if err == nil && !bytes.Contains(line, []byte(`"`+wire.OpHookPrompt+`"`)) {
		err = errors.New("not a prompt hook request: " + string(line[:min(len(line), 80)]))
	}
	return errors.Join(err, c.Close())
}

// PK1: the settings' status-line command runs SL1 and exits 0: nothing with no session, and the
// red stopped form with a run directory and no link; refreshInterval is 25, below 30.
func TestTheStatusLineCommandRunsAsWritten(t *testing.T) {
	u := newUsedKit(t)
	command := u.command(u.kit.Settings(), "statusLine", "command")
	if command != u.kit.Binary()+" status-line" {
		t.Fatalf("status line %q", command)
	}
	payload := `{"session_id":"s1","workspace":{"project_dir":"` + u.home + `"}}`
	if got := u.shell(command, payload, false).Stdout; len(got) != 0 {
		t.Errorf("no session: %q", got)
	}
	u.makeRun()
	got := u.shell(command, payload, true).Stdout
	if !bytes.Contains(got, []byte("teamchat stopped: relaunch this session")) || !bytes.HasPrefix(got, []byte("\x1b[31m")) {
		t.Errorf("no link: %q", got)
	}
	data, err := os.ReadFile(u.kit.Settings())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		StatusLine struct {
			RefreshInterval int `json:"refreshInterval"`
		} `json:"statusLine"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.StatusLine.RefreshInterval != kit.StatusLineRefresh || kit.StatusLineRefresh != 25 || doc.StatusLine.RefreshInterval >= 30 {
		t.Errorf("refreshInterval %d", doc.StatusLine.RefreshInterval)
	}
}
