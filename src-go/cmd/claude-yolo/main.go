// Command claude-yolo is the ccy launcher's Go front (Plan 00191 Task 4.5): installed at
// /var/local/claude-yolo/claude-yolo, it answers --version, --help, --list-tokens,
// --token-usage, --prevent and --export-token NAME itself and hands every other run to the bash launcher beside it, claude-yolo.bash (internal/ccylaunch).
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccylaunch"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Version is the launcher's CCY_VERSION, set at build time with
// -ldflags "-X main.Version=<version>" by play-claude-yolo.yml.
var Version string

func main() {
	os.Exit(int(run(os.Args[1:], Version)))
}

func run(args []string, version string) term.ExitCode {
	self, err := os.Executable()
	if err != nil {
		if _, werr := fmt.Fprintf(os.Stderr, "ccy: cannot find this program's own path: %v\n", err); werr != nil {
			return term.ExitFailure
		}
		return term.ExitFailure
	}
	c, err := clock()
	if err != nil {
		if _, werr := fmt.Fprintf(os.Stderr, "ccy: %v\n", err); werr != nil {
			return term.ExitFailure
		}
		return term.ExitFailure
	}
	front := ccylaunch.Front{
		Version: version, Self: self, Args: args, Environ: os.Environ(), LookPath: exec.LookPath,
		Runner: execx.OS{}, Execer: execx.OS{}, Stdout: os.Stdout, Stderr: os.Stderr, Clock: c,
	}
	return front.Run(context.Background())
}
