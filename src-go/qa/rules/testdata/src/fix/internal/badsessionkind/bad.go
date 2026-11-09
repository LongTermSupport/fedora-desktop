// Package badsessionkind asks whether a row is a seat session by comparing its kind with ccy,
// which a cc session on the desktop is not.
package badsessionkind

import "fix/internal/teams/board"

// sessionLine is ccy-teams --graph as it was: a host seat lost its session line.
func sessionLine(r board.Row) string {
	if r.Kind == board.KindCcy && r.Dir != "-" { // want `a seat session is a ccy or a cc one: ask Kind.IsSession`
		return "session"
	}
	return ""
}

func notASession(r board.Row) bool {
	return r.Kind != board.KindCcy // want `a seat session is a ccy or a cc one: ask Kind.IsSession`
}

func reversed(r board.Row) bool {
	return board.KindCcy == r.Kind // want `a seat session is a ccy or a cc one: ask Kind.IsSession`
}
