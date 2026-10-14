package teams

import (
	"context"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// backup is ccy-teams backup.
func (a *App) backup(ctx context.Context, args []string) term.ExitCode {
	return a.newTeamOps().backup(ctx, args)
}

// restore is ccy-teams restore.
func (a *App) restore(ctx context.Context, args []string) term.ExitCode {
	return a.newTeamOps().restore(ctx, args)
}
