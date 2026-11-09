// Package agent is the agent program a run launches, reached through one value, so that another
// agent arrives as an adapter and not as a refactor (SPEC-n3 6.1, Plan 00215 decision O4). An
// Agent is the run's launch value, not its member kind. The keeper asks its Adapter how to
// prepare the launch, the link asks its Capabilities, and nothing else branches on the value
// (project_agent_branch).
package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Agent is a run's launch value: the agent program CMD starts.
type Agent string

// The agents, whose strings are the launch values of session.pid and the launch frame.
const (
	Claude Agent = state.LaunchClaude
	Plain  Agent = state.LaunchPlain
)

// Capabilities are the questions the run asks of its agent. Delivery is the agent's best mode
// (state.DeliverySocket or state.DeliveryNone; inbound refuse lowers socket to wait in the
// link); SessionStart is whether the run awaits the agent's first SessionStart.
type Capabilities struct {
	Delivery     string
	SessionStart bool
}

// Preparer makes the launch: what the keeper appends to CMD.
type Preparer interface {
	Prepare(f RunFacts) (Launch, error)
}

// Adapter is one agent's capabilities and preparer.
type Adapter struct {
	Capabilities Capabilities
	Preparer     Preparer
}

// Launch is what the keeper's argv holds after CMD.
type Launch struct {
	Args []string
}

// RunFacts are the run's facts and the keeper's functions a Preparer is given; no adapter is
// given a root it can write. Home is the launch's HOME as read; ClaudeConfigDir is the
// keeper's absolute, clean CLAUDE_CONFIG_DIR, or nil. WriteRunFile creates a file directly under
// Run.Dir(), mode 0600, and on a failure has logged it and returns the error to hand on.
// Conversation reads the conversation record, its error naming the state root or the record.
type RunFacts struct {
	Inbound         string
	ResumeRecorded  bool
	Kit             paths.Kit
	Run             paths.Run
	Cwd             string
	Home            string
	ClaudeConfigDir *string

	ReadTrusted  func(path string, limit int) ([]byte, error)
	WriteRunFile func(path string, data []byte) error
	Conversation func() (state.Conversation, error)
	Logf         func(format string, args ...any)
}

// MaxSettings is the cap on the kit's settings file.
const MaxSettings = 64 << 10

// inboundKey is the settings key that carries the run's inbound to the plugin.
const inboundKey = "crossSessionInbound"

const msgSettings exits.Template = "the kit's settings {} are not a JSON object without the inbound key"

// For is a's adapter. cli's parse and the launch frame's check refuse any other value first.
func For(a Agent) (Adapter, error) {
	switch a {
	case Claude:
		return Adapter{Capabilities: Capabilities{Delivery: state.DeliverySocket, SessionStart: true}, Preparer: claude{}}, nil
	case Plain:
		return Adapter{Capabilities: Capabilities{Delivery: state.DeliveryNone}, Preparer: plain{}}, nil
	}
	return Adapter{}, fmt.Errorf("agent: launch %q is not claude or plain", string(a))
}

// plain is a plain command: nothing read, nothing appended.
type plain struct{}

func (plain) Prepare(RunFacts) (Launch, error) { return Launch{}, nil }

// claude is Claude Code (B 2.4.1 step 5): the kit's settings with the inbound key, written to
// the run before the resume rule runs, then the resume, the plugin and those settings.
type claude struct{}

func (claude) Prepare(f RunFacts) (Launch, error) {
	settings := f.Kit.Settings()
	data, err := f.ReadTrusted(settings, MaxSettings)
	var obj map[string]json.RawMessage
	if err == nil {
		err = json.Unmarshal(data, &obj)
	}
	if _, has := obj[inboundKey]; err != nil || obj == nil || has {
		f.Logf("read %s: %v", settings, err)
		return Launch{}, exits.New(exits.CodeRuntime, nil, msgSettings, exits.Name(settings))
	}
	obj[inboundKey], err = json.Marshal(f.Inbound)
	if err != nil {
		return Launch{}, err
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return Launch{}, err
	}
	if err := f.WriteRunFile(f.Run.Settings(), out); err != nil {
		return Launch{}, err
	}
	var args []string
	if f.ResumeRecorded {
		args = append(args, resume(f)...)
	}
	args = append(args, "--plugin-dir", f.Kit.PluginDir(), "--settings", f.Run.Settings())
	return Launch{Args: args}, nil
}

// resume is --resume <id> when the conversation record names a session whose transcript is in
// the session's project directory; else nothing, and keeper.log says why.
func resume(f RunFacts) []string {
	conv, err := f.Conversation()
	if err != nil {
		f.Logf("no resume: %v", err)
		return nil
	}
	id := conv.SessionID
	if id == "" || filepath.Base(id) != id || strings.HasPrefix(id, ".") {
		f.Logf("no resume: the conversation record names no session id")
		return nil
	}
	path := filepath.Join(claudeDir(f), "projects", projectDir(f.Cwd), id+".jsonl")
	var st unix.Stat_t
	if err := unix.Fstatat(unix.AT_FDCWD, path, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG {
		f.Logf("no resume: the transcript %s is not a regular file (%v)", path, err)
		return nil
	}
	return []string{"--resume", id}
}

// claudeDir is Claude Code's configuration directory: CLAUDE_CONFIG_DIR when absolute and
// clean, else ~/.claude.
func claudeDir(f RunFacts) string {
	if f.ClaudeConfigDir != nil {
		return *f.ClaudeConfigDir
	}
	return filepath.Join(f.Home, ".claude")
}

// projectDir is the project directory Claude Code derives from a working directory: every byte
// that is not a letter or a digit as '-'.
func projectDir(cwd string) string {
	b := []byte(cwd)
	for i, c := range b {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			b[i] = '-'
		}
	}
	return string(b)
}
