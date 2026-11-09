package badrowkind

import "fix/internal/teams/board"

// fromHandle reads the kind from the handle.
func fromHandle(team, seat, handle string) board.Row {
	return board.Row{Team: team, Member: seat, Kind: board.KindOf(handle), Handle: handle}
}

// teamRow has no member and so no handle: its kind is the row's own.
func teamRow(team string) board.Row {
	return board.Row{Team: team, Kind: board.KindTeam, Handle: "-", Dir: "-"}
}

// humanRow is a person, with no handle.
func humanRow(team, name string) board.Row {
	return board.Row{Team: team, Member: name, Kind: board.KindHuman, Handle: "-"}
}

// deferred is an LXC member that never joined: no handle yet, so its record says the kind.
func deferred(team, label string) board.Row {
	return board.Row{Team: team, Member: label, Kind: "lxc", Handle: "-"}
}

// fromVariable takes the kind a caller read from the handle.
func fromVariable(team, handle string, kind board.Kind) board.Row {
	return board.Row{Team: team, Kind: kind, Handle: handle}
}

// empty is the zero row.
func empty() board.Row { return board.Row{} }
