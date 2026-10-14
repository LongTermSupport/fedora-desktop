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
