package conversation

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ForgetTeam removes the records of every seat of team in each checkout given: what a team taken
// down does, so a team made again under its name resumes nothing of the old one (issue 417). A
// checkout with no records of the team is nothing to do. The team's record directory is removed
// whole, through an os.Root on the checkout so nothing outside it can be reached, and only when
// every directory from .claude down to it is a real directory: a link anywhere on that path (a
// container may write .claude/ccy as its own) is refused, never followed. The first failure is
// returned, naming its path.
func ForgetTeam(checkouts []string, team string) error {
	seats, err := parseList("x@" + team)
	if err != nil || len(seats) != 1 || seats[0].Team != team {
		return fmt.Errorf("%q is not a team name", team)
	}
	for _, checkout := range checkouts {
		if err := forgetIn(checkout, team); err != nil {
			return err
		}
	}
	return nil
}

// forgetIn removes team's record directory in one checkout.
func forgetIn(checkout, team string) (err error) {
	root, err := os.OpenRoot(checkout)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("the seat records of team %s in %s: %w", team, checkout, err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing %s: %w", checkout, closeErr))
		}
	}()
	rel := filepath.Join(seatRecordDir, team)
	parts := strings.Split(rel, string(filepath.Separator))
	above := len(strings.Split(seatRecordDir, string(filepath.Separator)))
	present, err := realDirs(root, checkout, team, parts[:above])
	if err != nil || !present {
		return err
	}
	lock, err := root.OpenFile(filepath.Join(seatRecordDir, seatRecordLock), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("could not open the seat-record lock of %s: %w", checkout, err)
	}
	return withSeatRecordLock(lock, func() error {
		present, err := realDirs(root, checkout, team, parts)
		if err != nil || !present {
			return err
		}
		if err := root.RemoveAll(rel); err != nil {
			return fmt.Errorf("could not remove the seat records of team %s at %s: %w", team, filepath.Join(checkout, rel), err)
		}
		return nil
	})
}

// realDirs is whether every directory from the first of parts to the last is there, each a real
// directory under root: false when one is missing, an error when one is not a directory (a link is
// never followed) or cannot be read.
func realDirs(root *os.Root, checkout, team string, parts []string) (bool, error) {
	for i := range parts {
		sub := filepath.Join(parts[:i+1]...)
		info, err := root.Lstat(sub)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return false, nil
		case err != nil:
			return false, fmt.Errorf("the seat records of team %s: %s: %w", team, filepath.Join(checkout, sub), err)
		case !info.IsDir():
			return false, fmt.Errorf("the seat records of team %s are left as they are: %s is not a directory (a link is never followed)",
				team, filepath.Join(checkout, sub))
		}
	}
	return true, nil
}
