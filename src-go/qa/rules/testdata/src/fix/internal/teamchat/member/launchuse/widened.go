package launchuse

import (
	"strings"

	"fix/internal/teamchat/member/agent"
	"fix/internal/teamchat/member/state"
	"fix/internal/teamchat/member/wire"
)

// The ways round a plain comparison: the field and a launch value given to one call, a launch
// value in a list spelled as one string, a map literal indexed by the field, and the field
// passed through a call before it is compared or switched on.

func folded(l wire.Launch) bool {
	return strings.EqualFold(l.Launch, "plain") // want `the launch field given to a call with "plain" outside package agent`
}

func inOneString(p state.SessionPID) bool {
	return strings.Contains("plain,x", p.Launch) // want `the launch field given to a call with "plain,x" outside package agent`
}

func suffix(l *wire.Launch) bool {
	return strings.HasSuffix(l.Launch, "claude") // want `the launch field given to a call with "claude" outside package agent`
}

func prefix(a agent.Agent) bool {
	return strings.HasPrefix(string(a), plainName) // want `a string of an agent.Agent given to a call with "plain" outside package agent`
}

func indexed(l wire.Launch) bool {
	return map[string]bool{"plain": true}[l.Launch] // want `the launch field used as an index into a literal holding a launch value outside package agent`
}

func lowered(l wire.Launch) int {
	switch strings.ToLower(l.Launch) {
	case "plain": // want `a call over the launch field compared with "plain" outside package agent`
		return 1
	}
	return 0
}

func trimmed(p state.SessionPID) bool {
	return strings.TrimSpace(p.Launch) == "claude" // want `a call over the launch field compared with "claude" outside package agent`
}

func nested(l wire.Launch) bool {
	return strings.EqualFold(strings.TrimSpace(l.Launch), plainName) // want `a call over the launch field given to a call with "plain" outside package agent`
}

// A part of a launch value is a declared gap: it is not judged.
func partial(l wire.Launch) bool { return strings.HasPrefix(l.Launch, "pla") }

// Text that names a launch value in a sentence is not a list of them.
func message(l wire.Launch) string { return strings.Join([]string{l.Launch, "is not claude or plain"}, " ") }
