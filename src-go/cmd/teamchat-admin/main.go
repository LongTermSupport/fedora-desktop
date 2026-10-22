// Command teamchat-admin is the host program of the agent-team bus v2 (Plan 00209, C 3.3). The
// flow is internal/teamchat/admin.
package main

import (
	"io"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/admin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// SourceDigest is the binary's source digest, set at build time with
// -ldflags "-X main.SourceDigest=<digest>" (C 3.3); `version` refuses to answer without it.
var SourceDigest string

func main() {
	os.Exit(int(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, SourceDigest)))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, digest string) term.ExitCode {
	return admin.Run(admin.Env{
		Args: args, Stdin: stdin, Stdout: stdout, Stderr: stderr, Proc: admin.OSProcess(), Digest: digest,
	})
}
