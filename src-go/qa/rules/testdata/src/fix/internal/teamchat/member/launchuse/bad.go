// Package launchuse stands for the keeper and the link, which branched on the launch value
// before the agent seam: each original comparison is flagged.
package launchuse

import (
	"fix/internal/teamchat/member/cli"
	"fix/internal/teamchat/member/state"
	"fix/internal/teamchat/member/wire"
)

type keeper struct {
	cfg     cli.Launch
	inbound string
	failed  bool
}

// sessionLaunch is the keeper's session.pid and launch frame value.
func (k *keeper) sessionLaunch() string {
	launch := state.LaunchPlain // want `state.LaunchPlain named outside package agent`
	if k.cfg.Mode == cli.LaunchClaude { // want `an agent.Agent compared outside package agent`
		launch = state.LaunchClaude // want `state.LaunchClaude named outside package agent`
	}
	return launch
}

// argv is the keeper's plain early return.
func (k *keeper) argv() bool {
	return k.cfg.Mode != cli.LaunchClaude // want `an agent.Agent compared outside package agent`
}

// needsRelaunch is the keeper's report.json flag.
func (k *keeper) needsRelaunch() bool {
	return k.cfg.Mode == cli.LaunchClaude && k.inbound == "refuse" // want `an agent.Agent compared outside package agent`
}

// build is the link's delivery and first SessionStart.
func build(l wire.Launch) string {
	plain := l.Launch == state.LaunchPlain // want `the launch field compared with "plain" outside package agent`
	if plain {
		return state.DeliveryNone
	}
	return state.DeliverySocket
}
