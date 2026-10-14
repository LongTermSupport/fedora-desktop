package picker

import (
	"context"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/board"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
)

// act does what key asks of row: the key that was pressed, or "" for Enter, and the row it
// was pressed on. Enter means what the row's kind makes of it.
func (r *run) act(ctx context.Context, key string, row board.Row) (flow.Result, error) {
	switch {
	case key == "ctrl-n":
		return r.p.Seats.NewSession(ctx, r.snap, row.Team)
	case key == "ctrl-r":
		return r.p.Seats.ToggleRestore(ctx, r.snap, row)
	case key == "ctrl-x":
		return r.p.Seats.RemoveOrPark(ctx, r.snap, row)
	case key == "ctrl-e":
		return r.p.Login.OpenElement(ctx, r.snap, row.Team)
	case key == "ctrl-p", key == "" && row.Kind == board.KindHuman:
		return r.p.Login.PasswordRow(ctx, r.snap, row)
	case key == "ctrl-u":
		return r.updateAll(ctx)
	case key == "?", key == "" && row.Kind == board.KindTeam:
		r.p.showHelp(ctx)
		return flow.Done, nil
	case key == "" && row.Kind == board.KindCcy:
		return r.p.Seats.AttachSeat(ctx, r.snap, row)
	}
	r.p.Kit.Tell("Enter on " + row.Member + " does nothing: Ctrl-X parks a member. ? shows every key.")
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
