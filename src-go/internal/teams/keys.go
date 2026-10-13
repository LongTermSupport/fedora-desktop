package teams

import "context"

// act does what key asks of row: the key that was pressed, or "" for Enter, and the row it
// was pressed on. Enter means what the row's kind makes of it.
func (r *pickerRun) act(ctx context.Context, key string, row Row) (result, error) {
	switch {
	case key == "ctrl-n":
		return r.newSession(ctx, row.Team)
	case key == "ctrl-r":
		return r.toggleRestore(ctx, row)
	case key == "ctrl-x":
		return r.removeOrPark(ctx, row)
	case key == "ctrl-e":
		return r.openElement(ctx, row.Team)
	case key == "ctrl-p", key == "" && row.Kind == kindHuman:
		return r.passwordRow(ctx, row)
	case key == "ctrl-u":
		return r.updateAll(ctx)
	case key == "?", key == "" && row.Kind == kindTeam:
		r.a.showHelp(ctx)
		return done, nil
	case key == "" && row.Kind == kindCcy:
		return r.attachSeat(ctx, row)
	}
	r.a.tell("Enter on " + row.Member + " does nothing: Ctrl-X parks a member. ? shows every key.")
	r.a.backToList(ctx)
	return done, nil
}

// updateAll brings every team that is out of date or not built up to what is declared.
func (r *pickerRun) updateAll(ctx context.Context) (result, error) {
	if err := r.a.inPicker("ccy-teams apply --all", r.a.Ops.apply(ctx, []string{"--all"})); err != nil {
		return done, err
	}
	r.a.backToList(ctx)
	return done, nil
}
