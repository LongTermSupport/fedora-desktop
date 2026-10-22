// Package wiring is the member binary's assembly (Plan 00209 N3, n3c W): the verbs built
// outside cli (n3c-D5), and the link's seams joined, the team goroutine (teamsync) and the
// session's ends (ends) over the engine. cmd/teamchat passes Verbs to cli.Run.
package wiring

import (
	"context"
	"errors"
	"sync"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/client"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/ends"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/keeper"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/link"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/teamsync"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// clientVerbs are the link.sock verbs, each client.Main's.
var clientVerbs = []string{"recv", "wait", "inbox", "status", "verify", "history"}

// Verbs are the verbs cmd/teamchat passes in cli.Env.Verbs: every name of cli.Outside.
func Verbs() map[string]cli.RunFunc {
	v := map[string]cli.RunFunc{
		"run":               keeper.Main,
		"link":              link.Main(link.Wiring{NewTeam: NewTeam, NewEnds: NewEnds}),
		"seat disconnected": client.SeatDisconnected,
		"reach":             client.Reach,
	}
	for _, name := range clientVerbs {
		v[name] = client.Main(name)
	}
	return v
}

// NewEnds is the session's ends over the engine, for this link's run.
func NewEnds(c engine.EndsConfig) team.Ends {
	return ends.New(ends.Config{Engine: c.Engine, Run: c.Run, Clock: c.Clock, Log: c.Log})
}

// NewTeam is a held team's goroutine: teamsync's, with its seat directory watched while it
// runs (LN9). A team made only for its notices (an owed leave) is never run, so it holds no
// watch.
func NewTeam(c engine.TeamConfig) (team.Team, error) {
	ch := &seatChanges{}
	t, err := teamsync.New(teamsync.Config{
		Team: c.Team, Seat: c.Seat, Root: c.Root, Run: c.Run, Engine: c.Engine, Ends: c.Ends,
		Clock: c.Clock, Watcher: ch, RemovedUserIDs: c.RemovedUserIDs, Plain: c.Plain,
		FirstSessionStart: c.FirstSessionStart, Log: c.Log,
	})
	if err != nil {
		return nil, err
	}
	return &watched{Team: t, seat: c.Seat, changes: ch}, nil
}

// watched is a team goroutine whose seat watch lives as long as its Run.
type watched struct {
	*teamsync.Team

	seat    paths.Seat
	changes *seatChanges
}

// Run watches the seat directory, which an add holds, and runs the team; a seat that cannot
// be watched is the team's stop.
func (w *watched) Run(ctx context.Context) error {
	sw, err := teamsync.WatchSeat(w.seat)
	if err != nil {
		return err
	}
	w.changes.set(sw)
	runErr := w.Team.Run(ctx)
	w.changes.set(nil)
	return errors.Join(runErr, sw.Close())
}

// seatChanges is the team's Watcher: the running watch's changes, none outside Run.
type seatChanges struct {
	mu sync.Mutex
	w  *teamsync.SeatWatcher
}

// Changes is nil (never ready) while no watch runs.
func (s *seatChanges) Changes() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		return nil
	}
	return s.w.Changes()
}

func (s *seatChanges) set(w *teamsync.SeatWatcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.w = w
}
