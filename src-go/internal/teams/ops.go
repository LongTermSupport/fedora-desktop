package teams

import "context"

// liveOps is the operator an App has: the flows the picker calls into, each built over the
// App's adapters when it is called, so an adapter a test replaces is the one it uses.
type liveOps struct {
	app *App
}

func (o liveOps) create(ctx context.Context, args []string) int { return o.app.create(ctx, args) }
func (o liveOps) apply(ctx context.Context, args []string) int  { return o.app.apply(ctx, args) }
func (o liveOps) remove(ctx context.Context, args []string) int { return o.app.remove(ctx, args) }

func (o liveOps) applyTeams(ctx context.Context, teams []string) error {
	return o.app.newTeamOps().applyTeams(ctx, teams)
}

func (o liveOps) runElementPlay(ctx context.Context, teams []string) error {
	return o.app.newTeamOps().runElementPlay(ctx, teams)
}
