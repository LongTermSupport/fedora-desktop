package teams

import (
	"context"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// apply is ccy-teams apply.
func (a *App) apply(ctx context.Context, args []string) term.ExitCode {
	return a.newTeamOps().apply(ctx, args)
}

// newTeamOps is the flows that change what runs on this machine, over the App's adapters.
// Its Term is its own, as the creator's is.
func (a *App) newTeamOps() *teamOps {
	return &teamOps{
		Cfg: a.Config, Runner: a.Runner, Declare: a.Declare, Sudo: a.Sudo, Bus: a.Bus,
		Gum: a.Gum, Picker: a.Picker, Term: a.freshTerm(), Stdin: a.Stdin, Stdout: a.Stdout,
		IsTerminal: a.Term.IsTerminal,
		Refresh: func(ctx context.Context) (map[string]declare.Status, error) {
			snap, err := a.refresh(ctx)
			if err != nil {
				return nil, err
			}
			return snap.Status, nil
		},
		Changed:      func() { a.forgetDevices("") },
		NeedCheckout: a.needCheckout,
		LookPath:     a.LookPath,
		FileExists: func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
	}
}
