package engine

import (
	"context"
	"maps"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/hooktext"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// HookView is what the link answers the hooks from (HK1, HK2), read on the state goroutine
// from memory alone, so no hook op waits on the network. Live is delivery live and not
// stalled (15a major 4); Waiting is a wait request running now; Desired is the desired
// membership's last read, and Refused the adds the keeper refused.
type HookView struct {
	Teams                  []HookTeam
	Delivery               string
	Live, Stalled, Waiting bool
	Desired                state.ReportDesired
	Refused                []paths.TeamID
}

// HookTeam is one held team for the hooks: the session's handle and role in it, the
// coordinator's handle and the description ("" for none), whether those are current (as
// TeamRole's), the pending items the socket will not push (NotPushed, by event id, and their
// Counts by kind), how many of them are held as missing, and the team's health.
type HookTeam struct {
	Team                                   paths.TeamID
	Handle, Role, Coordinator, Description string
	Counts                                 hooktext.Counts
	NotPushed                              []string
	Missing                                int64
	Failed                                 *string
	Down                                   bool
	Current                                bool
	// Uncounted are the own questions posts not counted whose hook line is owed (QX3), oldest
	// first, after UncountedUnknown more whose numbers this link does not know (a restart).
	Uncounted        []proto.Dropped
	UncountedUnknown int64
}

// TeamRole is one held team as the membership notice names it (DV6); Role is "" until the
// team's verification has told it. Current is whether Role and Coordinator come from a team
// record verified current (issue 192): Health.Verified, a verification of a team not stopped
// since. A team that cannot reach its server keeps its last verification current; a stopped
// one (untrusted, its token refused), and one restarted after a stop and not verified again,
// does not, whatever Health still holds. Down is whether the team cannot reach its server: the
// notice ignores it for a team already told, so a network blip makes no noise, but a team the
// frame called not known yet is told only once its server answers.
type TeamRole struct {
	Team                                   paths.TeamID
	Role, Handle, Coordinator, Description string
	Current, Down                          bool
}

// hookState is what the engine keeps for the hooks: the desired membership's last read, the
// held teams last told to OnTeams, and how many wait requests run.
type hookState struct {
	desired   state.ReportDesired
	teams     []TeamRole
	teamsTold bool
}

// HookView is the hooks' view of the session now.
func (e *Engine) HookView(ctx context.Context) (HookView, error) {
	var v HookView
	err := e.do(ctx, func() { v = e.hookViewLocked() })
	return v, err
}

func (e *Engine) hookViewLocked() HookView {
	v := HookView{
		Delivery: e.cfg.Delivery, Stalled: e.stall.stalled, Waiting: e.waits.Load() > 0,
		Desired: e.hooks.desired, Refused: slices.SortedFunc(maps.Keys(e.refused), compareTeams),
	}
	if v.Desired.State == "" {
		v.Desired.State = state.DesiredApplied
	}
	v.Live = e.liveLocked() && !v.Stalled
	for _, id := range e.order() {
		h := e.teams[id]
		if h.removed {
			continue
		}
		v.Teams = append(v.Teams, hookTeam(h, v.Live))
	}
	return v
}

// hookTeam is h for the hooks; live is whether the socket pushes what is not held.
func hookTeam(h *held, live bool) HookTeam {
	r := teamRole(h)
	ht := HookTeam{
		Team: h.id, Handle: r.Handle, Role: r.Role, Coordinator: r.Coordinator, Description: r.Description, Current: r.Current,
		Down: h.health.Down,
	}
	if f, ok := failures[h.health.Stopped]; ok {
		ht.Failed = &f
	}
	if h.store == nil {
		return ht
	}
	ht.Uncounted, ht.UncountedUnknown = h.owedUncounted()
	for _, p := range h.store.Pending {
		if p.Handed != nil || !p.Held && live {
			continue
		}
		ht.NotPushed = append(ht.NotPushed, p.ID)
		count(&ht.Counts, p.Item)
		if p.Held {
			ht.Missing++
		}
	}
	return ht
}

// owedUncounted are the uncounted posts whose line is owed (the store's count above `said`): the
// newest of them this link folded, by number, and how many older ones it holds no number for.
func (h *held) owedUncounted() ([]proto.Dropped, int64) {
	u := h.store.Outbox.QuestionsUncounted
	owed := u.Count - u.Said
	known := h.uncounted[max(0, int64(len(h.uncounted))-owed):]
	return slices.Clone(known), owed - int64(len(known))
}

// UncountedSaid records n owed uncounted lines of team id told (QX3): `said` raised in one store
// write, and the numbers no longer owed forgotten, so a restart neither repeats nor loses one.
func (e *Engine) UncountedSaid(ctx context.Context, id paths.TeamID, n int64) error {
	var out error
	err := e.do(ctx, func() {
		out = e.changeLocked(id, func(_ *held, s *state.Store) (bool, error) {
			u := &s.Outbox.QuestionsUncounted
			u.Said = min(u.Count, u.Said+n)
			return true, nil
		})
		if out != nil {
			return
		}
		h := e.teams[id]
		u := h.store.Outbox.QuestionsUncounted
		h.uncounted = h.uncounted[max(0, int64(len(h.uncounted))-(u.Count-u.Said)):]
	})
	if err != nil {
		return err
	}
	return out
}

// count adds one item of kind to c.
func count(c *hooktext.Counts, kind string) {
	c.Total++
	switch kind {
	case records.KindHuman:
		c.Humans++
	case records.KindPeer:
		c.Agents++
	case records.KindPing:
		c.Pings++
	case records.KindNotice:
		c.Notices++
	case records.KindTimeout:
		c.Timeouts++
	}
}

func teamRole(h *held) TeamRole {
	r := TeamRole{Team: h.id, Role: h.health.Role, Handle: h.handle(), Down: h.health.Down, Current: h.health.Verified && h.health.Role != "" && h.health.Stopped == ""}
	if h.health.Coordinator != nil {
		r.Coordinator = *h.health.Coordinator
	}
	if h.health.Description != nil {
		r.Description = *h.health.Description
	}
	return r
}

// tellTeams tells OnTeams the held teams when they differ from the last told; on the state
// goroutine, from publish.
func (e *Engine) tellTeams() {
	if e.cfg.OnTeams == nil {
		return
	}
	teams := []TeamRole{}
	for _, id := range e.order() {
		if h := e.teams[id]; !h.removed {
			teams = append(teams, teamRole(h))
		}
	}
	if e.hooks.teamsTold && slices.Equal(teams, e.hooks.teams) {
		return
	}
	e.hooks.teams, e.hooks.teamsTold = teams, true
	e.cfg.OnTeams(slices.Clone(teams))
}

// SetUnusable takes session.json's hook-stdin count (15a major 3) and its last unusable hook
// input (issue 394), which the link records, for status.json and status; the render count is
// the engine's own and is not taken.
func (e *Engine) SetUnusable(ctx context.Context, u state.Unusable) error {
	return e.do(ctx, func() {
		e.unusable.HookStdin, e.unusable.HookLast = u.HookStdin, u.HookLast
		e.publish()
	})
}

// runningWait counts a wait request until the func it returns is called.
func (e *Engine) runningWait() func() {
	e.waits.Add(1)
	return func() { e.waits.Add(-1) }
}
