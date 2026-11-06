// Package fakehs is a stub of internal/teamchat/matrix/fakehs: a fake server a test stops and
// restarts.
package fakehs

// Server is a fake homeserver.
type Server struct{}

// Start serves.
func (*Server) Start() error { return nil }

// Stop stops serving.
func (*Server) Stop() error { return nil }

// Restart stops and serves again.
func (*Server) Restart() error { return nil }

// Close is Stop, for a cleanup.
func (*Server) Close() error { return nil }
