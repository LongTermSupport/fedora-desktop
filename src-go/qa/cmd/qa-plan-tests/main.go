// Command qa-plan-tests runs every active plan folder's own Python tests, each folder from itself,
// and refuses to call a run a pass unless it can prove which tests ran. It is a QA tool, not
// deployed anywhere; scripts/qa-plan-tests.bash builds and runs it.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/plantests"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(plantests.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(plantests.ExitCannotRun)
	}
	return int(plantests.Main(args, newEnv(os.Stdout, os.Stderr, cwd)))
}

// newEnv is the command's view of the machine: the real file system, the shared discovery for the
// tracked plan tests, the real process seam and fresh random tokens.
func newEnv(stdout, stderr io.Writer, cwd string) plantests.Env {
	return plantests.Env{
		Stdout:  stdout,
		Stderr:  stderr,
		Cwd:     cwd,
		Resolve: filepath.EvalSymlinks,
		Open:    os.DirFS,
		Tracked: tracked,
		Runner:  proc.OS{},
		Token:   token,
	}
}

// token is 16 random bytes in hex.
func token() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// tracked is the shared discovery's tracked-plan-tests list (git's index, less deleted files),
// repository-relative. The discovery bounds its own git processes.
func tracked(root string) ([]string, error) {
	var stdout, stderr bytes.Buffer
	denv := discovery.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}}
	args := []string{"--root", root, "--scripts", filepath.Join(root, "scripts"), "tracked-plan-tests"}
	if code := discovery.Main(args, denv); code != discovery.ExitOK {
		return nil, fmt.Errorf("discovery of the tracked plan tests failed: %s", strings.TrimSpace(strings.TrimPrefix(stderr.String(), "ERROR: ")))
	}
	var rels []string
	for rel := range strings.SplitSeq(stdout.String(), "\x00") {
		if rel != "" {
			rels = append(rels, rel)
		}
	}
	return rels, nil
}
