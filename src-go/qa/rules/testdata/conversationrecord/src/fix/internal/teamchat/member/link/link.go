// Package link stands for the member's writer of its record, an owner: not judged.
package link

import "fix/internal/teamchat/paths"

// The member's own writer is the record's owner: clean.
func write(l paths.Layout) (string, string) { return l.ConversationFile(), "conversation.json" }

var _ = write
