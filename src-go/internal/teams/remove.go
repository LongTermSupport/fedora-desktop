package teams

import "context"

// remove is ccy-teams remove.
func (a *App) remove(ctx context.Context, args []string) int {
	return a.newTeamOps().remove(ctx, args)
}
