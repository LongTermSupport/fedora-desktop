package hot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/coordinator"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/roles"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// noUndo is the undo of a step that changed nothing.
func noUndo(context.Context) {}

// setRoleTo makes handle, a member of any kind, the team's want (worker or coordinator) through
// package roles (H25, H7), as seats.ChangeRole does: live by sudo agent-bus set-role, and in a
// declared member's entry, then applied. A coordinator while another member holds the role first
// makes that one a worker, only with demote (asked on a terminal). undo puts back what it
// changed, for an add that is rolled back.
func (x *Hot) setRoleTo(ctx context.Context, team, handle, want string, demote bool) (func(context.Context), term.ExitCode, bool) {
	roster, err := x.Host.Bus.List(ctx, team)
	if err != nil {
		x.mark(markFailed, "role: the members of "+team+" could not be read: "+lastLine(err))
		return noUndo, term.ExitFailure, false
	}
	member, found := roster.Member(handle)
	if !found {
		x.mark(markFailed, "role: "+handle+" is not a member of "+team)
		return noUndo, term.ExitFailure, false
	}
	if member.Role == want {
		x.mark(markSkip, "role: "+handle+" is a "+want+" already")
		return noUndo, term.ExitOK, true
	}
	holder, code, agreed := x.demoting(ctx, roster, team, handle, want, demote)
	if !agreed {
		return noUndo, code, false
	}
	if !x.Kit.SudoReady(ctx, "A role is changed as root, by agent-bus.") {
		x.mark(markFailed, "role: sudo is not ready, so no role was changed")
		return noUndo, term.ExitFailure, false
	}
	var steps []roles.Step
	if holder != "" {
		steps = append(steps, roles.Step{
			Handle: holder, From: agentbus.RoleCoordinator, To: agentbus.RoleWorker, Note: " (it was " + team + "'s coordinator)",
		})
	}
	steps = append(steps, roles.Step{Handle: handle, From: member.Role, To: want})
	done, err := x.Roles.Change(ctx, team, func(line string) { x.mark(markDone, "role: "+line) }, steps...)
	if err != nil {
		x.mark(markFailed, "role: "+lastLine(err))
		return noUndo, term.ExitFailure, false
	}
	undo := func(ctx context.Context) {
		if back := done.Undo(ctx); back != nil {
			x.mark(markFailed, "role: not put back ("+lastLine(back)+"): ccy-teams, the member's menu, sets it")
			return
		}
		x.mark(markDone, "role: put back as it was")
	}
	return undo, term.ExitOK, true
}

// demoting is the coordinator a new coordinator replaces ("" for none), with demote said or
// asked on a terminal; agreed is false when it is not to be demoted. The commands decide the
// takeover before anything changes (coordinatorPath); this is the guard for a coordinator that
// appeared since, and for the role handed on by a remove.
func (x *Hot) demoting(ctx context.Context, roster agentbus.Roster, team, handle, want string, demote bool) (string, term.ExitCode, bool) {
	current, held := coordinator.Of(roster, handle)
	if want != agentbus.RoleCoordinator || !held {
		return "", term.ExitOK, true
	}
	if demote {
		return current.Handle, term.ExitOK, true
	}
	h := coordinator.Holder{Handle: current.Handle, Running: coordinator.Unknown}
	if !x.interactive() {
		return "", x.usageError(coordinator.Refusal(team, handle, h, optDemote)), false
	}
	code, yes := x.ask(ctx, coordinator.OfferTakeover(team, handle, h), false)
	return current.Handle, code, yes
}

// coordinatorPath is which of the two paths a change to r.role takes, decided before anything
// changes: none (not a coordinator), the plain offer (the team has no coordinator), or the
// takeover (another member holds the role), which needs --demote-current where nothing can be
// asked, --yes included. The question names the path; takeover is whether the holder is demoted.
func (x *Hot) coordinatorPath(r request, roster agentbus.Roster, team, handle, who string, look coordinator.Lookup) (string, bool, term.ExitCode, bool) {
	if r.role != agentbus.RoleCoordinator {
		return "", false, term.ExitOK, true
	}
	if m, found := roster.Member(handle); found && m.Role == agentbus.RoleCoordinator {
		return handle + " is already " + team + "'s coordinator, so its role stays. Go on?", false, term.ExitOK, true
	}
	h := coordinator.Holding(roster, handle, look)
	if h.Handle == "" {
		return coordinator.OfferNone(team, who), false, term.ExitOK, true
	}
	if !r.demoteCurrent && (r.yes || !x.interactive()) {
		return "", false, x.usageError(coordinator.Refusal(team, who, h, optDemote)), false
	}
	return coordinator.OfferTakeover(team, who, h), true, term.ExitOK, true
}

