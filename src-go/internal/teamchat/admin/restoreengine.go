package admin

import (
	"context"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// Engine is the Restorer over the restore engine and its Deps (RW1). The verb holds the team
// lock, so a discard, which takes no lock argument, is given none.
type Engine struct {
	Deps restore.Deps
}

// Run implements Restorer.
func (e Engine) Run(ctx context.Context, held *teamlock.Lock, req RestoreRequest) ([]byte, error) {
	return answered(restore.Run(ctx, e.Deps, held, restore.Request{
		Team: req.Team, Backup: req.Backup, Offline: req.Offline, FromBackup: req.RosterFromBackup,
	}))
}

// Resume implements Restorer.
func (e Engine) Resume(ctx context.Context, held *teamlock.Lock, team paths.TeamID) ([]byte, error) {
	return answered(restore.Resume(ctx, e.Deps, held, team))
}

// Discard implements Restorer.
func (e Engine) Discard(ctx context.Context, _ *teamlock.Lock, team paths.TeamID) ([]byte, error) {
	return answered(restore.Discard(ctx, e.Deps, team))
}

// answered is the engine's answer as the verb prints it (RS9, RS13): the answer of a restore
// that ran, or one beside a failure that marks it Printed (RS8); nothing beside any other
// failure.
func answered(a restore.Answer, err error) ([]byte, error) {
	if err != nil && !a.Printed {
		return nil, err
	}
	return a.Encode(), err
}

// Bootstrapper is step 10 asked of sys's child (n2d-D7): host.Deps.Bootstrap for team apply and
// the restore's bootstrap after its swap (RS7).
func Bootstrapper(sys *System) func(ctx context.Context, b host.Bootstrap) ([]string, error) {
	return func(ctx context.Context, b host.Bootstrap) ([]string, error) { return bootstrap(ctx, sys, b) }
}
