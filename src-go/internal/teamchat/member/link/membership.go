package link

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/hooktext"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// membership is DV6's memory: the held teams the engine last told, the farewell of each team
// removed this link's life, and the desired generation. sending is held while a notice is
// made and its told written; owed is a notice the socket did not take, for the next prompt.
type membership struct {
	mu         sync.Mutex
	teams      []engine.TeamRole
	known      bool
	farewells  map[paths.TeamID]string
	generation int64
	wake       chan struct{}

	sending sync.Mutex
	owed    atomic.Bool
}

// onTeams is the engine's held teams, on its state goroutine: kept for announceTeams, so the
// goroutine never waits on the socket.
func (l *link) onTeams(teams []engine.TeamRole) {
	l.mem.mu.Lock()
	l.mem.teams, l.mem.known = teams, true
	l.mem.mu.Unlock()
	select {
	case l.mem.wake <- struct{}{}:
	default:
	}
}

// noteFarewell keeps why t is being removed, for its left line.
func (l *link) noteFarewell(t paths.TeamID, farewell *string) {
	reason := proto.FarewellNone
	if farewell != nil {
		reason = *farewell
	}
	l.mem.mu.Lock()
	defer l.mem.mu.Unlock()
	if l.mem.farewells == nil {
		l.mem.farewells = map[paths.TeamID]string{}
	}
	l.mem.farewells[t] = reason
}

func (l *link) setGeneration(g int64) {
	l.mem.mu.Lock()
	defer l.mem.mu.Unlock()
	l.mem.generation = g
}

// snapshot is the teams, whether the engine has told any, the farewells and the generation.
func (m *membership) snapshot() ([]engine.TeamRole, bool, map[paths.TeamID]string, int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.teams, m.known, maps.Clone(m.farewells), m.generation
}

// announceTeams announces each change of the held teams, until the link ends.
func (l *link) announceTeams(ctx context.Context) {
	for {
		select {
		case <-l.quit:
			return
		case <-ctx.Done():
			return
		case <-l.mem.wake:
		}
		l.announce(ctx)
	}
}

// change is a diff of the held teams against told, ready to tell.
type change struct {
	notice proto.Membership
	told   state.Told
	// tell is whether there is a notice to give; quiet whether told changes without one.
	tell, quiet bool
}

// changeNow is the change from what the session was told to the teams now; none before the
// session's frame was told, which says it all, or before the engine told the teams.
func (l *link) changeNow() change {
	f := l.session.File()
	teams, known, farewells, gen := l.mem.snapshot()
	if !f.FrameTold || !known {
		return change{}
	}
	m, told, tell := diffMembership(f.Told, teams, farewells)
	told.Generation = gen
	same := maps.Equal(told.Roles, f.Told.Roles) && maps.Equal(told.Coordinators, f.Told.Coordinators)
	return change{notice: m, told: told, tell: tell, quiet: !tell && !same}
}

// announce is one DV6 send: the change told on the socket and recorded once written; a notice
// the socket did not take is owed to the next prompt; one that cannot render never can, so it
// is logged and recorded as told.
func (l *link) announce(ctx context.Context) {
	l.mem.sending.Lock()
	defer l.mem.sending.Unlock()
	c := l.changeNow()
	if !c.tell {
		if c.quiet {
			l.recordTold(c)
		}
		return
	}
	written, err := l.eng.SendMembership(ctx, c.notice)
	if se, ok := errors.AsType[*proto.SlotError](err); ok {
		l.logf("link: a membership notice cannot be told (%v); recorded as told", se)
		l.recordTold(c)
		return
	}
	if err != nil || !written {
		l.logErr("send a membership notice", err)
		l.mem.owed.Store(true)
		return
	}
	l.recordTold(c)
}

// owedMembership is the prompt's notice (the B30 fallback): the change owed, rendered with a
// fresh nonce and recorded as told, as the prompt's lines; none while a send is under way.
func (l *link) owedMembership() []string {
	if !l.mem.owed.Load() || !l.mem.sending.TryLock() {
		return nil
	}
	defer l.mem.sending.Unlock()
	c := l.changeNow()
	if !c.tell {
		l.recordTold(c)
		return nil
	}
	nonce, err := newNonce()
	if err != nil {
		l.logErr("render the owed membership notice", err)
		return nil
	}
	text, err := c.notice.Render(nonce)
	if err != nil {
		l.logf("link: a membership notice cannot be told (%v); recorded as told", err)
		l.recordTold(c)
		return nil
	}
	l.recordTold(c)
	return []string{hooktext.Membership(strings.Split(text, "\n"))}
}

// recordTold writes c's told into session.json; once written nothing is owed, and the
// farewells told are forgotten.
func (l *link) recordTold(c change) {
	err := l.session.Update(func(n *state.Session) bool {
		n.Told = c.told
		return true
	})
	if err != nil {
		l.logErr("record the membership told", err)
		return
	}
	l.mem.owed.Store(false)
	l.mem.mu.Lock()
	for _, left := range c.notice.Left {
		if id, err := paths.ParseTeamID(left.Team); err == nil {
			delete(l.mem.farewells, id)
		}
	}
	l.mem.mu.Unlock()
}

// framedCurrent is whether the frame may state t's role and coordinator: its record was
// verified current and its server answers. A team that cannot reach its server stays current
// for the membership notice, so a blip in the network makes no noise, but the frame is a
// definite claim and the verification it would rest on may predate a handover (issue 192).
func framedCurrent(t engine.HookTeam) bool { return t.Current && !t.Down }

