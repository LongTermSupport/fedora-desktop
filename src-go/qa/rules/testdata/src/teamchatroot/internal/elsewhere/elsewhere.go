// Package elsewhere is outside ccy-teams, which alone is given a teamchat root: not judged.
package elsewhere

import (
	"os"

	tp "teamchatroot/internal/teamchat/paths"
)

func read(team string) error {
	_, err := os.ReadFile(tp.StatusFile(team))
	return err
}
