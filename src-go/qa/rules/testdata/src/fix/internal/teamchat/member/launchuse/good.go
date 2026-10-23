package launchuse

import (
	"path/filepath"
	"slices"

	"fix/internal/teamchat/member/agent"
	"fix/internal/teamchat/member/cli"
	"fix/internal/teamchat/member/wire"
)

// What the seam leaves: the adapter asked, the value spelled and resolved but never compared,
// another field compared with the same strings, Claude Code's argument found by its name.

func resolve(l wire.Launch) (agent.Capabilities, error) {
	return agent.For(agent.Agent(l.Launch))
}

func spelled(m cli.Mode) string { return string(m) }

func asked(c agent.Capabilities) bool { return !c.SessionStart && c.Delivery != "none" }

func kind(l wire.Launch) bool { return l.Kind == "claude" }

func kinds(l wire.Launch) bool { return slices.Contains([]string{"claude", "plain"}, l.Kind) }

func base(arg string) bool { return filepath.Base(arg) == "claude" }

func inbound(l wire.Launch) bool { return l.Inbound == "refuse" }
