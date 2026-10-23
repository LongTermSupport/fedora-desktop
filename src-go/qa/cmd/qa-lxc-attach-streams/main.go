// Command qa-lxc-attach-streams is the gate for the rule lxc-attach-file-stream: no command that
// reaches lxc-attach may have a standard stream on a file. It is a QA tool, not deployed
// anywhere; scripts/qa-lxc-attach-streams.bash builds and runs it.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/lxcattach"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(lxcattach.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(lxcattach.ExitCannotRun)
	}
	return int(lxcattach.Main(args, newEnv(os.Stdout, os.Stderr, cwd)))
}

// newEnv is the command's view of the machine: the real file system and the shared discovery's
// tracked shell scripts.
func newEnv(stdout, stderr io.Writer, cwd string) lxcattach.Env {
	return lxcattach.Env{
		Stdout:  stdout,
		Stderr:  stderr,
		Cwd:     cwd,
		Resolve: filepath.EvalSymlinks,
		Open:    os.DirFS,
		Tracked: tracked,
	}
}

// tracked is the shared discovery's tracked-shell list (git's index, less deleted files and the
// excluded trees), repository-relative. The discovery bounds its own git processes.
func tracked(root string) ([]string, error) {
	var stdout, stderr bytes.Buffer
	denv := discovery.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}}
	args := []string{"--root", root, "--scripts", filepath.Join(root, "scripts"), "tracked-shell"}
	if code := discovery.Main(args, denv); code != discovery.ExitOK {
		return nil, fmt.Errorf("discovery of the tracked shell scripts failed: %s", strings.TrimSpace(strings.TrimPrefix(stderr.String(), "ERROR: ")))
	}
	var rels []string
	for rel := range strings.SplitSeq(stdout.String(), "\x00") {
		if rel != "" {
			rels = append(rels, rel)
		}
	}
	return rels, nil
}
