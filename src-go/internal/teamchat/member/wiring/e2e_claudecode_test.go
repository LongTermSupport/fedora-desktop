package wiring

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/fakeinbox"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/faketranscript"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/hook"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/testfiles"
)

// The fake Claude Code: the test binary run as `claude`, a --claude session's CMD (SPEC-n3 CL5).
// It does what Claude Code does that the member binary sees: it is given --plugin-dir and
// --settings, makes the session's inbox socket and transcript, and runs the plugin's hook
// commands and the settings' status line command through a shell, as Claude Code does, with the
// payload on stdin. A test drives it over the keeper's stdin, the one stream CMD is given: each
// line is `<n> <command> [arguments]`, and the answer is one line on the shared stdout, `cc-reply
// <n> <json>` or `cc-error <n> <text>`.
//
// It is not Claude Code: it does not model a turn, a prompt or a tool, and it takes a frame the
// inbox accepted as a user turn at once (faketranscript.Idle).

// claudeName is the name the test binary is Claude Code by: a --claude run's CMD has an argument
// of this base name.
const claudeName = "claude"

// ccSessionID is the fake session's id, a lowercase UUID.
const ccSessionID = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"

// The replies, shared with the test side by their JSON.

// ccBegun is what begin made: the inbox socket, the transcript and the session.
type ccBegun struct {
	Socket     string `json:"socket"`
	Transcript string `json:"transcript"`
	Session    string `json:"session"`
}

// ccRun is a command the fake ran through a shell: the command line, its exit status and its
// output.
type ccRun struct {
	Command string `json:"command"`
	Exit    int    `json:"exit"`
	Stdout  string `json:"stdout"`
	Stderr  string `json:"stderr"`
}

// ccFrame is one line the fake inbox read, as fakeinbox.Frame is.
type ccFrame struct {
	Auth     bool   `json:"auth"`
	Content  string `json:"content"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason"`
}

// ccHooksFile, ccHookGroup and ccHookCommand are the plugin's hooks.json, the part Claude Code
// reads to run a hook.
type ccHooksFile struct {
	Hooks map[string][]ccHookGroup `json:"hooks"`
}

type ccHookGroup struct {
	Hooks []ccHookCommand `json:"hooks"`
}

type ccHookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// ccSettings is the part of the run's settings file Claude Code runs a status line from.
type ccSettings struct {
	StatusLine ccHookCommand `json:"statusLine"`
}

// claudeCode is the fake's one session.
type claudeCode struct {
	pluginDir, settings string
	home, runtime, cwd  string
	// auth is the inbox's token, which only the hooks' environment and the auth line carry.
	auth  string
	inbox *fakeinbox.Inbox
	tr    *faketranscript.Transcript
	// release ends the inbox's hold, nil when it is not held.
	release func()
}

// fakeClaude is the `claude` command: its pid and TEAMCHAT_RUNTIME on the first line, as the
// plain agent says them, then each command on stdin answered on stdout until stdin ends.
func fakeClaude(args []string, stdin io.Reader, stdout io.Writer) int {
	c, err := newClaudeCode(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 3
	}
	code := 0
	if _, err := fmt.Fprintf(stdout, "%s %d %s\n", claudeName, os.Getpid(), os.Getenv(paths.EnvRuntime)); err != nil {
		code = 3
	}
	scan := bufio.NewScanner(stdin)
	for code == 0 && scan.Scan() {
		if err := c.serve(stdout, scan.Text()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 4
		}
	}
	if err := c.close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 5
	}
	return code
}

func newClaudeCode(args []string) (*claudeCode, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	c := &claudeCode{home: os.Getenv("HOME"), runtime: os.Getenv("XDG_RUNTIME_DIR"), cwd: cwd, auth: rand.Text()}
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "--plugin-dir":
			c.pluginDir = args[i+1]
		case "--settings":
			c.settings = args[i+1]
		}
	}
	if c.pluginDir == "" || c.settings == "" || c.home == "" || c.runtime == "" {
		return nil, fmt.Errorf("claude: %q, HOME %q and XDG_RUNTIME_DIR %q must give --plugin-dir, --settings, a home and a runtime directory",
			args, c.home, c.runtime)
	}
	return c, nil
}

