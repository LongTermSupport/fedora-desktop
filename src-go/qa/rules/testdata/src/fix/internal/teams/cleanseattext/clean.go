// Package cleanseattext is the clean fixture of project_seat_number_text: the launch named by
// team, a seat in words, and a seat named for diagnosis.
package cleanseattext

import (
	"errors"
	"fmt"
)

// Seat is one seat of a checkout.
type Seat struct {
	Name   string
	Role   string
	Handle string
}

func launch(team string) string {
	return "run ccy --teams " + team + " at a terminal"
}

func argv(s Seat, team string) []string {
	return []string{"--teams", s.Name + "@" + team}
}

func diagnosis(s Seat, team string) error {
	return fmt.Errorf("seat %s of %s is held by another session", s.Name, team)
}

func refused(team string) error {
	return errors.New("no seat of " + team + " was found in this checkout")
}

func echoed(value string) string {
	return "--coordinator=hand-to=" + value + ": no other member has that seat"
}

func recorded(list string) string {
	return "record: --teams " + list
}

func roleRow(s Seat, team string) string {
	return "The " + s.Role + " of " + team + ": last ran 2 hours ago"
}
