package engine

import (
	"context"
	"maps"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/hooktext"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
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
// coordinator's handle and the description ("" for none), the pending items the socket will
// not push (NotPushed, by event id, and their Counts by kind), how many of them are held as
// missing, and the team's health.
type HookTeam struct {
	Team                                   paths.TeamID
	Handle, Role, Coordinator, Description string
	Counts                                 hooktext.Counts
	NotPushed                              []string
	Missing                                int64
	Down                                   bool
	Failed                                 *string
}

// TeamRole is one held team as the membership notice names it (DV6); Role is "" until the
// team's verification has told it.
type TeamRole struct {
	Team                                   paths.TeamID
	Role, Handle, Coordinator, Description string
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
	ht := HookTeam{Team: h.id, Handle: r.Handle, Role: r.Role, Coordinator: r.Coordinator, Description: r.Description, Down: h.health.Down}
	if f, ok := failures[h.health.Stopped]; ok {
		ht.Failed = &f
	}
	if h.store == nil {
		return ht
	}
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
	r := TeamRole{Team: h.id, Role: h.health.Role, Handle: h.handle()}
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

// SetUnusable takes session.json's hook-stdin count (15a major 3), which the link counts, for
// status.json and status; the render count is the engine's own and is not taken.
func (e *Engine) SetUnusable(ctx context.Context, u state.Unusable) error {
	return e.do(ctx, func() {
		e.unusable.HookStdin = u.HookStdin
		e.publish()
	})
}

// runningWait counts a wait request until the func it returns is called.
func (e *Engine) runningWait() func() {
	e.waits.Add(1)
	return func() { e.waits.Add(-1) }
}
