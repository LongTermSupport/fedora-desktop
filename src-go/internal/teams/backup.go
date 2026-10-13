package teams

import "context"

// backup is ccy-teams backup: not built yet.
func (a *App) backup(_ context.Context, _ []string) int {
	return a.notBuilt("backup")
}

// restore is ccy-teams restore: not built yet.
func (a *App) restore(_ context.Context, _ []string) int {
	return a.notBuilt("restore")
}
