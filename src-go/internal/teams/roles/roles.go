// Package roles is the one place a member's role is changed, whatever kind of member it is
// (Plan 00203 Task 3.8): a ccy or cc session, an LXC container's agent, a host, docker or vm
// member. The question asked first is the caller's (seats.ChangeRole on a terminal, hot for
// `ccy-teams session role`, lxcadd for a takeover); what changes is decided here, and by kind
// only at the last step:
//
//   - every member that has a handle has its role set in the team's registry at once, by
//     `agent-bus set-role` as root, so the team's record, which routes messages, changes live;
//   - a member whose role is declared, an agent_bus_lxc_members entry, also has its entry set
//     (`declare lxc-role`), first, so the next apply keeps the role instead of putting the old
//     one back. The entry an LXC handle belongs to is read from each candidate entry's
//     checkout, as the play derives a handle from it; an entry that cannot be read stops the
//     change before anything is changed.
//
// A step that fails puts back every step before it. Change applies the team when an entry was
// written, through the same ops.ApplyTeams every flow uses, and holds the play lock
// (ops.Locked) around the entry, the live role and the apply together, so a wait for the lock
// that is stopped changes nothing.
package roles

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/lxcfreeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
)

// Reach is how a role change reaches the member, the same for every kind: the team's record
// changes at once, and pingbus tells a running session, of any launcher, what it now is.
const Reach = "Who is copied on what is sent changes at once. A running session of it is told its new role " +
	"by its watcher at once, or else at its next prompt; no restart is needed."

// rootReader is the declare.py script the host runs as root to read a container's checkout.
const rootReader = "root-read"

// readTimeout bounds one read of a container's checkout.
const readTimeout = 20 * time.Second

// lxcKind is the type word of an LXC container's agent's handle.
const lxcKind = "lxc"

// Reader reads a container's checkout from the host's side; *lxcfreeze.Lister is the real one.
type Reader interface {
	ReadCheckout(ctx context.Context, script, mode, container, user, path string) (string, error)
}

// Setter changes roles. Every dependency is a field so a test stands in for each.
type Setter struct {
	Kit     *flow.Kit
	Declare *declare.Client
	// SetRole is `agent-bus set-role` as root.
	SetRole func(ctx context.Context, team, handle, role string) error
	Reader  Reader
	// Apply is ops.ApplyTeams: the play for the teams named.
	Apply func(ctx context.Context, teams ...string) error
	// Locked is ops.Locked: fn in one hold of the play lock, waited for first (re-entrant, so
	// Apply inside it is the same hold); nil runs fn alone.
	Locked      func(ctx context.Context, purpose string, fn func(context.Context) error) error
	ReadTimeout time.Duration
}

// New is the Setter over a host; apply is ops.ApplyTeams and locked ops.Locked.
func New(h *host.Host, kit *flow.Kit, apply func(ctx context.Context, teams ...string) error,
	locked func(ctx context.Context, purpose string, fn func(context.Context) error) error,
) *Setter {
	return &Setter{
		Kit: kit, Declare: h.Declare, SetRole: h.Bus.SetRole, Reader: &lxcfreeze.Lister{Sudo: h.Sudo, LookPath: h.LookPath},
		Apply: apply, Locked: locked, ReadTimeout: readTimeout,
	}
}

// Step is one member's role going From one role To another.
type Step struct {
	// Handle is the member's handle; "" for a declared entry whose member has not joined yet.
	Handle string
	// Entry is the member's declaration when the caller knows it; otherwise it is looked for
	// from Handle.
	Entry    *declare.LxcMember
	From, To string
	// Note is said after the live change, e.g. that it was the coordinator.
	Note string
}

// Done is what Set changed.
type Done struct {
	// Declared is whether an entry was written: the team is applied for it to agree.
	Declared bool
	undo     []func(context.Context) error
	// apply is the team's apply, which an undo of a written entry runs again, inside locked,
	// the play lock's hold, as Change made it.
	apply  func(context.Context) error
	locked func(context.Context, func(context.Context) error) error
}

// Undo puts back what was changed, the last first, and applies the team again when an entry
// was written (in one hold of the play lock, as the change was); the first failure stops it.
func (d Done) Undo(ctx context.Context) error {
	back := func(ctx context.Context) error {
		for _, step := range slices.Backward(d.undo) {
			if err := step(ctx); err != nil {
				return err
			}
		}
		if d.Declared && d.apply != nil {
			return d.apply(ctx)
		}
		return nil
	}
	if d.Declared && d.locked != nil {
		return d.locked(ctx, back)
	}
	return back(ctx)
}

