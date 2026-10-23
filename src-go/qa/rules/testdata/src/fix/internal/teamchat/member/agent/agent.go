// Package agent stands for teamchat's agent seam, the owner: it names and branches on the
// launch values freely.
package agent

import (
	"errors"

	"fix/internal/teamchat/member/state"
)

// Agent is a run's launch value.
type Agent string

// The agents.
const (
	Claude Agent = state.LaunchClaude
	Plain  Agent = state.LaunchPlain
)

// Capabilities are what the run asks of its agent.
type Capabilities struct {
	Delivery     string
	SessionStart bool
}

var byAgent = map[Agent]Capabilities{Claude: {state.DeliverySocket, true}, Plain: {state.DeliveryNone, false}}

// For is the agent's capabilities.
func For(a Agent) (Capabilities, error) {
	switch a {
	case Claude, Plain:
		return byAgent[a], nil
	}
	if string(a) == "plain" || a == Plain {
		return Capabilities{}, nil
	}
	return Capabilities{}, errors.New("agent: not claude or plain")
}
