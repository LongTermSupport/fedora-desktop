// Package teamsync stands for teamchat's team goroutine: the reports join bounds itself by the
// same 30 s the long-poll is held for, both armed on the one fake clock.
package teamsync

import "time"

const (
	longPoll         = 30 * time.Second
	reportsJoinLimit = 30 * time.Second
	otherWait        = 31 * time.Second
)
