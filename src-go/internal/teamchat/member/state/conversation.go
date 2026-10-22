package state

import (
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// MaxConversation bounds conversation.json.
const MaxConversation = 1 << 10

const conversationFile = "conversation.json"

// Conversation is B 2.3.3's conversation record: the session id a relaunch resumes, when its
// run equals the seat report's. A resume hint, never authority; it holds no secret.
type Conversation struct {
	V         int         `json:"v"`
	Run       paths.RunID `json:"run"`
	SessionID string      `json:"session_id"`
	Source    string      `json:"source"`
	WrittenMS int64       `json:"written_ms"`
}

// Encode is the file's bytes.
func (r Conversation) Encode() ([]byte, error) {
	r.V = Version
	return encode(conversationFile, r, DecodeConversation)
}

// DecodeConversation reads conversation.json strictly; the host reads a ccy session's by the
// same rule (C9).
func DecodeConversation(data []byte) (Conversation, error) {
	var r Conversation
	if _, err := decodeStrict(conversationFile, data, MaxConversation, &r, func(c *rules) { r.check(c) }); err != nil {
		return Conversation{}, err
	}
	return r, nil
}

func (r Conversation) check(c *rules) {
	c.need(validSessionID(r.SessionID), "session_id is not a session id")
	c.need(slices.Contains(sources, r.Source), "source is not startup, resume, clear or compact")
	c.need(validMS(r.WrittenMS), "written_ms is not a time")
}
