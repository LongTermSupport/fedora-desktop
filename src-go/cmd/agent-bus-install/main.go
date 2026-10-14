// Command agent-bus-install builds an agent team bus homeserver host on Fedora: packages, the
// service account, the pinned Tuwunel, the units, and each team's files, firewall rules and
// services. It runs as root; the play installs it at /usr/local/sbin/agent-bus-install.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/businstall"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// SourceDigest identifies the source this binary was built from, set at build time with
// -ldflags "-X main.SourceDigest=<digest>". `team` stamps it, and declare compares it with
// the digest of the checkout.
var SourceDigest string

func main() {
	os.Exit(int(execute()))
}

// execute is main without the exit, so a test can see what the process looks like afterwards.
// It sets no umask: every file the installer writes carries an explicit mode on its own
// descriptor, and a process-wide umask would reach the programs it runs.
func execute() term.ExitCode {
	return run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
}

// run builds the configuration (loadConfig is the one place the test build differs), resets
// PATH when the configuration says so, and runs the installer.
func run(args []string, getenv func(string) string, stdout, stderr io.Writer) term.ExitCode {
	cfg, err := loadConfig(getenv, stdout, stderr, SourceDigest)
	if err != nil {
		return complain(stderr, term.ExitUsage, "%v", err)
	}
	if cfg.Path != "" {
		if err = os.Setenv("PATH", cfg.Path); err != nil {
			return complain(stderr, term.ExitFailure, "set PATH: %v", err)
		}
	}
	return businstall.NewApp(cfg).Run(args)
}

// complain prints a diagnostic and returns the exit status; stderr that cannot be written is
// itself a failure.
func complain(stderr io.Writer, code term.ExitCode, format string, args ...any) term.ExitCode {
	if _, err := fmt.Fprintf(stderr, "agent-bus-install: "+format+"\n", args...); err != nil {
		return term.ExitFailure
	}
	return code
}
