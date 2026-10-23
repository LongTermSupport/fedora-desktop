// Command gh-account-setup logs each configured GitHub account into gh with the required
// scopes, makes and uploads its SSH key, and proves the key logs in as that account. Started by
// ssh-keygen or ssh as their askpass program, it answers the passphrase prompt instead.
package main

import (
	"context"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ghaccount"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	if ghaccount.IsAskpass(os.Getenv) {
		return ghaccount.Askpass(os.Getenv, args, os.Stdout)
	}
	ctx, stop := execx.SignalContext(context.Background())
	defer stop()
	return ghaccount.App{Env: ghaccount.NewEnv()}.Run(ctx, args)
}
