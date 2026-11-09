package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// SaveRoster keeps the roster teamsync verified at rev in the seat's roster cache (n3d-D3):
// roster.json is written on the state goroutine only when rev is not the one held, and the
// roster is held for composing sends before the next link's first verification. A roster that
// does not decode for this team at rev is refused and written nowhere.
func (e *Engine) SaveRoster(ctx context.Context, t paths.TeamID, rev int64, content json.RawMessage) error {
	var out error
	if err := e.do(ctx, func() { out = e.saveRoster(t, rev, content) }); err != nil {
		return err
	}
	return out
}

func (e *Engine) saveRoster(t paths.TeamID, rev int64, content json.RawMessage) error {
	h, err := e.heldFor(t)
	if err != nil {
		return err
	}
	if h.roster != nil && h.rosterRev == rev {
		return nil
	}
	r, err := decodeCached(h, rev, content)
	if err != nil {
		return fmt.Errorf("engine: team %s: the roster cache: %w", t, err)
	}
	if err := state.Write(e.cfg.Root, h.lock.Seat().RosterCache(), state.RosterCache{Rev: rev, Content: content}); err != nil {
		return fmt.Errorf("engine: team %s: the roster cache: %w", t, err)
	}
	h.roster, h.rosterRev = &r, rev
	return nil
}

// loadRoster is Add's read of the roster cache: a missing file, one that does not decode, or a
// roster that is not this team's at its rev is no cache, each logged. A stale cache is only a
// composing aid: every receiver checks the addressing again, and the first verification
// replaces it.
func (e *Engine) loadRoster(h *held) {
	c, err := state.Read(e.cfg.Root, h.lock.Seat().RosterCache(), state.MaxRosterCache, state.DecodeRosterCache)
	if errors.Is(err, fs.ErrNotExist) {
		e.logf("team %s: no roster cache; sends wait for the first verification", h.id)
		return
	}
	var r proto.Roster
	if err == nil {
		r, err = decodeCached(h, c.Rev, c.Content)
	}
	if err != nil {
		e.logf("team %s: the roster cache is not used: %v", h.id, err)
		return
	}
	h.roster, h.rosterRev = &r, c.Rev
}

// decodeCached is content as h's team roster at rev, by proto's roster decoder against
// seat.json's anchors.
func decodeCached(h *held, rev int64, content json.RawMessage) (proto.Roster, error) {
	if h.anchors.MainRoom == "" {
		return proto.Roster{}, errors.New("seat.json was not read")
	}
	v, err := proto.Parse(content)
	if err != nil {
		return proto.Roster{}, err
	}
	r, err := proto.DecodeRoster(v, h.anchors.Server, h.anchors.Team, h.anchors.MainRoom)
	if err != nil {
		return proto.Roster{}, err
	}
	if r.Rev != rev {
		return proto.Roster{}, fmt.Errorf("the roster is rev %d, not %d", r.Rev, rev)
	}
	return r, nil
}

// removeRoster unlinks the seat's roster cache with its store, so a re-added seat never starts
// from a removed team's roster; none is not an error.
func (e *Engine) removeRoster(h *held) error {
	err := e.cfg.Root.Unlink(h.lock.Seat().RosterCache())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
