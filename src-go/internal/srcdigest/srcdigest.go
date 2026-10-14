// Package srcdigest answers `--version` for a deployed command with the digest of the source
// it was built from. The play bakes the digest in (-ldflags "-X main.SourceDigest=<hex>"), and
// helpers/go_drift compares the installed command's answer with the checkout's digest, so a
// binary the plays have not rebuilt since a source change shows as drift.
package srcdigest

import (
	"fmt"
	"io"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Run answers a command line that is exactly `--version` with the digest and a newline, and
// hands every other command line to next. A build that carries no digest fails instead of
// printing an empty answer the drift check would read as a mismatch.
func Run(args []string, digest string, stdout, stderr io.Writer, next func([]string) term.ExitCode) term.ExitCode {
	if len(args) != 1 || args[0] != "--version" {
		return next(args)
	}
	if digest == "" {
		if _, err := fmt.Fprintln(stderr, "this binary was built without a source digest (go build -ldflags \"-X main.SourceDigest=<hex>\")"); err != nil {
			return term.ExitFailure
		}
		return term.ExitFailure
	}
	if _, err := fmt.Fprintln(stdout, digest); err != nil {
		return term.ExitFailure
	}
	return term.ExitOK
}
