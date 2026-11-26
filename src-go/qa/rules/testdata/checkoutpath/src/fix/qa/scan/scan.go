// Package scan is a QA gate of the nested qa module: it reads checkout files to judge them, on
// the checkout it was run in, never a container's. Not judged.
package scan

import "os"

// Read reads one.
func Read() ([]byte, error) { return os.ReadFile(".claude/ccy/mounts") }
