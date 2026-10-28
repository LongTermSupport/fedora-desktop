// Command container-images removes what nothing uses from the rootless podman store (Plan
// 00235): `prune [--yes]`, run daily by container-images-prune.timer.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/containerimages"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	home, err := os.UserHomeDir()
	if err != nil {
		if _, werr := fmt.Fprintf(os.Stderr, "container-images: %v\n", err); werr != nil {
			return term.ExitFailure
		}
		return term.ExitFailure
	}
	ctx, stop := execx.SignalContext(context.Background())
	defer stop()
	return containerimages.App{Env: containerimages.Env{
		Runner:   execx.OS{},
		Getenv:   os.Getenv,
		LookPath: exec.LookPath,
		Now:      time.Now,
		Home:     home,
		UID:      os.Getuid(),
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
	}}.Run(ctx, args)
}