// serve answers one command line.
func (c *claudeCode) serve(stdout io.Writer, line string) error {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return fmt.Errorf("claude: a command line is a number and a command: %q", line)
	}
	if _, err := strconv.Atoi(fields[0]); err != nil {
		return fmt.Errorf("claude: a command line starts with its number: %q", line)
	}
	reply, err := c.handle(fields[1], fields[2:])
	text := "cc-error " + fields[0] + " " + strings.ReplaceAll(fmt.Sprint(err), "\n", " ")
	if err == nil {
		var data []byte
		if data, err = json.Marshal(reply); err == nil {
			text = "cc-reply " + fields[0] + " " + string(data)
		}
	}
	_, werr := io.WriteString(stdout, text+"\n")
	return werr
}

// handle runs one command: begin, hook <Event> [active], statusline, frames, transcript, hold,
// release or refuse.
func (c *claudeCode) handle(verb string, args []string) (any, error) {
	if c.inbox == nil && verb != "begin" {
		return nil, fmt.Errorf("claude: %s before begin", verb)
	}
	switch verb {
	case "begin":
		return c.begin()
	case "hook":
		if len(args) == 0 {
			return nil, errors.New("claude: hook takes an event")
		}
		return c.hook(args[0], len(args) > 1 && args[1] == "active")
	case "statusline":
		return c.statusLine()
	case "frames":
		return c.frames(), nil
	case "transcript":
		return c.transcript()
	case "hold", "release", "refuse":
		return true, c.socketFault(verb)
	}
	return nil, fmt.Errorf("claude: no command %q", verb)
}

// socketFault is the inbox's hold (it reads nothing until released), its release, or its refusal
// of every connect from now on (PT1's socket faults).
func (c *claudeCode) socketFault(verb string) error {
	switch {
	case verb == "hold" && c.release == nil:
		c.release = c.inbox.Hold()
	case verb == "release" && c.release != nil:
		c.release()
		c.release = nil
	case verb == "refuse":
		c.inbox.Refuse()
	default:
		return fmt.Errorf("claude: %s with the inbox's hold at %v", verb, c.release != nil)
	}
	return nil
}

// begin makes the session's inbox socket, at <runtime>/cc-socks/<pid>.sock, and its transcript,
// at the path Claude Code derives from the working directory; each frame the inbox takes is
// written to the transcript as a user turn.
func (c *claudeCode) begin() (ccBegun, error) {
	if c.inbox != nil {
		return ccBegun{}, errors.New("claude: begin twice")
	}
	sock, err := paths.InboxSocket(c.runtime, os.Getpid())
	if err != nil {
		return ccBegun{}, err
	}
	project := ".claude/projects/" + projectName(c.cwd)
	for _, d := range []struct{ base, rel string }{{c.runtime, "cc-socks"}, {c.home, project}} {
		if err = makeDirs(d.base, d.rel); err != nil {
			return ccBegun{}, err
		}
	}
	inbox, err := fakeinbox.Start(sock, fakeinbox.Options{Token: c.auth, Clock: clock.System()})
	if err != nil {
		return ccBegun{}, err
	}
	c.inbox = inbox
	c.tr, err = faketranscript.New(filepath.Join(c.home, project), ccSessionID, clock.System())
	if err != nil {
		return ccBegun{}, err
	}
	c.inbox.OnAccepted(func(f fakeinbox.Frame) { c.tr.Record(f.Content, faketranscript.Idle, 0) })
	return ccBegun{Socket: sock, Transcript: c.tr.Path(), Session: ccSessionID}, nil
}

// projectName is the directory name Claude Code gives a working directory: every character that
// is not a letter or a digit is a hyphen.
func projectName(cwd string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, cwd)
}

// makeDirs makes rel and its parents beneath base, private.
func makeDirs(base, rel string) error {
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	return errors.Join(root.MkdirAll(rel, 0o700), root.Close())
}

