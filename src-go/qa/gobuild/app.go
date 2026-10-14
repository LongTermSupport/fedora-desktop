package gobuild

import (
	"context"
	"flag"
	"io"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const usage = `usage: go-source digest --source <checkout> --command <name>
       go-source drift  --source <checkout> [--home <dir>]

digest  prints the source digest of the command under src-go/cmd and nothing else
drift   compares each installed command of scripts/go-binary-installs.tsv with the checkout
`

// Main runs one command line of go-source and returns its exit code.
func Main(ctx context.Context, runner proc.Runner, args []string, stdout, stderr io.Writer) int {
	out, errs := &sink{w: stdout}, &sink{w: stderr}
	return settle(dispatch(ctx, runner, args, out, errs), out, errs)
}

func dispatch(ctx context.Context, runner proc.Runner, args []string, out, errs *sink) int {
	if len(args) == 0 || (args[0] != "digest" && args[0] != "drift") {
		errs.printf("%s", usage)
		return ExitCannotRun
	}
	flags := flag.NewFlagSet("go-source "+args[0], flag.ContinueOnError)
	flags.SetOutput(errs.w)
	source := flags.String("source", "", "the fedora-desktop checkout")
	command := flags.String("command", "", "the command under src-go/cmd (digest)")
	home := flags.String("home", defaultHome(), "what ~/ in the table means (drift)")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return ExitCannotRun
	}
	if *source == "" {
		errs.printf("go-source %s: --source is required\n", args[0])
		return ExitCannotRun
	}
	if args[0] == "drift" {
		return drift(ctx, runner, DriftOptions{Source: *source, Home: *home}, out, errs)
	}
	if *command == "" {
		errs.printf("go-source digest: --command is required\n")
		return ExitCannotRun
	}
	digest, err := SourceDigest(*source, *command)
	if err != nil {
		errs.printf("go-source: %v\n", err)
		return ExitFailure
	}
	out.printf("%s\n", digest)
	return ExitOK
}

func defaultHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}