// tellFrame records SessionStart's frame: frame_told, and told as the roles and a worker's
// coordinator the frame stated, from teams it could state them for; a team the frame said is
// not known yet is told as not known, so its role and coordinator are announced once verified,
// as what changed for a team the session has, never as a join. Nothing is owed after it.
func (l *link) tellFrame(v engine.HookView) error {
	l.mem.mu.Lock()
	told := newTold(l.mem.generation)
	l.mem.mu.Unlock()
	for _, t := range v.Teams {
		if !framedCurrent(t) || t.Role == "" {
			told.NotKnown = append(told.NotKnown, t.Team)
			continue
		}
		told.Roles[t.Team] = t.Role
		if t.Role == proto.RoleWorker {
			told.Coordinators[t.Team] = t.Coordinator
		}
	}
	err := l.session.Update(func(n *state.Session) bool {
		n.FrameTold = n.Started()
		n.Told = told
		return true
	})
	if err == nil {
		l.mem.owed.Store(false)
	}
	return err
}

// newTold is an empty told at generation gen.
func newTold(gen int64) state.Told {
	return state.Told{Generation: gen, Roles: map[paths.TeamID]string{}, Coordinators: map[paths.TeamID]string{}, NotKnown: []paths.TeamID{}}
}

// keepTold copies team t's told role and coordinator, or its being told as not known, from
// told into next.
func keepTold(next, told state.Told, t paths.TeamID) {
	if slices.Contains(told.NotKnown, t) {
		next.NotKnown = append(next.NotKnown, t)
		return
	}
	next.Roles[t] = told.Roles[t]
	if c, ok := told.Coordinators[t]; ok {
		next.Coordinators[t] = c
	}
}

// diffMembership is the notice from told to the held teams, the told to record (its
// generation the caller's), and whether there is a notice. A held team whose record is current
// gives its role, and a worker's its coordinator; any other held team keeps what was told, so
// nothing is announced from a record not verified current (issue 192). A team the frame called
// not known yet is the session's already: once its record is current and its server answers it
// is told as a role (and a worker's coordinator), never as a join. A worker whose role is
// unchanged is told its coordinator when it differs from the one told, or none was told. A
// team told and no longer held has left when a remove was seen for it (with farewells'
// reason); else it stays told, as an add that has not come back yet. A probe's role is
// recorded quietly: the notice has no probe line.
func diffMembership(told state.Told, teams []engine.TeamRole, farewells map[paths.TeamID]string) (proto.Membership, state.Told, bool) {
	var m proto.Membership
	next := newTold(0)
	held := map[paths.TeamID]bool{}
	for _, t := range teams {
		held[t.Team] = true
		_, wasTold := told.Roles[t.Team]
		framed := slices.Contains(told.NotKnown, t.Team)
		switch {
		case !t.Current || t.Role == "" || framed && t.Down:
			if wasTold || framed {
				keepTold(next, told, t.Team)
				m.Teams = append(m.Teams, t.Team.String())
			}
		default:
			m.Teams = append(m.Teams, t.Team.String())
			tellTeam(&m, next, told, t, wasTold || framed)
		}
	}
	m.Left = diffGone(next, told, held, farewells)
	return m, next, len(m.Left)+len(m.Joined)+len(m.Roles)+len(m.Coordinators) > 0
}

// tellTeam records the role of t, a team whose record is current, in next and puts it in the
// notice under the line it belongs to: a team the session has (inSession: told a role, or
// framed as not known yet) gets a role line when its role is not the one told, or a
// coordinator line when only a worker's coordinator differs; any other team is joined.
func tellTeam(m *proto.Membership, next, told state.Told, t engine.TeamRole, inSession bool) {
	role := t.Role
	next.Roles[t.Team] = role
	mt := proto.MembershipTeam{Team: t.Team.String(), Handle: t.Handle, Role: role, Description: t.Description, Current: true}
	if role == proto.RoleWorker {
		mt.Coordinator = t.Coordinator
		next.Coordinators[t.Team] = t.Coordinator
	}
	switch {
	case role == proto.RoleProbe:
	case inSession && told.Roles[t.Team] == role:
		if c, ok := told.Coordinators[t.Team]; role == proto.RoleWorker && (!ok || c != t.Coordinator) {
			m.Coordinators = append(m.Coordinators, mt)
		}
	case inSession:
		m.Roles = append(m.Roles, mt)
	default:
		m.Joined = append(m.Joined, mt)
	}
}

// diffGone is the told teams not held now that left (a remove seen, with its reason); every
// other told team not held stays told in next.
func diffGone(next, told state.Told, held map[paths.TeamID]bool, farewells map[paths.TeamID]string) []proto.MembershipLeft {
	var left []proto.MembershipLeft
	gone := append(slices.Collect(maps.Keys(told.Roles)), told.NotKnown...)
	slices.SortFunc(gone, func(a, b paths.TeamID) int { return strings.Compare(a.String(), b.String()) })
	for _, t := range gone {
		if held[t] {
			continue
		}
		reason, removed := farewells[t]
		if !removed {
			keepTold(next, told, t)
			continue
		}
		left = append(left, proto.MembershipLeft{Team: t.String(), Reason: reason})
	}
	return left
}

// newNonce is a delivery nonce, 8 hex digits from crypto/rand.
func newNonce() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("link: the nonce: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
