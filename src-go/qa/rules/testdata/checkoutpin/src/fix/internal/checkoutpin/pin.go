// Package checkoutpin is the owner: it names the files it pins.
package checkoutpin

import (
	"os"
	"path/filepath"
)

// Mounts is the project mounts file.
const Mounts = ".claude/ccy/mounts"

// ComposeFiles are the compose files.
var ComposeFiles = []string{"docker-compose.yml", "compose.yaml", "podman-compose.override.yml"}

// HasCompose is whether dir holds a compose file.
func HasCompose(dir string) bool {
	for _, name := range ComposeFiles {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}
