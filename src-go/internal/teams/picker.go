package teams

import "context"

// picker is ccy-teams with no argument: not built yet.
func (a *App) picker(_ context.Context) int {
	return a.notBuilt("picker")
}
