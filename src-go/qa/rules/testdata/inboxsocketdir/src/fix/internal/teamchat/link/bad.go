// Package link resolves Claude Code's inbox socket by itself, the defect's form among others.
package link

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// socksDir is the directory, declared once and used below.
const socksDir = "cc-socks" // want `Claude Code's inbox socket directory "cc-socks" named outside teamchat/paths/inboxsocket.go`

// SocketPID is the defect: a prefix cut under XDG_RUNTIME_DIR alone, then the path rebuilt.
func SocketPID(path string) (int, bool) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	name, ok := strings.CutPrefix(path, runtimeDir+"/cc-socks/") // want `Claude Code's inbox socket directory`
	if !ok {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSuffix(name, ".sock"))
	return pid, err == nil && runtimeDir+"/cc-socks/"+name == path // want `Claude Code's inbox socket directory`
}

// Joined builds it with a join.
func Joined(dir string, pid int) string {
	return filepath.Join(dir, "cc-socks", strconv.Itoa(pid)+".sock") // want `Claude Code's inbox socket directory`
}

// Suffixed checks its shape.
func Suffixed(path string) bool {
	return strings.HasSuffix(path, "/cc-socks/1.sock") // want `Claude Code's inbox socket directory`
}

// Pieces spells it in pieces, which is the name.
func Pieces(dir string) string {
	return dir + "/" + ("cc-" + "socks") // want `Claude Code's inbox socket directory`
}

// Fallback is the long-path fallback's directory.
func Fallback(uid int) string {
	return "/tmp/cc-socks-" + strconv.Itoa(uid) // want `Claude Code's inbox socket directory`
}

// Declared uses the declared constant: reported at its declaration, not here.
func Declared(dir string) string { return filepath.Join(dir, socksDir) }
