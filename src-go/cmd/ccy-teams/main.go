// Command ccy-teams creates, applies and removes agent teams on this desktop.
package main

import (
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams"
)

// RootDir is the fedora-desktop checkout the binary was built from, set at build time with
// -ldflags "-X main.RootDir=<checkout>": the declare helper and the plays are found there.
var RootDir string

func main() {
	os.Exit(run(os.Args[1:], os.Getenv))
}

func run(args []string, getenv func(string) string) int {
	return teams.NewApp(teams.LoadConfig(RootDir, getenv)).Run(args)
}