// running is what the seat lists of this session's checkout, and of every checkout a session
// record names, say of a member's session. A list that cannot be read leaves its members
// unknown, which the question says.
func (x *Hot) running(ctx context.Context, t *target) coordinator.Lookup {
	return func(m agentbus.Member) coordinator.Running {
		dirs := []string{t.checkout()}
		if listing, err := t.store.List(); err == nil {
			for _, rec := range listing.Records {
				if !slices.Contains(dirs, rec.Dir) {
					dirs = append(dirs, rec.Dir)
				}
			}
		}
		var seats []agentbus.Seat
		for _, dir := range dirs {
			if found, _, err := x.Host.Bus.SeatList(ctx, dir); err == nil {
				seats = append(seats, found...)
			}
		}
		return coordinator.FromSeats(seats)(m)
	}
}

// changeRole is setRoleTo for a change that is never undone.
func (x *Hot) changeRole(ctx context.Context, team, handle, want string, demote bool) (term.ExitCode, bool) {
	undo, code, ok := x.setRoleTo(ctx, team, handle, want, demote)
	if undo == nil {
		return term.ExitFailure, false
	}
	return code, ok
}

// setRole is `session role` (H25): the session's seat in the team made a worker or the
// coordinator, then the wait for the session to be told.
func (x *Hot) setRole(ctx context.Context, t *target, r request) term.ExitCode {
	seat, in := seatOf(t.desired.Seats, r.team)
	if !in {
		x.mark(markFailed, "role: "+t.session+" is not in "+r.team)
		return term.ExitFailure
	}
	roster, code, up := x.teamUp(ctx, r.team)
	if !up {
		return code
	}
	handle := x.handleOf(ctx, t, r.team, seat)
	if handle == "" {
		return term.ExitFailure
	}
	member, found := roster.Member(handle)
	changed := !found || member.Role != r.role
	demote := r.demoteCurrent
	if changed {
		who := fmt.Sprintf("%s (seat %s, %s)", t.session, seat, handle)
		question, takeover, pathCode, ok := x.roleQuestion(r, roster, handle, who, x.running(ctx, t))
		if !ok {
			return pathCode
		}
		if askCode, proceed := x.confirm(ctx, question, r.yes); !proceed {
			return askCode
		}
		demote = demote || takeover
	}
	if roleCode, set := x.changeRole(ctx, r.team, handle, r.role, demote); !set {
		return roleCode
	}
	return x.waitTold(ctx, t, r.team, handle, r.role, changed)
}

// memberRole is `session role` given a member's handle in place of a session (Plan 00203 Task
// 3.8): the member, of any kind, made a worker or the coordinator by the same question and the
// same path as a session's seat. There is no session record to wait on: how the member's running
// session learns it is said (roles.Reach).
func (x *Hot) memberRole(ctx context.Context, r request) term.ExitCode {
	handle := r.session
	roster, code, up := x.teamUp(ctx, r.team)
	if !up {
		return code
	}
	member, found := roster.Member(handle)
	if !found {
		x.mark(markFailed, "role: "+handle+" is not a member of "+r.team)
		return term.ExitFailure
	}
	if member.Role == r.role {
		x.mark(markSkip, "role: "+handle+" is a "+r.role+" already")
		x.say("Nothing to do: " + handle + " is " + roleWords(r.team, r.role) + ".")
		return term.ExitOK
	}
	question, takeover, pathCode, ok := x.roleQuestion(r, roster, handle, handle, nil)
	if !ok {
		return pathCode
	}
	if askCode, proceed := x.confirm(ctx, question, r.yes); !proceed {
		return askCode
	}
	if roleCode, set := x.changeRole(ctx, r.team, handle, r.role, r.demoteCurrent || takeover); !set {
		return roleCode
	}
	x.say(handle + " is " + roleWords(r.team, r.role) + " now. " + roles.Reach)
	return term.ExitOK
}

// roleQuestion is role's question about who, the member handle: one of the two coordinator
// paths (coordinatorPath), or a worker's, which says when the team is left with no coordinator.
func (x *Hot) roleQuestion(r request, roster agentbus.Roster, handle, who string, look coordinator.Lookup) (string, bool, term.ExitCode, bool) {
	question, takeover, code, ok := x.coordinatorPath(r, roster, r.team, handle, who, look)
	if !ok || question != "" {
		return question, takeover, code, ok
	}
	question = fmt.Sprintf("Make %s %s?", who, roleWords(r.team, r.role))
	if m, found := roster.Member(handle); found && m.Role == agentbus.RoleCoordinator {
		question += " " + r.team + " then has no coordinator."
	}
	return question, false, term.ExitOK, true
}

// handleOf is the seat's handle, as agent-bus seat list has it; "" has been said.
func (x *Hot) handleOf(ctx context.Context, t *target, team, seat string) string {
	held, handle, err := x.seatHeld(ctx, t, team, seat)
	switch {
	case err != nil:
		x.mark(markFailed, "seat "+seat+": "+err.Error())
	case handle == "":
		x.mark(markFailed, "seat "+seat+": the checkout has no seat "+seat+" in "+team)
	case !held:
		x.mark(markSkip, "seat "+seat+": not held at the moment (ccy-teams session list says why)")
	}
	return handle
}

// roleWords is a role as a phrase: alpha's coordinator, a worker in alpha.
func roleWords(team, role string) string {
	if role == agentbus.RoleCoordinator {
		return team + "'s coordinator"
	}
	return "a worker in " + team
}

