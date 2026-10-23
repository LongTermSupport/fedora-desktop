package engine

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/colour"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// The trust a team record shows: main verified (the roster is held), main refused, or not yet
// verified. B 2.2.2 names the key, not its set.
const (
	trustTrusted    = "trusted"
	trustUntrusted  = "untrusted"
	trustUnverified = "unverified"
)

// The states a room record shows: the roster names the room, or not yet.
const (
	roomJoined = "joined"
	roomAbsent = "absent"
)

// failures maps the exits code that stopped a team to status.json's closed set; a stop the set
// has no word for (a refused store) shows only in status and the keeper's report.
var failures = map[string]string{
	exits.CodeToken:     state.FailedUnauthorized,
	exits.CodeForbidden: state.FailedForbidden,
	exits.CodeUntrusted: state.FailedTrust,
	exits.CodeBundle:    state.FailedBundle,
}

func compareTeams(a, b paths.TeamID) int { return strings.Compare(a.String(), b.String()) }

// order is the held teams by id, so every listing is stable.
func (e *Engine) order() []paths.TeamID {
	return slices.SortedFunc(maps.Keys(e.teams), compareTeams)
}

// handle is h's handle, "" while seat.json was never read.
func (h *held) handle() string {
	hd, ok := proto.AgentHandle(h.userID, paths.ServerName(h.id))
	if !ok {
		return ""
	}
	return hd.String()
}

// member is h's short member name for the status line (B27), nil while seat.json was never
// read.
func (h *held) member() *string {
	hd, ok := proto.AgentHandle(h.userID, paths.ServerName(h.id))
	if !ok {
		return nil
	}
	short := hd.Short()
	return &short
}

// teamColour is the colour the status line draws team t in: the admin's override held in
// health, else the automatic colour of its name (B27, n3e-D2).
func teamColour(t paths.TeamID, override *string) string {
	if override != nil && colour.Stored(*override) {
		return *override
	}
	return colour.Auto(t.String())
}

// pending is how many items the store owes, those awaiting a delivery's proof not counted.
func (h *held) pending() int64 {
	if h.store == nil {
		return 0
	}
	awaiting := awaitingIDs(h.store)
	n := int64(0)
	for _, p := range h.store.Pending {
		if !awaiting[p.ID] {
			n++
		}
	}
	return n
}

// publish tells the keeper each held team's report that changed and offers status.json the
// newest status; on the state goroutine, after every change.
func (e *Engine) publish() {
	e.offerStatus(e.buildStatus())
	e.proofChanged()
}

// buildStatus is status.json's content as it is now, its written_ms left for the write, and
// tells the keeper each held team's report that changed.
func (e *Engine) buildStatus() state.Status {
	rows := []state.StatusTeam{}
	for _, id := range e.order() {
		h := e.teams[id]
		if h.removed {
			continue
		}
		rows = append(rows, e.statusRow(h))
		handle := h.handle()
		if handle == "" {
			continue
		}
		report := wire.State{Team: id, Handle: handle, Pending: h.pending(), Delivery: e.cfg.Delivery, Store: state.StoreOK}
		if h.health.LastSyncOKMS > 0 {
			ms := h.health.LastSyncOKMS
			report.LastSyncOKMS = &ms
		}
		if h.refused {
			report.Store = state.StoreRefused
		}
		if last, ok := e.sent[id]; !ok || !sameState(last, report) {
			e.sent[id] = report
			e.cfg.OnState(report)
		}
	}
	for _, id := range slices.SortedFunc(maps.Keys(e.refused), compareTeams) {
		rows = append(rows, state.StatusTeam{Team: id, Down: true, Delivery: state.DeliveryNone, Colour: teamColour(id, nil)})
	}
	return state.Status{Run: e.cfg.Run, Desired: e.desired, Teams: rows}
}

func sameState(a, b wire.State) bool {
	sync := (a.LastSyncOKMS == nil) == (b.LastSyncOKMS == nil) && (a.LastSyncOKMS == nil || *a.LastSyncOKMS == *b.LastSyncOKMS)
	a.LastSyncOKMS, b.LastSyncOKMS = nil, nil
	return sync && a == b
}

func (e *Engine) statusRow(h *held) state.StatusTeam {
	row := state.StatusTeam{Team: h.id, Down: h.health.Down, Delivery: e.cfg.Delivery, Pending: h.pending()}
	row.Stalled = e.stall.stalled && row.Pending > 0
	row.Member, row.Colour = h.member(), teamColour(h.id, h.health.Colour)
	if h.health.Role != "" {
		role := h.health.Role
		row.Role = &role
	}
	if f, ok := failures[h.health.Stopped]; ok {
		row.Failed = &f
	}
	row.Missing = h.missing()
	return row
}

// Refuse shows an add the keeper refused, its seat held by another run (RC1, n3c-D21), in
// status and status.json; the NOTICE is n3e's (n3c-D23).
func (e *Engine) Refuse(ctx context.Context, t paths.TeamID, holder paths.RunID) error {
	return e.do(ctx, func() {
		if _, ok := e.teams[t]; ok {
			return
		}
		e.refused[t] = holder
		e.logf("team %s: the add was refused, the seat is held by run %s", t, holder)
		e.publish()
	})
}

