// Package board is the shape of src-go/internal/teams/board that project_row_kind judges: a
// row of the teams board, whose KIND a member's handle says.
package board

import "strings"

// Kind is a row's KIND.
type Kind string

// The kinds a row is given without a handle to read.
const (
	KindTeam  Kind = "team"
	KindHuman Kind = "human"
	KindCcy   Kind = "ccy"
)

// Row is one line of the board.
type Row struct {
	Team, Member string
	Kind         Kind
	Handle, Dir  string
}

// KindOf is the kind a handle's type word says.
func KindOf(handle string) Kind {
	return Kind(handle[strings.LastIndex(handle, ".")+1:])
}

// seatRow is the board's own: a seat's row takes its kind from the seat's handle.
func seatRow(team, seat, handle string) Row {
	return Row{Team: team, Member: seat, Kind: KindOf(handle), Handle: handle}
}

// ccySeatRow is the defect inside the owner's package: every seat called ccy.
func ccySeatRow(team, seat, handle string) Row {
	return Row{Team: team, Member: seat, Kind: KindCcy, Handle: handle} // want `the row's KIND is a constant though its handle says what the member is`
}

var _, _ = seatRow, ccySeatRow