// waitTold waits for the session's report to show it was told its role (H25, H12); changed is
// whether this run changed it. A role this run did not change, already told at the first
// look, is the H19 no-op.
func (x *Hot) waitTold(ctx context.Context, t *target, team, handle, role string, changed bool) term.ExitCode {
	start := x.Now()
	for first := true; ; first = false {
		rep, err := membership.ReadReport(t.checkout(), t.session)
		if err == nil && rep.ToldRole(team, handle) == role {
			if !changed && first {
				x.mark(markSkip, "session: told it is "+roleWords(team, role))
				x.say("Nothing to do: " + handle + " is " + roleWords(team, role) + ", and the session knows it.")
				return term.ExitOK
			}
			x.mark(markDone, fmt.Sprintf("session: told it is %s (%.1f s)", roleWords(team, role), x.elapsed(start)))
			return term.ExitOK
		}
		if x.Now().Sub(start) >= x.Waits.Apply || x.ended(ctx, t) {
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				x.mark(markFailed, "session: not told within "+seconds(x.Waits.Apply)+" that it is "+roleWords(team, role)+
					": the session's report could not be read: "+err.Error()+"; fix or remove that file, then run the same command again")
				return term.ExitFailure
			}
			x.mark(markFailed, "session: not told within "+seconds(x.Waits.Apply)+" that it is "+roleWords(team, role)+
				": its watcher tells it at its next sync of "+team+", or its next prompt does; run the same command again to wait for it")
			return term.ExitFailure
		}
		x.Sleep(x.Waits.Poll)
	}
}

// leaving is what becomes of the coordinator role when a seat leaves its team (H23 step 2):
// nothing to do, or the role change to run once the remove is confirmed.
type leaving func(context.Context) (term.ExitCode, bool)

// leaveRole decides H23 step 2 before anything changes: a worker leaves as it is; the
// coordinator needs --coordinator (asked on a terminal, starting on No).
func (x *Hot) leaveRole(ctx context.Context, r request, team, handle string, up bool) (leaving, string, term.ExitCode, bool) {
	if !up {
		x.mark(markSkip, "role: not checked (the homeserver is down)")
		return nil, "", term.ExitOK, true
	}
	roster, err := x.Host.Bus.List(ctx, team)
	if err != nil {
		x.mark(markFailed, "role: the members of "+team+" could not be read: "+lastLine(err))
		return nil, "", term.ExitFailure, false
	}
	m, found := roster.Member(handle)
	if !found || m.Role != agentbus.RoleCoordinator {
		x.mark(markDone, "role: "+agentbus.Label(handle, "")+" is a worker")
		return nil, "", term.ExitOK, true
	}
	who := agentbus.Label(handle, "")
	choice := r.coordinator
	if choice == "" {
		if !x.interactive() {
			return nil, "", x.usageError(who + " is " + team + "'s coordinator: say what becomes of the role with " +
				"--coordinator=demote (no one has it) or --coordinator=hand-to=<seat>."), false
		}
		code, yes := x.ask(ctx, who+" is "+team+"'s coordinator. Make it a worker as it leaves (the role goes to no one)? "+
			"To hand the role to another seat instead, say no and use --coordinator=hand-to=<seat>.", false)
		if !yes {
			return nil, "", code, false
		}
		choice = coordDemote
	}
	said := who + " is " + team + "'s coordinator: "
	if choice == coordDemote {
		return func(ctx context.Context) (term.ExitCode, bool) {
			return x.changeRole(ctx, team, handle, agentbus.RoleWorker, false)
		}, said + "it becomes a worker, and " + team + " has no coordinator.", term.ExitOK, true
	}
	heir, why := handTo(roster, strings.TrimPrefix(choice, coordHandTo), handle)
	if why != "" {
		return nil, "", x.usageError(why), false
	}
	return func(ctx context.Context) (term.ExitCode, bool) {
		return x.changeRole(ctx, team, heir, agentbus.RoleCoordinator, true)
	}, said + heir + " becomes " + team + "'s coordinator instead.", term.ExitOK, true
}

// handTo is the member hand-to names: its full handle, or the one active member (not the one
// leaving) whose handle has that seat; why says why there is none.
func handTo(roster agentbus.Roster, value, leaving string) (string, string) {
	var found []string
	for _, m := range roster.Members {
		if m.Handle == leaving || m.Parked {
			continue
		}
		if m.Handle == value || handleSeat(m.Handle) == value {
			found = append(found, m.Handle)
		}
	}
	slices.Sort(found)
	switch len(found) {
	case 0:
		return "", "--coordinator=hand-to=" + value + ": no other active member of the team has that seat or handle (ccy-teams lists them)."
	case 1:
		return found[0], ""
	}
	return "", "--coordinator=hand-to=" + value + " names " + strconv.Itoa(len(found)) + " members (" + strings.Join(found, ", ") +
		"): give the full handle."
}

// handleSeat is a handle's seat: <repo>.<seat>+<host>.<type>.
func handleSeat(handle string) string {
	parts := strings.SplitN(handle, ".", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.SplitN(parts[1], "+", 2)[0]
}
