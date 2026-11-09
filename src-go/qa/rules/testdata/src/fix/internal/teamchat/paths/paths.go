// Package paths stands for teamchat's paths: a run and its files.
package paths

// Run is one session's runtime directory.
type Run struct{ dir string }

// KeeperLog is the run's keeper.log.
func (r Run) KeeperLog() string { return r.dir + "/keeper.log" }

// LinkLog is the run's link.log.
func (r Run) LinkLog() string { return r.dir + "/link.log" }
