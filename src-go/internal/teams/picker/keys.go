package picker

import (
	"context"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
)

// act does what key asks of row: the key that was pressed, or "" for Enter, and the row it
// was pressed on. Enter opens the menu of what can be done to the row; a key is a shortcut to
// one entry of that menu.
func (r *run) act(ctx context.Context, key string, row board.Row) (flow.Result, error) {
	if key == "" {
		return r.menu(ctx, row)
	}
	return r.do(ctx, key, row)
}

// do is one action on row: a key of the menu, which is also the shortcut for it.
func (r *run) do(ctx context.Context, key string, row board.Row) (flow.Result, error) {
	switch key {
	case keyNew:
		return r.p.Seats.NewSession(ctx, r.snap, row.Team)
	case keyRestore:
		return r.p.Seats.ToggleRestore(ctx, r.snap, row)
	case keyRemove:
		return r.p.Seats.RemoveOrPark(ctx, r.snap, row)
	case keyElement:
		return r.p.Login.OpenElement(ctx, r.snap, row.Team)
	case keyElementAll:
		// Unlike one team's Ctrl-E, this can name several teams with no profile, so it waits
		// for Enter before the list is redrawn over what it said.
		res, err := r.p.Login.OpenEveryTeam(ctx, r.snap)
		if err == nil {
			r.p.Kit.BackToList(ctx)
		}
		return res, err
	case keyPassword:
		return r.p.Login.PasswordRow(ctx, r.snap, row)
	case keyUpdate:
		return r.updateAll(ctx)
	case keyHelp:
		r.p.showHelp(ctx)
		return flow.Done, nil
	case keyAttach:
		return r.p.Seats.AttachSeat(ctx, r.snap, row)
	case keyCoordinator:
		return r.p.Seats.ToggleCoordinator(ctx, r.snap, row)
	case keyLxc:
		return r.addLxc(ctx, row.Team)
	case keyShutdown:
		return r.p.Shutdown.Offer(ctx, row.Team)
	}
	r.p.Kit.Tell("That key does nothing here. Enter lists what can be done to a row. ? shows every key.")
	r.p.Kit.BackToList(ctx)
	return flow.Done, nil
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
