package teams

import "context"

// backup is ccy-teams backup.
func (a *App) backup(ctx context.Context, args []string) int {
	return a.newTeamOps().backup(ctx, args)
}

// restore is ccy-teams restore.
func (a *App) restore(ctx context.Context, args []string) int {
	return a.newTeamOps().restore(ctx, args)
}
