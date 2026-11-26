package conversation

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ForgetTeam removes the records of every seat of team in each checkout given: what a team taken
// down does, so a team made again under its name resumes nothing of the old one (issue 417). A
// checkout with no records of the team is nothing to do. The team's record directory is removed
// whole; a link in its place is refused, never followed. The first failure is returned, naming
// its path.
func ForgetTeam(checkouts []string, team string) error {
	seats, err := parseList("x@" + team)
	if err != nil || len(seats) != 1 || seats[0].Team != team {
		return fmt.Errorf("%q is not a team name", team)
	}
	for _, checkout := range checkouts {
		dir := filepath.Join(checkout, seatRecordDir, team)
		info, err := os.Lstat(dir)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return fmt.Errorf("the seat records of team %s at %s: %w", team, dir, err)
		case !info.IsDir():
			return fmt.Errorf("the seat records of team %s at %s are not a directory, so they are left as they are", team, dir)
		}
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("could not remove the seat records of team %s at %s: %w", team, dir, err)
		}
	}
	return nil
}
