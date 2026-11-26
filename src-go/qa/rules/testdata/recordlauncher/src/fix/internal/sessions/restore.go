// Package sessions holds the defect: a restore ran the record's own launcher path.
package sessions

import "fix/internal/registry"

// Command is the defect as restore.go had it.
func Command(rec registry.Record, args []string) []string {
	command := []string{"env", "CCY_SESSION_RESTORE=1"}
	return append(append(command, rec.Launcher), args...) // want `the launcher= path of a session record read outside internal/registry`
}

// Fixed is the fix: the table's program.
func Fixed(rec registry.Record, args []string) []string {
	return append([]string{"env", registry.Launcher(rec)}, args...)
}

// Same compares two records: a comparison runs nothing.
func Same(x, y registry.Record) bool { return x.Launcher == y.Launcher && x.Prefix == y.Prefix }

// Differs is the other comparison.
func Differs(x registry.Record) bool { return x.Launcher != "" }

// Declared writes the field: a write is not a read.
func Declared(rec *registry.Record) registry.Record {
	rec.Launcher = "/var/local/claude-yolo/claude-yolo"
	return registry.Record{Launcher: rec.Prefix}
}

// Held keeps it, which is a read too.
func Held(rec registry.Record) string {
	path := rec.Launcher // want `the launcher= path of a session record read outside internal/registry`
	return path
}
