// Package badseattext is the fixture of project_seat_number_text: text that has a person type
// or pick a seat.
package badseattext

import (
	"errors"
	"fmt"
	"strings"
)

// Seat is one seat of a checkout.
type Seat struct {
	Name   string
	Handle string
}

const placeholderUsage = "usage: ccy-teams seats model SEAT@TEAM" // want `a seat placeholder a person is told to fill \(project_seat_number_text\)`

func launchLine(s Seat, team string) string {
	return "take free seat " + s.Name + ": ccy --teams " + s.Name + "@" + team // want `a launch that spells out the seat \(project_seat_number_text\)`
}

func placeholder(team string) error {
	return errors.New("then ccy --teams <seat>@" + team) // want `a launch that spells out the seat \(project_seat_number_text\)`
}

func usage() string {
	return placeholderUsage
}

func formatted(seat, team string) error {
	return fmt.Errorf("run ccy --teams %s@%s", seat, team) // want `a launch that spells out the seat \(project_seat_number_text\)`
}

func handTo(team string) string {
	return team + ": say --coordinator=hand-to=<seat>" // want `a command that names the seat \(project_seat_number_text\)`
}

func repair(seat, team string) string {
	return fmt.Sprintf("ccy-teams seats conversation %s@%s --set <id>", seat, team) // want `a command that names the seat \(project_seat_number_text\)`
}

func listed(seats []string) string {
	seatList := strings.Join(seats, ",")
	return "ccy --teams " + seatList // want `a launch with its seat list filled in \(project_seat_number_text\)`
}

func repairList(list, id string) string {
	return fmt.Sprintf("ccy-teams seats conversation %s --set %s", list, id) // want `a launch with its seat list filled in \(project_seat_number_text\)`
}

func seatRow(s Seat) string {
	return fmt.Sprintf("seat %-6s  free", s.Name) // want `a choice's row labelled by its seat \(project_seat_number_text\)`
}

func handleRows(seats []Seat) []string {
	var rows []string
	for _, s := range seats {
		rows = append(rows, s.Handle+"   free") // want `a choice's row that shows a handle, which carries the seat \(project_seat_number_text\)`
	}
	return rows
}
