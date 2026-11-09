// Command histclean removes every shell-history entry that contains a pattern, from the
// history file and the Ctrl+R context file, after one confirmation.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/histclean"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	home := os.Getenv("HOME")
	os.Exit(int(run(os.Args[1:], home, os.Stdin, term.FileIsTerminal(os.Stdin), os.Stdout, os.Stderr)))
}

// run keeps both files where the history setup puts them, under the user's state directory;
// HISTFILE is not read, because a terminal with Ctrl+R set points it at /dev/null.
func run(args []string, home string, stdin io.Reader, interactive bool, stdout, stderr io.Writer) term.ExitCode {
	if home == "" {
		if _, err := fmt.Fprintln(stderr, "histclean: HOME is not set, so the history files cannot be found"); err != nil {
			return term.ExitFailure
		}
		return term.ExitFailure
	}
	ctx, stop := execx.SignalContext(context.Background())
	defer stop()
	state := filepath.Join(home, ".local", "state", "bash")
	return histclean.App{
		HistoryPath: filepath.Join(state, "history"),
		ContextPath: filepath.Join(state, "context"),
		Stdin:       stdin, Interactive: interactive,
		Stdout: stdout, Stderr: stderr,
	}.Run(ctx, args)
}