// Change is Set, then the apply of team when an entry was written. A change that writes an
// entry happens in one hold of the play lock (ops.Locked), the entry, the live role and the
// apply all inside it: a wait for the lock that is stopped, or that reaches its bound, changes
// nothing. A change with no entry to write is live only and takes no lock. A failed apply leaves
// the roles changed, live and in the entries, which agree; the error says how to finish.
func (s *Setter) Change(ctx context.Context, team string, said func(string), steps ...Step) (Done, error) {
	resolved, err := s.resolve(ctx, team, steps)
	if err != nil {
		return Done{}, err
	}
	if !slices.ContainsFunc(resolved, func(st Step) bool { return st.Entry != nil }) {
		return s.run(ctx, team, said, resolved)
	}
	var done Done
	ran := false
	lockErr := s.locked(ctx, "change a role in team "+team, func(ctx context.Context) error {
		ran = true
		var runErr error
		if done, runErr = s.run(ctx, team, said, resolved); runErr != nil {
			return runErr
		}
		if applyErr := s.Apply(ctx, team); applyErr != nil {
			last := resolved[len(resolved)-1]
			done = Done{}
			return fmt.Errorf("%s is a %s now, live and in the host_vars, but the apply of %s that follows failed (%w): "+
				"once the cause is fixed, ccy-teams apply %s finishes it", who(last), last.To, team, applyErr, team)
		}
		return nil
	})
	if lockErr != nil && !ran {
		return Done{}, fmt.Errorf("%w, so no role was changed", lockErr)
	}
	if lockErr != nil {
		return Done{}, lockErr
	}
	done.apply = func(ctx context.Context) error { return s.Apply(ctx, team) }
	done.locked = func(ctx context.Context, fn func(context.Context) error) error {
		return s.locked(ctx, "put back a role in team "+team, fn)
	}
	return done, nil
}

// Set makes each step, in order, and says each live change; it applies nothing and takes no
// lock (a caller that writes the host_vars, as lxc-add does, holds the play lock around it).
// Sudo must be ready: the registry and a container's checkout are root's.
func (s *Setter) Set(ctx context.Context, team string, said func(string), steps ...Step) (Done, error) {
	resolved, err := s.resolve(ctx, team, steps)
	if err != nil {
		return Done{}, err
	}
	return s.run(ctx, team, said, resolved)
}

// locked is s.Locked, or fn alone where none is wired.
func (s *Setter) locked(ctx context.Context, purpose string, fn func(context.Context) error) error {
	if s.Locked == nil {
		return fn(ctx)
	}
	return s.Locked(ctx, purpose, fn)
}

// resolve finds each step's entry when the caller gave none: only reads, nothing changes.
func (s *Setter) resolve(ctx context.Context, team string, steps []Step) ([]Step, error) {
	found := &entries{s: s, team: team}
	resolved := make([]Step, len(steps))
	for i, st := range steps {
		if st.Entry == nil && st.Handle != "" {
			entry, declared, err := found.of(ctx, st.Handle)
			if err != nil {
				return nil, fmt.Errorf("%w, so no role was changed", err)
			}
			if declared {
				st.Entry = &entry
			}
		}
		resolved[i] = st
	}
	return resolved, nil
}

// run makes the resolved steps, putting back the earlier ones when one fails.
func (s *Setter) run(ctx context.Context, team string, said func(string), resolved []Step) (Done, error) {
	var done Done
	for _, st := range resolved {
		if err := s.step(ctx, team, st, said, &done); err != nil {
			if back := done.Undo(ctx); back != nil {
				return Done{}, fmt.Errorf("%w, and what was changed before it could not be put back (%w): "+
					"ccy-teams, the member's menu, sets each role", err, back)
			}
			return Done{}, err
		}
	}
	return done, nil
}

// step is one member's change: its entry first, then its live role.
func (s *Setter) step(ctx context.Context, team string, st Step, said func(string), done *Done) error {
	if st.Entry != nil {
		if err := s.declare(ctx, *st.Entry, st.To); err != nil {
			return err
		}
		entry := *st.Entry
		done.Declared = true
		done.undo = append(done.undo, func(ctx context.Context) error { return s.declare(ctx, entry, st.From) })
	}
	if st.Handle == "" {
		return nil
	}
	if err := s.SetRole(ctx, team, st.Handle, st.To); err != nil {
		s.Kit.ReportRootErr(err)
		return fmt.Errorf("%s was not made a %s", st.Handle, st.To)
	}
	said(st.Handle + " set to " + st.To + st.Note)
	done.undo = append(done.undo, func(ctx context.Context) error {
		if err := s.SetRole(ctx, team, st.Handle, st.From); err != nil {
			return fmt.Errorf("%s was not made a %s again: %w", st.Handle, st.From, err)
		}
		return nil
	})
	return nil
}

func (s *Setter) declare(ctx context.Context, m declare.LxcMember, role string) error {
	if _, err := s.Declare.LxcSetRole(ctx, m, role); err != nil {
		return fmt.Errorf("the entry of %s@%s was not made a %s: %w", m.User, m.Container, role, err)
	}
	return nil
}

