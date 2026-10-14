package teams

import (
	"context"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// remove is ccy-teams remove.
func (a *App) remove(ctx context.Context, args []string) term.ExitCode {
	return a.newTeamOps().remove(ctx, args)
}
