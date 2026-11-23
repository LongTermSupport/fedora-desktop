package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A session's conversation record is located in conversation.Locate alone, which knows both
// systems' records (Plan 00209 DESIGN-host-cc-v2.md section 4). The fixtures are the defect (a
// reader in package conversation joining v1's session directory and File itself), the other
// spellings outside the package (a literal, a constant built in pieces, File through another
// import name, the v2 layout's ConversationFile called, held as a value and as a method
// expression, a command), the clean forms (the locator, File's own declaration, the record's
// owners in teamchat/paths and teamchat/member, other file names, tests). The root is its own,
// because the owners and the locator are matched by package path.
func TestConversationRecord(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "conversationrecord")
	analysistest.Run(t, root, ConversationRecord,
		"fix/internal/conversation", "fix/internal/badrecord", "fix/internal/goodrecord",
		"fix/internal/teamchat/paths", "fix/internal/teamchat/member/link", "fix/cmd/tool")
}
