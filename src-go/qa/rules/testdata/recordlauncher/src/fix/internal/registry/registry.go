// Package registry is the owner: it reads and writes the launcher= line, and may read the field.
package registry

// Record is a session record.
type Record struct {
	Launcher string
	Prefix   string
}

// Line is the record's launcher= line.
func Line(r Record) string { return "launcher=" + r.Launcher }

// Launcher is the fixed table's program for the record's prefix.
func Launcher(r Record) string { return "/var/local/" + r.Prefix }
