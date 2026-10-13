package teams

import "context"

// create is ccy-teams create: not built yet.
func (a *App) create(_ context.Context, _ []string) int {
	return a.notBuilt("create")
}
