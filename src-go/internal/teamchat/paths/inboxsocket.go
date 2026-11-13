package paths

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// This file is the one resolver of Claude Code's inbox socket (project_inbox_socket_dir): no
// other file names its directory.

// inboxDir is the directory Claude Code keeps its inbox sockets in, under the directory its order
// picks; inboxFallbackDir, with the uid appended, is where it binds when that path is too long.
const (
	inboxDir         = "cc-socks"
	inboxFallbackDir = "/tmp/" + inboxDir + "-"
	inboxSuffix      = ".sock"
	// inboxMaxBytes is the longest socket path Claude Code binds under the directory its order
	// picks; a longer one moves to inboxFallbackDir.
	inboxMaxBytes = 103
	// inboxDefaultTmp is os.tmpdir() when none of its variables is set.
	inboxDefaultTmp = "/tmp"
)

// InboxEnv is what Claude Code places a session's inbox socket by: its environment, as the
// session process saw it, and its uid. A raw runtime directory is not one: Claude Code falls back
// past an empty XDG_RUNTIME_DIR, and so must every reader of the path.
type InboxEnv struct {
	// RuntimeDir is XDG_RUNTIME_DIR; ClaudeTmpDir is CLAUDE_CODE_TMPDIR.
	RuntimeDir, ClaudeTmpDir string
	// TmpDir is the first of TMPDIR, TMP and TEMP that is set, os.tmpdir()'s order.
	TmpDir string
	UID    int
}

// InboxEnvOf is the InboxEnv of an environment, read with getenv, and a uid.
func InboxEnvOf(getenv func(string) string, uid int) InboxEnv {
	return InboxEnv{
		RuntimeDir:   getenv("XDG_RUNTIME_DIR"),
		ClaudeTmpDir: getenv("CLAUDE_CODE_TMPDIR"),
		TmpDir:       firstSet(getenv("TMPDIR"), getenv("TMP"), getenv("TEMP")),
		UID:          uid,
	}
}

// InboxSocket is Claude Code's per-session inbox socket for the session process pid, placed by
// Claude Code's own order (read from its 2.1.323 bundle): <dir>/cc-socks/<pid>.sock resolved,
// where dir is XDG_RUNTIME_DIR, else CLAUDE_CODE_TMPDIR, else os.tmpdir(); and
// /tmp/cc-socks-<uid>/<pid>.sock when that path is longer than 103 bytes (B 2.4.4, 2.7.1;
// issue 266). A relative directory is refused: Claude Code resolves it against its own working
// directory, which is not known here.
func InboxSocket(env InboxEnv, pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("%w pid: must be positive", ErrInvalid)
	}
	dir := firstSet(env.RuntimeDir, env.ClaudeTmpDir, env.TmpDir, inboxDefaultTmp)
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("%w inbox socket directory: must be absolute", ErrInvalid)
	}
	name := strconv.Itoa(pid) + inboxSuffix
	if socket := filepath.Join(dir, inboxDir, name); len(socket) <= inboxMaxBytes {
		return socket, nil
	}
	if env.UID < 0 {
		return "", fmt.Errorf("%w uid: must not be negative", ErrInvalid)
	}
	return inboxFallbackDir + strconv.Itoa(env.UID) + "/" + name, nil
}

// IsInboxSocket is whether path has the shape of the inbox socket of process pid in any place
// Claude Code's order can pick: absolute and clean, <pid>.sock in a directory named cc-socks or
// cc-socks-<uid>.
func IsInboxSocket(path string, pid int) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != strconv.Itoa(pid)+inboxSuffix {
		return false
	}
	dir := filepath.Base(filepath.Dir(path))
	if dir == inboxDir {
		return true
	}
	uid, ok := strings.CutPrefix(dir, inboxDir+"-")
	n, err := strconv.Atoi(uid)
	return ok && err == nil && n >= 0 && strconv.Itoa(n) == uid
}

// firstSet is the first value that is not empty, "" when none is.
func firstSet(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
