package teams

import "context"

// remove is ccy-teams remove: not built yet.
func (a *App) remove(_ context.Context, _ []string) int {
	return a.notBuilt("remove")
}
