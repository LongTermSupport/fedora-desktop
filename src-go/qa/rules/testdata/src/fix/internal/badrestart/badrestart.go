// Package badrestart is code under test that takes a hook, as teamsync's engine does.
package badrestart

// Engine runs its hook on a goroutine of its own.
type Engine struct{ Hook func() error }

// Server is not a fake: a package whose name does not start with fake.
type Server struct{}

// Restart restarts it.
func (*Server) Restart() error { return nil }
