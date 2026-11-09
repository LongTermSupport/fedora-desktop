package picker

import (
	"context"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seats"
)

// do is one action on row, chosen from the menu Enter on the row opens.
func (r *run) do(ctx context.Context, id string, row board.Row) (flow.Result, error) {
	switch id {
	case actNew:
		return r.p.Seats.NewSession(ctx, r.snap, row.Team)
	case actRestore:
		return r.p.Seats.ToggleRestore(ctx, r.snap, row)
	case actRemove:
		return r.p.Seats.RemoveOrPark(ctx, r.snap, row)
	case actElement:
		return r.p.Login.OpenElement(ctx, r.snap, row.Team)
	case actPassword:
		return r.p.Login.PasswordRow(ctx, r.snap, row)
	case actHelp:
		r.p.showHelp(ctx)
		return flow.Done, nil
	case actAttach:
		return r.p.Seats.AttachSeat(ctx, r.snap, row)
	case actCoordinator, actDemote:
		return r.p.Seats.ToggleCoordinator(ctx, r.snap, row)
	case actLxc:
		return r.addLxc(ctx, row.Team)
	case actAddRunning:
		return r.addRunningTo(ctx, row.Team)
	case actAddThis:
		return r.addThisSession(ctx, row)
	}
	return r.doTeam(ctx, id, row)
}

// doTeam is one of a team row's own actions.
func (r *run) doTeam(ctx context.Context, id string, row board.Row) (flow.Result, error) {
	switch id {
	case actElementAll:
		// Unlike opening one team, this can name several teams with no profile, so it waits
		// for Enter before the list is redrawn over what it said.
		res, err := r.p.Login.OpenEveryTeam(ctx, r.snap)
		if err == nil {
			r.p.Kit.BackToList(ctx)
		}
		return res, err
	case actUpdate:
		return r.updateAll(ctx)
	case actShutdown:
		return r.p.Shutdown.Offer(ctx, row.Team)
	case actRestart:
		return r.p.Restart.Offer(ctx, row.Team)
	case actWindDown:
		return r.p.WindDown.Offer(ctx, row.Team)
	case actSettings:
		return r.p.Settings.Edit(ctx, row.Team, seats.SnapshotRunning(r.snap))
	case actAskDescription, actAskLimit:
		return r.askAgainOf(ctx, id, row.Team)
	}
	// Every ID comes from a menu line actionsFor built, so an unknown one is a defect here.
	return flow.Done, fmt.Errorf("picker: no action %q on the row", id)
}

// updateAll brings every team that is out of date or not built up to what is declared.
func (r *run) updateAll(ctx context.Context) (flow.Result, error) {
	if err := r.p.Kit.InPicker("ccy-teams apply --all", r.p.Ops.Apply(ctx, []string{ops.FlagAll})); err != nil {
		return flow.Done, err
	}
	r.p.Kit.BackToList(ctx)
	return flow.Done, nil
}

// addLxc is the menu's Add an LXC container's agent: the questions, the entry and the apply of
// ccy-teams lxc-add, for the row's team, then back to the list.
func (r *run) addLxc(ctx context.Context, team string) (flow.Result, error) {
	if err := r.p.Kit.InPicker("ccy-teams lxc-add "+team, r.p.Lxc.Run(ctx, []string{team})); err != nil {
		return flow.Done, err
	}
	r.p.Kit.BackToList(ctx)
	return flow.Done, nil
}
