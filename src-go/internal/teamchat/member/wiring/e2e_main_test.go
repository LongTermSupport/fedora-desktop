package wiring

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
		if err := armOnce(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		os.Exit(int(cli.Run(cli.Env{
			Args: os.Args[1:], Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
			Digest: testDigest, Getenv: os.Getenv, Verbs: Verbs(),
		})))
	case agentName:
		os.Exit(agent(os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

// The failpoint rows arm a point in the first process of a verb only: envArm is a comma list of
// verb=point, and the first process of the verb to create envArmDir/<point> arms it. A link
// the keeper restarts after the crash is not armed again, so the row sees the recovery.
const (
	envArm      = "E2E_ARM"
	envArmDir   = "E2E_ARM_DIR"
	envFailures = "TEAMCHAT_FAILPOINTS"
)

// armOnce sets TEAMCHAT_FAILPOINTS to the points this process claims, before any is hit.
func armOnce(args []string) error {
	spec, dir := os.Getenv(envArm), os.Getenv(envArmDir)
	if spec == "" || len(args) == 0 {
		return nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	points, err := claim(root, spec, args[0])
	if err = errors.Join(err, root.Close()); err != nil {
		return err
	}
	if len(points) == 0 {
		return os.Unsetenv(envFailures)
	}
	return os.Setenv(envFailures, strings.Join(points, ","))
}

// claim is the points of spec for verb that this process is the first to claim in root.
func claim(root *os.Root, spec, verb string) ([]string, error) {
	var points []string
	for pair := range strings.SplitSeq(spec, ",") {
		v, point, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("%s: %q is not verb=point", envArm, pair)
		}
		if v != verb {
			continue
		}
		f, err := root.OpenFile(point, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		points = append(points, point)
	}
	return points, nil
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
