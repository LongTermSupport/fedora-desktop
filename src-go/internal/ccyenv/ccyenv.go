// Package ccyenv answers the two questions every ccy host tool asks before it starts: is CCY
// installed here, and is this a CCY container (where a host tool has no business running).
package ccyenv

import (
	"os"
	"path/filepath"
)

const (
	// SessionLib is the file of the CCY library that marks it installed.
	SessionLib = "tmux-session.bash"
	// Workspace is where ccy mounts a project inside its container.
	Workspace = "/workspace"
)

// Missing is the refusal a tool prints when CCY cannot be used here, ending in a newline, or ""
// when the library in lib is installed. inContainer says whether this is a CCY container.
func Missing(tool, lib string, inContainer func() bool) string {
	file := filepath.Join(lib, SessionLib)
	if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() {
		return ""
	}
	if inContainer() {
		return tool + " runs on the host, not inside a CCY container.\n"
	}
	return "CCY is not installed here (" + file + " missing). Run playbooks/imports/play-claude-yolo.yml.\n"
}

// InContainer reports whether this process runs in a CCY container.
func InContainer() bool { return InContainerAt(Workspace) }

// InContainerAt is InContainer for a workspace at dir.
func InContainerAt(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}
