package seats

import (
	"context"
	"errors"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/coordinator"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
)

// ChangeRole makes the member the team's coordinator (want true) or a worker (want false), after
// asking, and says whether it changed anything. It is the one code path that changes a role: the
// seat menu and the launcher's chooser both call it. A member already in the wanted role is not
// asked about and costs no sudo. A team has at most one coordinator, so making another member
// the coordinator first makes the holder a worker. The two paths are asked apart (package
// coordinator): a team with no coordinator is offered one plainly; a team that has one is a
// takeover, whose question names the holder, says whether it runs (as look knows; nil knows
// nothing) and starts on no. If the second step fails the holder is put back, and the error says
// which steps happened.
func (s *Seats) ChangeRole(ctx context.Context, team, handle string, want bool, look coordinator.Lookup) (bool, error) {
	roster, err := s.Bus.List(ctx, team)
	if err != nil {
		s.Kit.ReportRootErr(err)
		return false, fmt.Errorf("the members of %s could not be read, so no role was changed", team)
	}
	member, ok := roster.Member(handle)
	if !ok {
		return false, fmt.Errorf("%s is not a member of %s", handle, team)
	}
	if want == (member.Role == agentbus.RoleCoordinator) {
		return false, nil
	}
	holder := coordinator.Holding(roster, handle, look)
	yes, err := s.Kit.Confirm(ctx, roleTitle(handle, team, want, holder), roleQuestion(handle, team, want, holder),
		roleAction(handle, want, holder), false)
	if err != nil || !yes {
		return false, err
	}
	if !s.Kit.SudoReady(ctx, "A role is changed as root, by agent-bus.") {
		return false, errors.New("sudo is not ready, so no role was changed")
	}
	if !want {
		return s.setRole(ctx, team, handle, agentbus.RoleWorker)
	}
	if holder.Handle != "" {
		if changed, err := s.setRole(ctx, team, holder.Handle, agentbus.RoleWorker); err != nil || !changed {
			return false, err
		}
		return s.promote(ctx, team, handle, holder.Handle)
	}
	return s.setRole(ctx, team, handle, agentbus.RoleCoordinator)
}

// promote makes handle the coordinator after previous was made a worker, and puts previous back
// when it cannot.
func (s *Seats) promote(ctx context.Context, team, handle, previous string) (bool, error) {
	if changed, err := s.setRole(ctx, team, handle, agentbus.RoleCoordinator); err != nil || !changed {
		if _, back := s.setRole(ctx, team, previous, agentbus.RoleCoordinator); back != nil {
			return false, fmt.Errorf("%s was made a worker and could not be made the coordinator again (%w): "+
				"the team has no coordinator; run sudo agent-bus set-role %s %s --role=coordinator",
				previous, back, team, previous)
		}
		return false, fmt.Errorf("%s could not be made the coordinator, so %s is the coordinator again: %w", handle, previous, err)
	}
	return true, nil
}

func (s *Seats) setRole(ctx context.Context, team, handle, role string) (bool, error) {
	if err := s.Bus.SetRole(ctx, team, handle, role); err != nil {
		s.Kit.ReportRootErr(err)
		return false, fmt.Errorf("%s was not made a %s", handle, role)
	}
	return true, nil
}

func roleTitle(handle, team string, want bool, holder coordinator.Holder) string {
	switch {
	case !want:
		return "Stop " + handle + " being the coordinator of " + team + "?"
	case holder.Handle != "":
		return "Replace " + holder.Handle + " as coordinator of " + team + "?"
	}
	return "Make " + handle + " the coordinator of " + team + "?"
}

func roleQuestion(handle, team string, want bool, holder coordinator.Holder) string {
	switch {
	case !want:
		return team + " will have no coordinator, and " + handle + " will be a worker. Workers still tell the humans what they do."
	case holder.Handle != "":
		return coordinator.OfferTakeover(team, handle, holder) + " " + holder.Handle + " becomes a worker; " + handle +
			" is copied on what workers tell humans and takes the humans' requests to dispatch."
	}
	return coordinator.OfferNone(team, handle) + " " + handle +
		" will be copied on what workers tell humans, and take the humans' requests to dispatch to workers."
}

func roleAction(handle string, want bool, holder coordinator.Holder) string {
	switch {
	case !want:
		return "stop it being the coordinator"
	case holder.Handle != "":
		return "remove " + holder.Handle + " as coordinator and make " + handle + " the coordinator"
	}
	return "make it the coordinator"
}

// SnapshotRunning is what the board's seat rows say of a member's session: a parked member is
// parked, a seat row a live process holds is running, a ccy seat row no process holds is not,
// and a member with no seat row (another host, an LXC member) is unknown.
func SnapshotRunning(snap *board.Snapshot) coordinator.Lookup {
	return func(m agentbus.Member) coordinator.Running {
		if m.Parked {
			return coordinator.Parked
		}
		state := coordinator.Unknown
		for _, row := range snap.Rows {
			switch {
			case row.Handle != m.Handle:
			case row.Held:
				return coordinator.Active
			case row.Kind == board.KindCcy:
				state = coordinator.Stopped
			}
		}
		return state
	}
}

// ToggleCoordinator is the seat menu's entry: make the seat's member the coordinator, or, when
// it is, stop it being. It says what happened and goes back to the list.
func (s *Seats) ToggleCoordinator(ctx context.Context, snap *board.Snapshot, row board.Row) (flow.Result, error) {
	want := snap.RoleOf(row.Team, row.Handle) != agentbus.RoleCoordinator
	changed, err := s.ChangeRole(ctx, row.Team, row.Handle, want, SnapshotRunning(snap))
	switch {
	case err != nil:
		s.Kit.Term.PrintError(err.Error())
		return s.Kit.Note(ctx, "Nothing was changed.")
	case !changed:
		return s.Kit.Note(ctx, "Nothing was changed.")
	case want:
		return s.Kit.Note(ctx, row.Handle+" is now the coordinator of "+row.Team+".")
	}
	return s.Kit.Note(ctx, row.Handle+" is now a worker; "+row.Team+" has no coordinator.")
}
