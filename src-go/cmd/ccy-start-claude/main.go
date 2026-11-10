// Command ccy-start-claude is the end of a ccy container's start: entrypoint.sh execs it with
// the session's command, and it execs the command line the session runs (internal/ccystart).
package main

import (
	"context"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccystart"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(session(os.Args[1:]))))
}

func session(args []string) ccystart.Session {
	return ccystart.Session{
		Env:    os.LookupEnv,
		Args:   args,
		Paths:  ccystart.ImagePaths(),
		Stderr: os.Stderr,
		Runner: execx.OS{},
		Execer: execx.OS{},
	}
}

func run(s ccystart.Session) term.ExitCode {
	return ccystart.Start(context.Background(), s)
}
