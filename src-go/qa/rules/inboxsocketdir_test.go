package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// Claude Code's inbox socket directory is named in one file (teamchat/paths/inboxsocket.go, the
// resolver of Claude Code's own order). The fixtures are the ways a second resolver names it: the
// defect itself (the link's prefix cut under a raw XDG_RUNTIME_DIR and its rebuilt path), a join,
// a suffix check, a declared constant, the name in pieces, the long-path fallback's cc-socks-<uid>,
// and the clean forms beside them (a comment, another name, the resolver's answer); the exempt
// file, a sibling of it, a file of the same name in another package, a command and a test file
// complete the picture. They have a testdata root of their own, because the exemption is a path.
func TestInboxSocketDir(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "inboxsocketdir")
	analysistest.Run(t, root, InboxSocketDir,
		"fix/internal/teamchat/link", "fix/internal/teamchat/paths", "fix/internal/elsewhere", "fix/cmd/tool")
}
