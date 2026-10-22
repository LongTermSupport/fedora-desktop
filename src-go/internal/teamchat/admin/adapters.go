package admin

import (
	"context"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// ChildViewer is the health tick's health.Viewer (CO4): the child's team.view over the applied
// team.json root reads under Root.
type ChildViewer struct {
	Child Child
	Root  string
}

// View implements health.Viewer: team.json missing or unreadable, or a config answer, wraps
// health.ErrViewFiles; a child that failed or broke the rules, or another failure,
// health.ErrViewChild; an unreachable answer matrix.ErrUnreachable.
func (v ChildViewer) View(ctx context.Context, team paths.TeamID) (health.View, error) {
	_, tf, err := readTeamFile(v.Root, team)
	if err != nil {
		return health.View{}, fmt.Errorf("%w: %w", health.ErrViewFiles, err)
	}
	a, err := v.Child.Ask(ctx, Request{Op: OpTeamView, Team: team, Args: proto.Obj(proto.M(keyTeamFile, tf))})
	if err == nil {
		err = answerFits(OpTeamView, a)
	}
	if err != nil {
		return health.View{}, fmt.Errorf("%w: %w", health.ErrViewChild, err)
	}
	if !a.OK {
		return health.View{}, viewFailure(a.Error)
	}
	return *a.View, nil
}

func viewFailure(e *AnswerError) error {
	switch e.Class {
	case ClassConfig:
		return fmt.Errorf("%w: %s", health.ErrViewFiles, e.Message)
	case ClassUnreachable:
		return fmt.Errorf("%w: %s", matrix.ErrUnreachable, e.Message)
	case ClassFailed, ClassRefused:
	}
	return fmt.Errorf("%w: %s", health.ErrViewChild, e.Message)
}

// ChildAnnouncer is the health tick's health.Announcer (CO4): the child's member.disconnected
// over the applied team.json root reads under Root.
type ChildAnnouncer struct {
	Child Child
	Root  string
}

// Disconnected implements health.Announcer: any failure is the error the tick retries; a
// notice not posted is the Notice with its why.
func (n ChildAnnouncer) Disconnected(ctx context.Context, team paths.TeamID, handle string, run paths.RunID, sinceMS int64) (health.Notice, error) {
	_, tf, err := readTeamFile(n.Root, team)
	if err != nil {
		return health.Notice{}, err
	}
	args := proto.Obj(
		proto.M(keyTeamFile, tf), proto.M(keyHandle, proto.Str(handle)), proto.M(keyRun, proto.Str(run.String())),
		proto.M(keySinceMS, proto.Int(sinceMS)),
	)
	a, err := n.Child.Ask(ctx, Request{Op: OpMemberDisconnected, Team: team, Args: args})
	if err == nil {
		err = answerFits(OpMemberDisconnected, a)
	}
	if err != nil {
		return health.Notice{}, fmt.Errorf("the %s child: %w", paths.CtlUser, err)
	}
	if !a.OK {
		return health.Notice{}, &ChildError{Class: a.Error.Class, Message: a.Error.Message}
	}
	return *a.Notice, nil
}
