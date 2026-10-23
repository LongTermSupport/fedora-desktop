// Package keeper stands for teamchat's keeper: its own code names keeper.log freely.
package keeper

import (
	"os"

	"fix/internal/teamchat/paths"
)

// open is the keeper writing its log, which the rule does not judge.
func open(r paths.Run) (*os.File, error) { return os.Create(r.KeeperLog()) }
