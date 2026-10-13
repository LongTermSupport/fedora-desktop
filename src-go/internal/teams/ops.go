package teams

import (
	"context"
	"errors"
)

// errOpNotBuilt is a flow of the picker that the Go port has not reached yet.
var errOpNotBuilt = errors.New("not built yet in the Go port")

// stubOps is the operator an App has until the real flows are wired in: the App's own
// subcommand methods, and the two team operations as errors.
type stubOps struct {
	app *App
}

func (o stubOps) create(ctx context.Context, args []string) int { return o.app.create(ctx, args) }
func (o stubOps) apply(ctx context.Context, args []string) int  { return o.app.apply(ctx, args) }
func (o stubOps) remove(ctx context.Context, args []string) int { return o.app.remove(ctx, args) }

// The two team operations say so on stderr before they fail, as every operator method does
// about its own failure: the picker adds only a pointer to what was said.
func (o stubOps) applyTeams(context.Context, []string) error {
	o.app.notBuilt("apply")
	return errOpNotBuilt
}

func (o stubOps) runElementPlay(context.Context, []string) error {
	o.app.notBuilt("the Element play")
	return errOpNotBuilt
}
