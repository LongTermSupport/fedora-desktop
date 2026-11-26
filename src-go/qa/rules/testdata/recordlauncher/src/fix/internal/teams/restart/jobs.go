// Package restart is the second instance the sweep found: a team restart's start of a stopped
// session.
package restart

import "fix/internal/registry"

type job struct{ rec registry.Record }

func (j *job) command(args []string) []string {
	return append([]string{"env", "CCY_SESSION_RESTORE=1", j.rec.Launcher}, args...) // want `the launcher= path of a session record read outside internal/registry`
}
