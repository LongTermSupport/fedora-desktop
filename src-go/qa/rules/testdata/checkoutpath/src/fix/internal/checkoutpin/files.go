// Package checkoutpin is the seam: it may name and open checkout paths.
package checkoutpin

import (
	"os"
	"path/filepath"
)

// Probe stats the checkout's ccy directory.
func Probe(dir string) error {
	_, err := os.ReadDir(filepath.Join(dir, ".claude/ccy"))
	return err
}
