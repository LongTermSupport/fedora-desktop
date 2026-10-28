// Command ssh-copy-id-with-key installs a public key on a host, logging in with only one private key (SPEC-ssh-with-key).
package main

import (
	"context"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sshkey"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	ctx, stop := execx.SignalContext(context.Background())
	defer stop()
	return sshkey.App{Tool: sshkey.CopyID, Env: sshkey.NewEnv()}.Run(ctx, args)
}
