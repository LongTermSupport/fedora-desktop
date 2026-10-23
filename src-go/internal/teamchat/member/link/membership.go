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
	m, roles, tell := diffMembership(f.Told.Roles, teams, farewells)
	return change{notice: m, told: state.Told{Generation: gen, Roles: roles}, tell: tell, quiet: !tell && !maps.Equal(roles, f.Told.Roles)}
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

// tellFrame records SessionStart's frame: frame_told, and told as the teams the frame named;
// nothing is owed after it.
func (l *link) tellFrame(v engine.HookView) error {
	roles := map[paths.TeamID]string{}
	for _, t := range v.Teams {
		if t.Role != "" {
			roles[t.Team] = t.Role
		}
	}
	l.mem.mu.Lock()
	gen := l.mem.generation
	l.mem.mu.Unlock()
	err := l.session.Update(func(n *state.Session) bool {
		n.FrameTold = n.Started()
		n.Told = state.Told{Generation: gen, Roles: roles}
		return true
	})
	if err == nil {
		l.mem.owed.Store(false)
	}
	return err
}

// diffMembership is the notice from told to the held teams, the roles to record, and whether
// there is a notice. A held team's role is its known one, else its told one. A team told and
// no longer held has left when a remove was seen for it (with farewells' reason); else it
// stays told, as an add that has not come back yet. A probe's role is recorded quietly: the
// notice has no probe line.
func diffMembership(told map[paths.TeamID]string, teams []engine.TeamRole, farewells map[paths.TeamID]string) (proto.Membership, map[paths.TeamID]string, bool) {
	var m proto.Membership
	roles := map[paths.TeamID]string{}
	held := map[paths.TeamID]bool{}
	for _, t := range teams {
		held[t.Team] = true
		was, wasTold := told[t.Team]
		role := t.Role
		if role == "" {
			if wasTold {
				roles[t.Team] = was
				m.Teams = append(m.Teams, t.Team.String())
			}
			continue
		}
		roles[t.Team] = role
		m.Teams = append(m.Teams, t.Team.String())
		if role == proto.RoleProbe || wasTold && was == role {
			continue
		}
		mt := proto.MembershipTeam{Team: t.Team.String(), Handle: t.Handle, Role: role, Description: t.Description}
		if role == proto.RoleWorker {
			mt.Coordinator = t.Coordinator
		}
		if wasTold {
			m.Roles = append(m.Roles, mt)
		} else {
			m.Joined = append(m.Joined, mt)
		}
	}
	gone := slices.SortedFunc(maps.Keys(told), func(a, b paths.TeamID) int { return strings.Compare(a.String(), b.String()) })
	for _, t := range gone {
		if held[t] {
			continue
		}
		reason, removed := farewells[t]
		if !removed {
			roles[t] = told[t]
			continue
		}
		m.Left = append(m.Left, proto.MembershipLeft{Team: t.String(), Reason: reason})
	}
	return m, roles, len(m.Left)+len(m.Joined)+len(m.Roles) > 0
}

// newNonce is a delivery nonce, 8 hex digits from crypto/rand.
func newNonce() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("link: the nonce: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
