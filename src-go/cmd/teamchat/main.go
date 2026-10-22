// Command teamchat is the member binary of the agent-team bus v2 (Plan 00209, B 2.2). The
// command line is internal/teamchat/member/cli.
package main

import (
	"io"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// SourceDigest is the binary's source digest, set at build time with
// -ldflags "-X main.SourceDigest=<digest>"; `--version` and `version` refuse to answer without it.
var SourceDigest string

func main() {
	os.Exit(int(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, SourceDigest)))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, digest string) term.ExitCode {
	return cli.Run(cli.Env{Args: args, Stdin: stdin, Stdout: stdout, Stderr: stderr, Digest: digest})
}
