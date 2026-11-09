package ccc

import (
	"errors"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tokens"
)

// Agent is one program ccc can run in the folder. Claude is the only one today; another (an
// opencode, say) is one more entry in agents with its own flag, command, guide file and
// sign-in, and the folder, the mounts, the snapshot and the terminal guard stay as they are.
type Agent struct {
	// Name labels the container and names the agent's state directory.
	Name string
	// Flag selects the agent on the command line.
	Flag string
	// GuideFile is the file in the folder the agent reads its standing instructions from.
	GuideFile string
	// LoginEnv is the variable the agent reads its sign-in from.
	LoginEnv string
	// ConfigEnv is the variable that points the agent at its state directory.
	ConfigEnv string
	// Command is the agent's argv in the container; resume continues the last conversation.
	Command func(resume bool) []string
	// SignIn is the argv, after the image, that makes a long-lived login token.
	SignIn []string
	// Entrypoint runs SignIn.
	Entrypoint string
}

// claudeName is Claude Code's program and the agent's name.
const claudeName = "claude"

// Claude is Claude Code, the default agent. It runs with its permission prompts off, as ccy
// does: the container, which holds only the folder, is the boundary.
var Claude = Agent{
	Name:      claudeName,
	Flag:      "--claude",
	GuideFile: "CLAUDE.md",
	LoginEnv:  "CLAUDE_CODE_OAUTH_TOKEN",
	ConfigEnv: "CLAUDE_CONFIG_DIR",
	Command: func(resume bool) []string {
		argv := []string{claudeName, "--dangerously-skip-permissions"}
		if resume {
			argv = append(argv, "--continue")
		}
		return argv
	},
	SignIn:     []string{"setup-token"},
	Entrypoint: claudeName,
}

// agents is every agent a flag can select; the first is the default.
var agents = []Agent{Claude}

// options is one command line read.
type options struct {
	Agent    Agent
	Token    string
	Continue bool
	Help     bool
}

// errUsage is a command line ccc does not take.
var errUsage = errors.New("usage")

func usageErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errUsage, fmt.Sprintf(format, args...))
}

// parseArgs reads the command line: at most one agent flag, --continue, --token NAME, -h.
func parseArgs(args []string) (options, error) {
	o := options{Agent: agents[0]}
	chosen := false
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "-h", "--help":
			o.Help = true
		case "--continue":
			o.Continue = true
		case "--token":
			if i+1 >= len(args) {
				return o, usageErr("--token needs a token name")
			}
			i++
			if err := tokens.ValidName(args[i]); err != nil {
				return o, usageErr("%v", err)
			}
			o.Token = args[i]
		default:
			agent, ok := agentByFlag(arg)
			if !ok {
				return o, usageErr("unknown argument %q", arg)
			}
			if chosen {
				return o, usageErr("one agent at a time")
			}
			o.Agent, chosen = agent, true
		}
	}
	return o, nil
}

func agentByFlag(flag string) (Agent, bool) {
	for _, a := range agents {
		if a.Flag == flag {
			return a, true
		}
	}
	return Agent{}, false
}