// hook runs the plugin's hook command for event as Claude Code does: through a shell, in the
// session's directory, with the payload on stdin and the inbox socket and token in the
// environment. active is stop_hook_active true on a Stop.
func (c *claudeCode) hook(event string, active bool) (ccRun, error) {
	command, err := c.hookCommand(event)
	if err != nil {
		return ccRun{}, err
	}
	// The payload's keys are Claude Code's own.
	p := map[string]any{"session_id": ccSessionID, "transcript_path": c.tr.Path(), "cwd": c.cwd, "hook_event_name": event}
	switch event {
	case "SessionStart":
		p["source"] = "startup"
	case "Stop":
		p["stop_hook_active"] = active
	case "SessionEnd":
		p["reason"] = "exit"
	}
	stdin, err := json.Marshal(p)
	if err != nil {
		return ccRun{}, err
	}
	return c.shell(command, string(stdin), []string{hook.EnvInboxSocket + "=" + c.inbox.Path(), hook.EnvInboxToken + "=" + c.auth}, nil)
}

// hookCommand is the one command the plugin's hooks.json names for event.
func (c *claudeCode) hookCommand(event string) (string, error) {
	var f ccHooksFile
	if err := c.readJSON(c.pluginDir+"/hooks/hooks.json", &f); err != nil {
		return "", err
	}
	groups := f.Hooks[event]
	if len(groups) != 1 || len(groups[0].Hooks) != 1 || groups[0].Hooks[0].Type != "command" {
		return "", fmt.Errorf("claude: hooks.json holds no one command for %s: %v", event, groups)
	}
	return groups[0].Hooks[0].Command, nil
}

// statusLine runs the settings' status line command, with the inbox variables of the session
// taken out of its environment (this process's own parent may have set them).
func (c *claudeCode) statusLine() (ccRun, error) {
	var s ccSettings
	if err := c.readJSON(c.settings, &s); err != nil {
		return ccRun{}, err
	}
	if s.StatusLine.Type != "command" || s.StatusLine.Command == "" {
		return ccRun{}, fmt.Errorf("claude: %s holds no statusLine command", c.settings)
	}
	stdin, err := json.Marshal(map[string]any{"session_id": ccSessionID, "cwd": c.cwd})
	if err != nil {
		return ccRun{}, err
	}
	return c.shell(s.StatusLine.Command, string(stdin), nil, []string{hook.EnvInboxSocket, hook.EnvInboxToken})
}

// readJSON decodes the file at path into out.
func (c *claudeCode) readJSON(path string, out any) error {
	data, err := testfiles.Try(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("claude: %s: %w", path, err)
	}
	return nil
}

// shell runs command as `sh -c`, as Claude Code runs a hook and the status line: a command
// that exits non-zero is an answer, not a failure of the fake.
func (c *claudeCode) shell(command, stdin string, env, unset []string) (ccRun, error) {
	res, err := execx.OS{}.Run(context.Background(), execx.Cmd{
		Name: "/bin/sh", Args: []string{"-c", command}, Stdin: strings.NewReader(stdin), Env: env, Unset: unset, Dir: c.cwd,
	})
	if exited, ok := errors.AsType[*execx.ExitError](err); ok && exited.Signal == 0 {
		err = nil
	}
	if err != nil {
		return ccRun{}, err
	}
	return ccRun{Command: command, Exit: res.ExitCode, Stdout: string(res.Stdout), Stderr: string(res.Stderr)}, nil
}

// frames is every line the inbox has read.
func (c *claudeCode) frames() []ccFrame {
	read := c.inbox.Frames()
	out := make([]ccFrame, 0, len(read))
	for _, f := range read {
		out = append(out, ccFrame{Auth: f.Auth, Content: f.Content, Accepted: f.Accepted, Reason: f.Reason})
	}
	return out
}

// transcript is the transcript's whole text.
func (c *claudeCode) transcript() (string, error) {
	if err := c.tr.Err(); err != nil {
		return "", err
	}
	data, err := testfiles.Try(filepath.Dir(c.tr.Path()), filepath.Base(c.tr.Path()))
	return string(data), err
}

// close ends the inbox.
func (c *claudeCode) close() error {
	if c.inbox == nil {
		return nil
	}
	return c.inbox.Close()
}
