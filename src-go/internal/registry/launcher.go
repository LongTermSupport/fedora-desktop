package registry

import (
	"fmt"
	"path/filepath"
)

// LauncherDirEnv names the directory a suite's fake launchers are in, each named for its prefix.
// It is the host's own environment (the restore unit's, or the person's shell), which no
// container session can set: the same trust as CCY_LIB, which names the code the launcher
// sources. Unset, the table is the installed launchers.
const LauncherDirEnv = "CCY_SESSIONS_LAUNCHER_DIR"

// launchers is the fixed table a restore or a restart starts a record's session by: the
// installed launcher of each prefix (play-claude-yolo.yml installs both).
var launchers = map[string]string{
	"ccy":    "/var/local/claude-yolo/claude-yolo",
	prefixCC: "/var/local/claude-code/cc",
}

// UnknownPrefixError is a record whose prefix names no launcher in the table.
type UnknownPrefixError struct{ Prefix string }

func (e *UnknownPrefixError) Error() string {
	return fmt.Sprintf("its prefix '%s' names no launcher a restore starts (ccy or cc)", e.Prefix)
}

// Launcher is the program that starts rec's session: the table's entry for its prefix, never
// the launcher= path the record holds. A record is a file any process of the user can write,
// so a recorded path would be a program the next boot runs unasked (Plan 00266 Task A.4).
func Launcher(getenv func(string) string, rec Record) (string, error) {
	path, ok := launchers[rec.Prefix]
	if !ok {
		return "", &UnknownPrefixError{Prefix: rec.Prefix}
	}
	dir := getenv(LauncherDirEnv)
	if dir == "" {
		return path, nil
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("%s is '%s', not an absolute directory", LauncherDirEnv, dir)
	}
	return filepath.Join(dir, rec.Prefix), nil
}
