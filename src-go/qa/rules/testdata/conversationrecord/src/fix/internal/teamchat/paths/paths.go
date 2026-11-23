// Package paths stands for teamchat's layout, an owner of the record: not judged.
package paths

// The record's owner names its file freely.
const conversationFile = "conversation.json"

// Layout is a member's state layout.
type Layout struct{ Root string }

// ConversationFile is the member's conversation record.
func (l Layout) ConversationFile() string { return l.Root + "/" + conversationFile }
