package engine

import (
	"context"
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const msgQuestionsRefused exits.Template = "the homeserver forbids this member's post to the questions room of team {}; tell a human"

// questionsTarget is the sender's half of QX3 for a queued questions post: posted only once the
// index is ready (held otherwise), and only while a question's number is still free; a post
// whose number another question took is refused at its post (n3d-D27) and removed.
func (e *Engine) questionsTarget(ctx context.Context, h *held, p nextPost, r proto.Roster) (string, *sendHold, bool) {
	v, err := proto.Parse(p.entry.Content)
	if err != nil {
		return "", e.unposted(ctx, h, p.entry, err), true
	}
	switch err := p.tm.QuestionsPostable(v); {
	case errors.Is(err, team.ErrQuestionsNotReady):
		return "", e.hold(ctx, h), true
	case err != nil:
		return "", e.unposted(ctx, h, p.entry, err), true
	case r.Rooms.Questions == "":
		e.logf("team %s: the roster names no questions room, so questions post %s waits", p.id, p.entry.Txn)
		return "", e.hold(ctx, h), true
	}
	return r.Rooms.Questions, nil, false
}
