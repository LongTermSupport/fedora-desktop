package system

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// AnswerLimit bounds Answers' probe of one team's homeserver: the team list waits at most this
// long for a bus that does not answer (issue 430).
const AnswerLimit = 5 * time.Second

// Answers is whether team's bus answers this checkout's pool seats (issue 430): "" when it does,
// else the words of why not, which the team list shows. It asks the homeserver that the first
// pool seat's own seat.json names, with the CA pinned to it, as that seat's member would reach it
// at its take (seat.VersionsReach: the versions, no token, nothing changed there), bounded by
// AnswerLimit on Timers. A team retired here whose server is gone, or whose bus is stopped,
// does not answer. A seat.json that cannot be read, or a caller that ended, is an error: never a
// team called live or dead on no evidence.
func (c *Cmd) Answers(ctx context.Context, team string) (string, error) {
	if c.Reach == nil || c.Timers == nil {
		return "", errors.New("whether a team's bus answers cannot be judged: no probe or timers are wired")
	}
	id, err := paths.ParseTeamID(team)
	if err != nil {
		return "", fmt.Errorf("team %s: %w", team, err)
	}
	store, key, _, err := Here(c.Getenv, c.Getwd)
	if err != nil {
		return "", err
	}
	names, err := PoolSeats(store, key, id)
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "this checkout has no pool seat of it to reach the team with", nil
	}
	path := store.PoolSeat(key, id, names[0]).Bundle()
	data, found, err := state.ReadFile(path, seat.MaxBundle)
	if err == nil && !found {
		err = errors.New("there is none")
	}
	if err != nil {
		return "", fmt.Errorf("the seat.json of team %s's pool seat %s (%s): %w", team, names[0], path, err)
	}
	b, err := seat.DecodeBundle(data)
	if err != nil {
		return "", fmt.Errorf("the seat.json of team %s's pool seat %s (%s): %w", team, names[0], path, err)
	}
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := c.Timers.AfterFunc(AnswerLimit, cancel)
	defer stop()
	err = c.Reach.Reach(probeCtx, b)
	switch {
	case err == nil:
		return "", nil
	case ctx.Err() != nil:
		return "", fmt.Errorf("whether team %s's bus answers: %w", team, ctx.Err())
	case probeCtx.Err() != nil:
		return "its homeserver at " + b.BaseURL + " gave no answer within " + AnswerLimit.String(), nil
	}
	return "its homeserver at " + b.BaseURL + " does not answer: " + err.Error(), nil
}
