// Package conversation stands for the host's reader of a session's conversation record.
package conversation

import (
	"path/filepath"

	"fix/internal/teamchat/paths"
	"fix/internal/teams/membership"
)

// File is the v1 record's name: its own declaration is not a reading of it.
const File = "conversation.json"

// Locate is the one locator: it may name either system's record.
func Locate(checkout, session string, v2 bool) string {
	if v2 {
		return paths.Layout{Root: checkout}.ConversationFile()
	}
	return filepath.Join(membership.SessionDir(checkout, session), File)
}

// read is the defect: a reader that joins the v1 directory and the file itself.
func read(checkout, session string) string {
	return filepath.Join(membership.SessionDir(checkout, session), File) // want `conversation record's path named outside conversation.Locate`
}

// other is a second declaration of the name, which is not the record's File.
const other = "conversation.json" // want `conversation record's path named outside conversation.Locate`

var _, _ = read, other
