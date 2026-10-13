package teams

import "context"

// apply is ccy-teams apply: not built yet.
func (a *App) apply(_ context.Context, _ []string) int {
	return a.notBuilt("apply")
}
