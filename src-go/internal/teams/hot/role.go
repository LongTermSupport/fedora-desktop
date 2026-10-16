package hot

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// noUndo is the undo of a step that changed nothing.
func noUndo(context.Context) {}

// setRoleTo makes handle the team's want (worker or coordinator) through sudo agent-bus
// set-role (H25, H7). A coordinator while another member holds the role first makes that one a
// worker, only with demote (asked on a terminal), as seats.ChangeRole does. undo puts back what
// it changed, for an add that is rolled back.
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
	if holder != "" {
		if !x.runSetRole(ctx, team, holder, agentbus.RoleWorker, " (it was "+team+"'s coordinator)") {
			return noUndo, term.ExitFailure, false
		}
	}
	if !x.runSetRole(ctx, team, handle, want, "") {
		if holder != "" && x.runSetRole(ctx, team, holder, agentbus.RoleCoordinator, " again") {
			x.say(holder + " is " + team + "'s coordinator again.")
		}
		return noUndo, term.ExitFailure, false
	}
	undo := func(ctx context.Context) {
		back := x.Host.Bus.SetRole(ctx, team, handle, member.Role)
		if back == nil && holder != "" {
			back = x.Host.Bus.SetRole(ctx, team, holder, agentbus.RoleCoordinator)
		}
		if back != nil {
			x.mark(markFailed, "role: not put back ("+lastLine(back)+"): ccy-teams, the seat's menu, sets it")
			return
		}
		x.mark(markDone, "role: put back as it was")
	}
	return undo, term.ExitOK, true
}

// demoting is the coordinator a new coordinator replaces ("" for none), with demote said or
// asked on a terminal; agreed is false when it is not to be demoted.
func (x *Hot) demoting(ctx context.Context, roster agentbus.Roster, team, handle, want string, demote bool) (string, term.ExitCode, bool) {
	current, held := roster.Coordinator()
	if want != agentbus.RoleCoordinator || !held || current.Handle == handle {
		return "", term.ExitOK, true
	}
	if demote {
		return current.Handle, term.ExitOK, true
	}
	if !x.interactive() {
		return "", x.usageError(current.Handle + " is " + team + "'s coordinator: add --demote-current to make it a worker first."), false
	}
	code, yes := x.ask(ctx, current.Handle+" is "+team+"'s coordinator, and becomes a worker. Go on?", false)
	return current.Handle, code, yes
}

// changeRole is setRoleTo for a change that is never undone.
func (x *Hot) changeRole(ctx context.Context, team, handle, want string, demote bool) (term.ExitCode, bool) {
	undo, code, ok := x.setRoleTo(ctx, team, handle, want, demote)
	if undo == nil {
		return term.ExitFailure, false
	}
	return code, ok
}

// runSetRole is one set-role, said.
func (x *Hot) runSetRole(ctx context.Context, team, handle, role, note string) bool {
	if err := x.Host.Bus.SetRole(ctx, team, handle, role); err != nil {
		x.Kit.ReportRootErr(err)
		x.mark(markFailed, "role: "+handle+" was not made a "+role+": "+lastLine(err))
		return false
	}
	x.mark(markDone, "role: "+handle+" set to "+role+note)
	return true
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
	if changed {
		question := fmt.Sprintf("Make %s (seat %s of %s) %s?", handle, seat, t.session, roleWords(r.team, r.role))
		if askCode, proceed := x.confirm(ctx, question, r.yes); !proceed {
			return askCode
		}
	}
	if roleCode, set := x.changeRole(ctx, r.team, handle, r.role, r.demoteCurrent); !set {
		return roleCode
	}
	return x.waitTold(ctx, t, r.team, handle, r.role, changed)
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
// whether this run changed it.
func (x *Hot) waitTold(ctx context.Context, t *target, team, handle, role string, changed bool) term.ExitCode {
	start := x.Now()
	for {
		rep, err := membership.ReadReport(t.checkout(), t.session)
		if err == nil && rep.ToldRole(team, handle) == role {
			if !changed && x.Now().Equal(start) {
				x.mark(markSkip, "session: told it is "+roleWords(team, role))
				x.say("Nothing to do: " + handle + " is " + roleWords(team, role) + ", and the session knows it.")
				return term.ExitOK
			}
			x.mark(markDone, fmt.Sprintf("session: told it is %s (%.1f s)", roleWords(team, role), x.elapsed(start)))
			return term.ExitOK
		}
		if x.Now().Sub(start) >= x.Waits.Apply || x.ended(ctx, t) {
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
// coordinator needs --coordinator (asked on a terminal, demote by default).
func (x *Hot) leaveRole(ctx context.Context, r request, team, seat, handle string, up bool) (leaving, term.ExitCode, bool) {
	if !up {
		x.mark(markSkip, "role: not checked (the homeserver is down)")
		return nil, term.ExitOK, true
	}
	roster, err := x.Host.Bus.List(ctx, team)
	if err != nil {
		x.mark(markFailed, "role: the members of "+team+" could not be read: "+lastLine(err))
		return nil, term.ExitFailure, false
	}
	if m, found := roster.Member(handle); !found || m.Role != agentbus.RoleCoordinator {
		x.mark(markDone, "role: seat "+seat+" is a worker")
		return nil, term.ExitOK, true
	}
	choice := r.coordinator
	if choice == "" {
		if !x.interactive() {
			return nil, x.usageError("seat " + seat + " is " + team + "'s coordinator: say what becomes of the role with " +
				"--coordinator=demote (no one has it) or --coordinator=hand-to=<seat>."), false
		}
		code, yes := x.ask(ctx, "Seat "+seat+" is "+team+"'s coordinator. Make it a worker as it leaves (the role goes to no one)? "+
			"To hand the role to another seat instead, say no and use --coordinator=hand-to=<seat>.", true)
		if !yes {
			return nil, code, false
		}
		choice = coordDemote
	}
	if choice == coordDemote {
		return func(ctx context.Context) (term.ExitCode, bool) {
			return x.changeRole(ctx, team, handle, agentbus.RoleWorker, false)
		}, term.ExitOK, true
	}
	heir, why := handTo(roster, strings.TrimPrefix(choice, coordHandTo), handle)
	if why != "" {
		return nil, x.usageError(why), false
	}
	return func(ctx context.Context) (term.ExitCode, bool) {
		return x.changeRole(ctx, team, heir, agentbus.RoleCoordinator, true)
	}, term.ExitOK, true
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
