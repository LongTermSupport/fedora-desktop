package restore

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Toward is the direction of RS16's renames.
type Toward int

// Swapped puts the restored copy at db; Unswapped puts the old database back.
const (
	Swapped Toward = iota
	Unswapped
)

func (t Toward) String() string { return [...]string{"swapped", "unswapped"}[t] }

// The state directory's names RS16 reads.
const (
	nameDB       = "db"
	namePrevious = "db.previous"
)

// restoreName is db.restore-<n>, relative to the state directory.
func restoreName(team paths.TeamID, n int64) string {
	return filepath.Base(paths.HomeserverPrivateRestoreDatabase(team, n))
}

// StuckError is RS16's refusal: the state directory holds a set of entries no row of the table
// names, so no step can repair it. Listing is what is there, as the record's stuck holds it.
type StuckError struct {
	Team    paths.TeamID
	Listing string
}

func (e *StuckError) Error() string {
	return "the state directory of team " + e.Team.String() + " holds what no restore step made: " + e.Listing
}

// Renames is RS16: it reads db, db.previous and db.restore-<n> before each rename, and renames
// toward the direction until its row is done. Any state outside the table is a StuckError,
// and nothing is renamed.
func Renames(fsys FS, team paths.TeamID, n int64, toward Toward) error {
	r := restoreName(team, n)
	for {
		row, listing, err := readRow(fsys, team, r)
		if err != nil {
			return err
		}
		moves, ok := renameTable[row]
		if !ok {
			return &StuckError{Team: team, Listing: listing}
		}
		move := moves[toward]
		if move == [2]string{} {
			return nil
		}
		if err := fsys.Rename(team, entryName(move[0], r), entryName(move[1], r)); err != nil {
			return err
		}
	}
}

// renameTable is RS16's table: for each row of directories present, the rename toward each
// direction, none when the row is done. "r" stands for db.restore-<N>.
var renameTable = map[string][2][2]string{
	"d r": {{nameDB, namePrevious}, {}},
	"p r": {{"r", nameDB}, {namePrevious, nameDB}},
	"d p": {{}, {nameDB, "r"}},
}

func entryName(name, r string) string {
	if name == "r" {
		return r
	}
	return name
}

// readRow is the row of directories present among db, db.previous and r ("d p r" order), and
// the listing; an entry that is neither absent nor a directory makes the row one no table
// entry has.
func readRow(fsys FS, team paths.TeamID, r string) (string, string, error) {
	names := []string{nameDB, namePrevious, r}
	letters := []string{"d", "p", "r"}
	var row, parts []string
	for i, name := range names {
		k, err := fsys.Kind(team, name)
		if err != nil {
			return "", "", err
		}
		switch k {
		case KindDirectory:
			row = append(row, letters[i])
		case KindAbsent:
		case KindFile, KindOther:
			row = append(row, "?")
		}
		parts = append(parts, name+"="+k.String())
	}
	return strings.Join(row, " "), strings.Join(parts, " "), nil
}

// Startable is RS17: the live unit may start only when db is a no-follow directory in the
// state directory and restore.block is absent, so no path can make Tuwunel create an empty
// database and serve it.
func Startable(fsys FS, team paths.TeamID) error {
	k, err := fsys.Kind(team, nameDB)
	if err != nil {
		return err
	}
	if k != KindDirectory {
		return fmt.Errorf("team %s's database is not in place (db=%s): the homeserver is not started", team, k)
	}
	block, err := fsys.RootKind(paths.RestoreBlock(team))
	if err != nil {
		return err
	}
	if block != KindAbsent {
		return fmt.Errorf("%s exists: the homeserver of team %s is not started", paths.RestoreBlock(team), team)
	}
	return nil
}
