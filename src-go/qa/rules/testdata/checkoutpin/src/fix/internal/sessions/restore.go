// Package sessions reads checkout files the host applies outside the pin package.
package sessions

import (
	"os"
	"path/filepath"

	"fix/internal/checkoutpin"
)

// composeFiles is the defect: restore's own list of the compose files a launch runs `up -d` on.
var composeFiles = []string{"docker-compose.yml", // want `"docker-compose.yml", a checkout file the host applies, named outside internal/checkoutpin`
	"compose.yaml"} // want `"compose.yaml", a checkout file the host applies, named outside internal/checkoutpin`

// mountsFile is the project mounts spelt whole.
const mountsFile = ".claude/ccy/mounts" // want `".claude/ccy/mounts", a checkout file the host applies, named outside internal/checkoutpin`

// lastLaunch is the saved launch choices a launch nobody attends takes, spelt whole and as the
// file's own name.
const lastLaunch = ".claude/ccy/.last-launch.conf" // want `".claude/ccy/.last-launch.conf", a checkout file the host applies, named outside internal/checkoutpin`

// LastLaunch joins the file's own name.
func LastLaunch(dir string) string {
	return filepath.Join(dir, ".claude/ccy", ".last-launch.conf") // want `".last-launch.conf", a checkout file the host applies, named outside internal/checkoutpin`
}

// Override is an override file in a constant expression.
func Override(dir string) string {
	return filepath.Join(dir, "docker-compose"+".override.yml") // want `"docker-compose.override.yml", a checkout file the host applies, named outside internal/checkoutpin`
}

// Mounts reads it.
func Mounts(dir string) ([]byte, error) { return os.ReadFile(filepath.Join(dir, mountsFile)) }

// Clean asks the pin package, and names words that are only part of a name.
func Clean(dir string) bool {
	_ = "mounts"
	_ = "compose"
	return checkoutpin.HasCompose(dir) && len(composeFiles) > 0
}
