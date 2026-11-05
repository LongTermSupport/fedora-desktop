package teamsync

import (
	"context"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// IndexBuilder is QX1's build and catch-up of the question index, run by a goroutine of the
// builder's own, never on the delivery path (n3f-c2 builds it; the team goroutine holds the
// seam).
type IndexBuilder interface {
	// Begin starts the job, ending one still running.
	Begin(ctx context.Context, job IndexJob)
	// Stop ends a running job and waits for it (QS9's demotion, the team's end).
	Stop()
}

// IndexJob is what the builder needs to build or catch the index up: the store's `questions`
// key as it is now (nil for a build from the room's start; a token for a catch-up; a `build`
// record for a build owed or under way), the room, and the questions floor items resume from
// (QS5).
type IndexJob struct {
	Key    *proto.QuestionsState
	RoomID string
	Floor  int64
}

// IndexAtHead is the builder's report that the index is at the head of the room: the index
// whose last folded event is last, which the team takes as its own.
func (t *Team) IndexAtHead(_ *proto.QuestionIndex, _ string) {}

// QuestionsStatus is the questions room as this member stands to it now (QS2, QS8), for the
// verbs and `status`.
func (t *Team) QuestionsStatus() team.QuestionsHealth { return team.QuestionsHealth{} }
