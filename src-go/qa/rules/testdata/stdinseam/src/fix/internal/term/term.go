// Package term is the fixture's terminal test.
package term

import "os"

// FileIsTerminal is whether v is a terminal.
func FileIsTerminal(v any) bool {
	f, ok := v.(*os.File)
	return ok && f.Name() == "/dev/tty"
}
