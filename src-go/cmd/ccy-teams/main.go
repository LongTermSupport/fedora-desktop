// Command ccy-teams creates, applies and removes agent teams on this desktop.
package main

import (
	"io"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// RootDir is the fedora-desktop checkout the binary was built from, set at build time with
// -ldflags "-X main.RootDir=<checkout>": the declare helper and the plays are found there.
var RootDir string

func main() {
	os.Exit(int(run(os.Args[1:], os.Getenv, RootDir, os.Stderr)))
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
	return teams.NewApp(teams.LoadConfig(root, getenv)).Run(args)
}
