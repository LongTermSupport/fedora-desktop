// Package system stands for a ccy-teams package that reads the machine's teamchat records.
package system

import (
	"errors"
	"fmt"
	"os"

	tp "teamchatroot/internal/teamchat/paths"
	"teamchatroot/internal/teamchat/status"
)

// Config carries the root the records are read under.
type Config struct{ TeamchatRoot string }

// StatusRecord is the defect: the machine's own record, whatever root the run was given.
func StatusRecord(team string) (string, bool, error) {
	return status.Read(tp.StatusFile(team), team) // want `a teamchat machine record read at its fixed path`
}

func bareReads(team string) error {
	if _, err := os.ReadFile(tp.StampFile(team)); err != nil { // want `a teamchat machine record read at its fixed path`
		return err
	}
	if _, err := os.ReadDir(tp.LXCRecordDir); err != nil { // want `a teamchat machine record read at its fixed path`
		return err
	}
	if _, err := os.Stat(tp.StatusRoot + "/teams"); err != nil { // want `a teamchat machine record read at its fixed path`
		return err
	}
	if _, err := os.Stat("/x" + tp.StatusFile(team)); err != nil { // want `a teamchat machine record read at its fixed path`
		return err
	}
	return nil
}

func rootOnTheRight(c Config, team string) error {
	_, err := os.ReadFile(tp.StatusFile(team) + c.TeamchatRoot) // want `a teamchat machine record read at its fixed path`
	return err
}

// StatusUnder is the fix: the record under the run's root.
func StatusUnder(teamchatRoot string) func(string) (string, bool, error) {
	return func(team string) (string, bool, error) { return status.Read(teamchatRoot+tp.StatusFile(team), team) }
}

func rooted(c Config, team string) error {
	if _, err := os.ReadFile(c.TeamchatRoot + tp.StampFile(team)); err != nil {
		return err
	}
	if _, err := os.ReadDir(c.TeamchatRoot + tp.StatusRoot + "/teams"); err != nil {
		return err
	}
	_, err := os.ReadFile(tp.SeatName("1"))
	return err
}

// Messages name the record; they read nothing.
func messages(team string) error {
	if team == "" {
		return errors.New("no team at " + tp.StatusFile(team))
	}
	return fmt.Errorf("the status record of team %s (%s)", team, tp.StatusFile(team))
}

// A record path held for a later message, not passed to a call.
func held(team string) string {
	where := tp.StatusFile(team)
	return "team " + team + " has a status record at " + where
}
