// Command qa-helper-tests runs the helper unit tests (tests/helpers/**/test_*.py) and refuses to
// call a run a pass unless it can prove which tests ran. It is a QA tool, not deployed anywhere;
// scripts/qa-helper-tests.bash builds and runs it.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/helpertests"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(helpertests.ExitCannotRun)
	}
	return int(helpertests.Main(args, newEnv(os.Stdout, os.Stderr, cwd)))
}

// newEnv is the command's view of the machine: the real file system, the shared discovery for the
// tracked tests, and the real process seam.
func newEnv(stdout, stderr io.Writer, cwd string) helpertests.Env {
	return helpertests.Env{
		Stdout:  stdout,
		Stderr:  stderr,
		Cwd:     cwd,
		Resolve: filepath.EvalSymlinks,
		Open:    os.DirFS,
		Tracked: tracked,
		Runner:  proc.OS{},
	}
}

// tracked is the shared discovery's tracked-helper-tests list (git's index, less deleted files),
// repository-relative. The discovery bounds its own git processes.
func tracked(root string) ([]string, error) {
	var stdout, stderr bytes.Buffer
	denv := discovery.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}}
	args := []string{"--root", root, "--scripts", filepath.Join(root, "scripts"), "tracked-helper-tests"}
	if code := discovery.Main(args, denv); code != discovery.ExitOK {
		return nil, fmt.Errorf("discovery of the tracked helper tests failed: %s", strings.TrimSpace(strings.TrimPrefix(stderr.String(), "ERROR: ")))
	}
	var rels []string
	for rel := range strings.SplitSeq(stdout.String(), "\x00") {
		if rel != "" {
			rels = append(rels, rel)
		}
	}
	return rels, nil
}
