package wiring

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// testDigest is the source digest the test binary answers as teamchat: the kit's name, its
// --version and the kit every client request carries.
const testDigest = "e2e0e2e0e2e0e2e0"

// agentName is the name the test binary is a session's CMD by.
const agentName = "agent"

// The end-to-end tests run the member binary for real: the test binary, copied into a kit as
// bin/teamchat, is teamchat by its name (cli.Run over Verbs, as cmd/teamchat), and named agent
// it is a session's CMD.
func TestMain(m *testing.M) {
	switch filepath.Base(os.Args[0]) {
	case "teamchat":
		os.Exit(int(cli.Run(cli.Env{
			Args: os.Args[1:], Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
			Digest: testDigest, Getenv: os.Getenv, Verbs: Verbs(),
		})))
	case agentName:
		os.Exit(agent(os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

// agent is a session's CMD: one line with its pid and TEAMCHAT_RUNTIME on the stdout it shares
// with the keeper, then it runs until its stdin ends.
func agent(stdin io.Reader, stdout io.Writer) int {
	if _, err := fmt.Fprintf(stdout, "%s %d %s\n", agentName, os.Getpid(), os.Getenv(paths.EnvRuntime)); err != nil {
		return 3
	}
	if _, err := io.Copy(io.Discard, stdin); err != nil {
		return 4
	}
	return 0
}
