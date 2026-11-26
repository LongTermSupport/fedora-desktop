// Package other has a Launcher field of its own: not a session record's, so not judged.
package other

// Seat is a type of its own.
type Seat struct{ Launcher string }

// Run reads its own field.
func Run(s Seat) string { return s.Launcher }
