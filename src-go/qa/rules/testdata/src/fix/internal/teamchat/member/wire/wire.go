// Package wire stands for teamchat's keeper-link frames: the launch frame and its value check.
package wire

import (
	"slices"

	"fix/internal/teamchat/member/state"
)

// Launch is the keeper's first frame.
type Launch struct {
	Launch  string
	Kind    string
	Inbound string
}

// check is an allowance row: the launch frame's value check.
func (l *Launch) check() string {
	if !slices.Contains(state.Launches, l.Launch) {
		return "launch is not claude or plain"
	}
	return ""
}

// plain stands beside the row and is not one.
func (l *Launch) plain() bool {
	return l.Launch == "plain" // want `the launch field compared with "plain" outside package agent`
}
