// Package badrecord names the record's path outside the locator, every way the rule sees.
package badrecord

import (
	"path/filepath"

	conv "fix/internal/conversation"
	"fix/internal/teamchat/paths"
	"fix/internal/teams/membership"
)

const name = "conversation.json" // want `conversation record's path named outside conversation.Locate`

const pieces = "conversation" + ".json" // want `conversation record's path named outside conversation.Locate`

// joined is the defect's spelling outside the package, through an import of another name.
func joined(checkout, session string) string {
	return filepath.Join(membership.SessionDir(checkout, session), conv.File) // want `conversation record's path named outside conversation.Locate`
}

// literal names the file in a call.
func literal(dir string) string {
	return filepath.Join(dir, "conversation.json") // want `conversation record's path named outside conversation.Locate`
}

// v2 is the v2 record's path, by its layout.
func v2(l paths.Layout) string {
	return l.ConversationFile() // want `conversation record's path named outside conversation.Locate`
}

// value holds the method without calling it.
func value(l paths.Layout) func() string {
	return l.ConversationFile // want `conversation record's path named outside conversation.Locate`
}

// expr is a method expression.
var expr = paths.Layout.ConversationFile // want `conversation record's path named outside conversation.Locate`

var _, _, _, _, _, _, _ = name, pieces, joined, literal, v2, value, expr