// SetDesired is the keeper's last read of the desired membership (RC1's link half).
func (e *Engine) SetDesired(ctx context.Context, d state.ReportDesired) error {
	return e.do(ctx, func() {
		e.desired = d.State
		e.publish()
	})
}

// teamView is one held team as status reads it, copied off the state goroutine.
type teamView struct {
	id      paths.TeamID
	tm      team.Team
	userID  string
	handle  string
	health  team.Health
	counts  team.Counts
	pending int64

	seenTwice int64
}

// status is LN5: per held team its team, member and room records, then the refused adds, then
// the runtime record. The roster is read off the state goroutine, from the team's memory.
func (e *Engine) status(ctx context.Context, only *paths.TeamID, out RecordWriter) wire.Done {
	var (
		views   []teamView
		refused []paths.TeamID
	)
	if err := e.do(ctx, func() { views, refused = e.snapshot(only) }); err != nil {
		return internal(err)
	}
	if only != nil && len(views) == 0 && len(refused) == 0 {
		return notHeld(*only)
	}
	var recs []records.Record
	for _, v := range views {
		recs = append(recs, teamRecords(v, e.cfg.Delivery)...)
	}
	for _, id := range refused {
		recs = append(recs, records.Team{Team: id, Name: id.String(), Trust: trustUnverified, Delivery: state.DeliveryNone, Down: true})
	}
	if rt, err := e.cfg.Runtime(); err != nil {
		e.logf("status: the runtime cost was not read: %v", err)
	} else {
		recs = append(recs, rt)
	}
	if err := writeAll(out, recs); err != nil {
		return internal(err)
	}
	if err := out.Flush(); err != nil {
		return internal(err)
	}
	return wire.Done{}
}

// snapshot is the held teams and refused adds status shows, on the state goroutine.
func (e *Engine) snapshot(only *paths.TeamID) ([]teamView, []paths.TeamID) {
	var (
		views   []teamView
		refused []paths.TeamID
	)
	for _, id := range e.order() {
		h := e.teams[id]
		if h.removed || only != nil && *only != id {
			continue
		}
		views = append(views, teamView{
			id: id, tm: h.team, userID: h.userID, handle: h.handle(), health: h.health,
			counts: h.counts, pending: h.pending(),
			seenTwice: h.seenTwice,
		})
	}
	for _, id := range slices.SortedFunc(maps.Keys(e.refused), compareTeams) {
		if only == nil || *only == id {
			refused = append(refused, id)
		}
	}
	return views, refused
}

func writeAll(out RecordWriter, rs []records.Record) error {
	for _, r := range rs {
		if err := out.Record(r); err != nil {
			return err
		}
	}
	return nil
}

func teamRecords(v teamView, delivery string) []records.Record {
	var (
		roster   proto.Roster
		verified bool
	)
	if v.tm != nil {
		roster, verified = v.tm.Notices().Roster()
	}
	tr := records.Team{
		Team: v.id, Name: v.id.String(), Handle: v.handle, Role: v.health.Role, Trust: trustUnverified,
		Delivery: delivery, Pending: v.pending, Down: v.health.Down || v.health.Stopped != "",
		Counts: records.TeamCounts{
			Ignored: v.counts.Ignored, Dropped: v.counts.Dropped, Unheard: v.counts.Unheard,
			Gaps: v.counts.Gaps, Redacted: v.counts.Redacted,
			SeenTwice: v.seenTwice,
		},
	}
	switch {
	case v.health.Stopped == exits.CodeUntrusted:
		tr.Trust = trustUntrusted
	case verified:
		tr.Trust = trustTrusted
		if roster.Name != "" {
			tr.Name = roster.Name
		}
		if m, ok := roster.Members[v.userID]; ok && tr.Role == "" {
			tr.Role = m.Role
		}
	}
	out := []records.Record{tr}
	if !verified {
		return out
	}
	server := paths.ServerName(v.id)
	for _, id := range slices.Sorted(maps.Keys(roster.Members)) {
		hd, ok := proto.AgentHandle(id, server)
		if !ok {
			continue
		}
		// Presence is not tracked here: only this member is known to be present.
		out = append(out, records.Member{Team: v.id, Handle: hd.String(), Role: roster.Members[id].Role, Present: id == v.userID})
	}
	out = append(out, records.Room{Team: v.id, Purpose: proto.PurposeMain, State: roomJoined})
	for _, r := range []struct{ purpose, id string }{
		{proto.PurposeQuestions, roster.Rooms.Questions}, {proto.PurposeReports, roster.Rooms.Reports},
	} {
		st := roomAbsent
		if r.id != "" {
			st = roomJoined
		}
		out = append(out, records.Room{Team: v.id, Purpose: r.purpose, State: st})
	}
	return out
}
