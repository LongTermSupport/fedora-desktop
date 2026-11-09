// Package badrowkind builds board rows for members whose handle says what they are, and states
// the kind instead of reading it.
package badrowkind

import (
	"fix/internal/teams/board"
)

// seatRow is the teams board's original: every seat of a checkout was called ccy, so a host cc
// session (<repo>.<seat>+<host>.host) showed as ccy.
func seatRow(team, seat, handle string) board.Row {
	return board.Row{Team: team, Member: seat, Kind: board.KindCcy, Handle: handle} // want `the row's KIND is a constant though its handle says what the member is`
}

// lxcByWord states the kind as a converted literal.
func lxcByWord(team, handle string) board.Row {
	return board.Row{Team: team, Member: handle, Kind: board.Kind("lxc"), Handle: handle} // want `the row's KIND is a constant though its handle says what the member is`
}

// untyped states it as an untyped string constant.
func untyped(team, handle string) board.Row {
	return board.Row{Team: team, Kind: "ccy", Handle: handle} // want `the row's KIND is a constant though its handle says what the member is`
}

// noKind leaves the KIND empty for a member with a handle.
func noKind(team, handle string) *board.Row {
	return &board.Row{Team: team, Handle: handle} // want `the row has a handle and no KIND`
}

// aliased reaches the type through an alias and a named constant of this package.
type row = board.Row

const podman = board.KindCcy

func aliased(team, handle string) row {
	return row{Team: team, Kind: podman, Handle: handle} // want `the row's KIND is a constant though its handle says what the member is`
}

// inSlice is a row literal with its type elided in a slice literal.
func inSlice(team, handle string) []board.Row {
	return []board.Row{{Team: team, Kind: board.KindCcy, Handle: handle}} // want `the row's KIND is a constant though its handle says what the member is`
}

// unkeyed names no field, so neither can be read: it is flagged.
func unkeyed(team, handle string) board.Row {
	return board.Row{team, "", board.KindCcy, handle, ""} // want `a board.Row with unkeyed fields`
}
