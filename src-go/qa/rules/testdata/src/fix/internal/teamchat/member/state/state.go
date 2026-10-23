// Package state stands for teamchat's member files: the launch values' home, their one list
// and session.pid's check of the value it reads.
package state

import "slices"

// The launch values.
const (
	LaunchClaude = "claude"
	LaunchPlain  = "plain"

	DeliverySocket = "socket"
	DeliveryNone   = "none"
)

// Launches is the one list of launch values.
var Launches = []string{LaunchClaude, LaunchPlain}

// SessionPID is session.pid.
type SessionPID struct {
	Launch string
	Kind   string
}

// check is an allowance row: session.pid's value check.
func (p SessionPID) check() bool { return slices.Contains(Launches, p.Launch) }

// claude stands beside the row and is not one.
func (p SessionPID) claude() bool {
	return p.Launch == LaunchClaude // want `the launch field compared with "claude" outside package agent \(project_agent_branch\)`
}