// who names a step's member.
func who(st Step) string {
	if st.Handle != "" {
		return st.Handle
	}
	return st.Entry.User + "@" + st.Entry.Container
}

// entries finds the entry an LXC handle is, reading the declaration and the root reader once.
type entries struct {
	s       *Setter
	team    string
	members []declare.LxcMember
	read    bool
	script  string
}

// parsed is a handle in its parts: <repo>.<seat>+<host>.<kind>.
type parsed struct {
	repo, seat, host, kind string
}

func parseHandle(handle string) (parsed, bool) {
	repo, rest, ok1 := strings.Cut(handle, ".")
	seat, rest, ok2 := strings.Cut(rest, "+")
	i := strings.LastIndex(rest, ".")
	if !ok1 || !ok2 || i < 0 {
		return parsed{}, false
	}
	return parsed{repo: repo, seat: seat, host: rest[:i], kind: rest[i+1:]}, true
}

// IsHandle is whether word is a member's handle, <repo>.<seat>+<host>.<type>, of a type a team
// member has: podman (ccy), host (cc and host members), lxc, docker or vm.
func IsHandle(word string) bool {
	h, ok := parseHandle(word)
	return ok && h.repo != "" && h.seat != "" && h.host != "" && slices.Contains([]string{"podman", "host", lxcKind, "docker", "vm"}, h.kind)
}

// couldBe is whether m, by what the entry itself says, could be the member h of team.
func (h parsed) couldBe(team string, m declare.LxcMember) bool {
	return m.Team == team && m.State == "present" && m.Checkout != "" &&
		(m.Host == "" || m.Host == h.host) && (m.Seat == "" || m.Seat == h.seat)
}

// of is the declared entry that is handle, and whether there is one: none for a member of any
// kind but lxc, or an LXC agent joined by hand, whose role lives in the registry only. Only the
// present entries of the team that could give handle are read: each one's checkout, as the
// play derives the handle from it.
func (e *entries) of(ctx context.Context, handle string) (declare.LxcMember, bool, error) {
	h, ok := parseHandle(handle)
	if !ok || h.kind != lxcKind {
		return declare.LxcMember{}, false, nil
	}
	if err := e.load(ctx); err != nil {
		return declare.LxcMember{}, false, err
	}
	var matched []declare.LxcMember
	for _, m := range e.members {
		if !h.couldBe(e.team, m) {
			continue
		}
		identity, err := e.identity(ctx, m, handle)
		if err != nil {
			return declare.LxcMember{}, false, err
		}
		if identity.Repo == h.repo && orDerived(m.Host, identity.Host) == h.host {
			matched = append(matched, m)
		}
	}
	switch len(matched) {
	case 0:
		return declare.LxcMember{}, false, nil
	case 1:
		return matched[0], true, nil
	}
	names := make([]string, len(matched))
	for i, m := range matched {
		names[i] = m.User + "@" + m.Container
	}
	return declare.LxcMember{}, false, fmt.Errorf("%s could be the member of %s: set the role in its entry in the "+
		"host_vars (give each entry its seat), then ccy-teams apply %s", handle, strings.Join(names, " or "), e.team)
}

func orDerived(own, derived string) string {
	if own != "" {
		return own
	}
	return derived
}

func (e *entries) load(ctx context.Context) error {
	if e.read {
		return nil
	}
	members, err := e.s.Declare.LxcMembers(ctx)
	if err != nil {
		return fmt.Errorf("the declared LXC members could not be read (%w)", err)
	}
	e.members, e.read = members, true
	return nil
}

// identity is the repo and host of the handles m's checkout gives; handle is the member being
// looked for, which a failure names with the entry that blocked it.
func (e *entries) identity(ctx context.Context, m declare.LxcMember, handle string) (declare.Identity, error) {
	if e.script == "" {
		script, err := e.s.Declare.LxcScript(ctx, rootReader)
		if err != nil {
			return declare.Identity{}, err
		}
		e.script = script
	}
	bounded, cancel := context.WithTimeout(ctx, e.s.ReadTimeout)
	defer cancel()
	probe, err := e.s.Reader.ReadCheckout(bounded, e.script, "probe", m.Container, m.User, m.Checkout)
	if err != nil {
		advice := "Start container " + m.Container
		if m.Seat == "" {
			advice += ", or give each of team " + e.team + "'s entries its seat in the host_vars"
		}
		return declare.Identity{}, fmt.Errorf("the entry of %s@%s blocks the change of %s: its checkout is read to "+
			"tell whether it is %s, and that read failed (%w). %s", m.User, m.Container, handle, handle, err, advice)
	}
	identity, err := e.s.Declare.LxcIdentity(ctx, probe)
	if err != nil {
		return declare.Identity{}, fmt.Errorf("the entry of %s@%s blocks the change of %s: whether it is %s could not be "+
			"told from its checkout (%w)", m.User, m.Container, handle, handle, err)
	}
	return identity, nil
}
