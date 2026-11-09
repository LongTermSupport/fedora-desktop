package launchuse

import (
	"slices"

	"fix/internal/teamchat/member/agent"
	"fix/internal/teamchat/member/cli"
	"fix/internal/teamchat/member/state"
	"fix/internal/teamchat/member/wire"
)

// The ways round it: a local copy of a constant, a named string constant, the operands
// reversed or parenthesised, a switch on the field, a contains over the one list or a literal
// one, a string of an Agent, a switch on an Agent or its string, a map keyed by one, the owner's
// constants, and the field reached through an embedded struct or a pointer.

const plainName = "plain"

func localCopy() string {
	c := state.LaunchPlain // want `state.LaunchPlain named outside package agent`
	return c
}

func named(l wire.Launch) bool {
	return l.Launch == plainName // want `the launch field compared with "plain" outside package agent`
}

func reversed(p *state.SessionPID) bool {
	return ("claude") != (p.Launch) // want `the launch field compared with "claude" outside package agent`
}

func switched(l *wire.Launch) int {
	switch l.Launch {
	case "claude": // want `the launch field compared with "claude" outside package agent`
		return 1
	case plainName: // want `the launch field compared with "plain" outside package agent`
		return 2
	}
	return 0
}

func listed(l wire.Launch) bool {
	return slices.Contains(state.Launches, l.Launch) // want `state.Launches named outside package agent`
}

func literal(p state.SessionPID) bool {
	return slices.Contains([]string{"claude", "plain"}, p.Launch) // want `the launch field checked against a list of launch values outside package agent`
}

func converted(a agent.Agent) bool {
	return string(a) == "claude" // want `a string of an agent.Agent compared with "claude" outside package agent`
}

func switchAgent(m cli.Mode) bool {
	switch m { // want `a switch on an agent.Agent outside package agent`
	case "plain":
		return true
	}
	return false
}

func switchString(a agent.Agent) bool {
	switch string(a) {
	case plainName: // want `a string of an agent.Agent compared with "plain" outside package agent`
		return true
	}
	return false
}

func keyed() map[agent.Agent]bool { // want `a map keyed by agent.Agent outside package agent`
	return nil
}

func ownerConst() agent.Agent {
	return agent.Claude // want `agent.Claude named outside package agent`
}

func againstOther(a, b agent.Agent) bool {
	return a == b // want `an agent.Agent compared outside package agent`
}

type outer struct{ wire.Launch }

func embedded(o outer) bool {
	return o.Launch.Launch == "plain" // want `the launch field compared with "plain" outside package agent`
}
