// Command ccy-teams creates, applies and removes agent teams on this desktop.
package main

import (
	"io"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/srcdigest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/config"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// RootDir is the fedora-desktop checkout the binary was built from, set at build time with
// -ldflags "-X main.RootDir=<checkout>": the declare helper and the plays are found there.
var RootDir string

// SourceDigest identifies the source this binary was built from, set at build time with the
// -X main.SourceDigest=<hex> linker flag that tasks/build-go-command.yml adds; `--version`
// prints it, and the deployed-drift check compares it with the checkout's.
var SourceDigest string

func main() {
	os.Exit(int(execute(os.Args[1:], os.Stdout, os.Stderr)))
}

// execute answers --version and otherwise runs the tool.
func execute(args []string, stdout, stderr io.Writer) term.ExitCode {
	return srcdigest.Run(args, SourceDigest, stdout, stderr, func(rest []string) term.ExitCode {
		return run(rest, os.Getenv, RootDir, stderr)
	})
}

// run refuses an empty root: without it every path is relative to the working directory, so a
// plain `go build` or `go install` run inside another checkout would act on that checkout.
func run(args []string, getenv func(string) string, root string, stderr io.Writer) term.ExitCode {
	if root == "" {
		t := &term.Term{Err: stderr, IsTerminal: term.FileIsTerminal, Getenv: getenv}
		t.PrintError("ccy-teams was built without -ldflags \"-X main.RootDir=<checkout>\"; " +
			"build it with playbooks/imports/play-claude-yolo.yml, which bakes the checkout in")
		return term.ExitFailure
	}
	return teams.NewApp(config.Load(root, getenv)).Run(args)
}
