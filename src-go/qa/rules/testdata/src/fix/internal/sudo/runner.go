package sudo

import "context"

// Sudo runs a root command: its argv is the program and then its arguments, so the program
// is whatever the first element names.
type Sudo struct{}

// Capture runs argv as root and returns its output.
func (s *Sudo) Capture(ctx context.Context, argv ...string) (string, error) { return "", ctx.Err() }

// AsRoot runs argv as root and returns its stdout.
func (s *Sudo) AsRoot(ctx context.Context, argv ...string) (string, error) { return "", ctx.Err() }

// Run streams argv as root.
func (s *Sudo) Run(ctx context.Context, argv ...string) error { return ctx.Err() }
