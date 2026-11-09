// Command ccy-token-usable says whether a ccy launch may start on a token (F15 in
// internal/ccylaunch): exit 0 when it has usage left or its usage could not be read, 3 when
// its 5-hour or 7-day window is used up, with the reason on stderr; --mark prints the used-up
// mark the token chooser shows, from the usage cache only. The bash launcher runs it.
package main

import (
	"context"
	"os"
	"os/exec"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccylaunch"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	front := ccylaunch.Front{
		Args: args, Environ: os.Environ(), LookPath: exec.LookPath, Runner: execx.OS{},
		Stdout: os.Stdout, Stderr: os.Stderr, Clock: ccylaunch.SystemClock{},
	}
	return front.TokenUsable(context.Background())
}
