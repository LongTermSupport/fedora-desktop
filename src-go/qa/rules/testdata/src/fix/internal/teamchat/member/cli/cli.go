// Package cli stands for teamchat's command line: its names for the launch values, the parse
// and the spelling.
package cli

import (
	"errors"
	"path/filepath"

	"fix/internal/teamchat/member/agent"
)

// Mode is a launch's agent.
type Mode = agent.Agent

// cli's names for the launch values: an allowance row.
const (
	LaunchClaude = agent.Claude
	LaunchPlain  = agent.Plain
)

// Launch is run's parsed command line.
type Launch struct {
	Mode           Mode
	ResumeRecorded bool
	Cmd            []string
}

// ParseLaunch is an allowance row: --resume-recorded refused on plain.
func ParseLaunch(claude, resume bool, cmd []string) (Launch, error) {
	l := Launch{ResumeRecorded: resume, Cmd: cmd}
	l.fromFlags(claude)
	if l.Mode == LaunchPlain && l.ResumeRecorded {
		return Launch{}, errors.New("--resume-recorded is only for a --claude launch")
	}
	if !l.checkClaudeCmd() {
		return Launch{}, errors.New("no claude")
	}
	return l, nil
}

// fromFlags is an allowance row: the spelling.
func (l *Launch) fromFlags(claude bool) {
	l.Mode = LaunchPlain
	if claude {
		l.Mode = LaunchClaude
	}
}

// checkClaudeCmd compares no launch value: it needs no row.
func (l *Launch) checkClaudeCmd() bool {
	for _, a := range l.Cmd {
		if filepath.Base(a) == "claude" {
			return true
		}
	}
	return false
}

// plain stands beside the rows and is not one.
func (l *Launch) plain() bool {
	return l.Mode == LaunchPlain // want `an agent.Agent compared outside package agent`
}
