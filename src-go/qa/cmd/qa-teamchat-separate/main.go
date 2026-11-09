// Command qa-teamchat-separate is the gate teamchat-keeps-off-v1: no file v2 owns holds a v1
// name (Plan 00209 D 4.1). It is a QA tool, not deployed anywhere; scripts/qa-teamchat-separate.bash
// builds and runs it.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/teamchatseparate"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(teamchatseparate.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(teamchatseparate.ExitCannotRun)
	}
	env := teamchatseparate.Env{
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Cwd:       cwd,
		Resolve:   filepath.EvalSymlinks,
		Open:      os.DirFS,
		Published: published,
	}
	return int(teamchatseparate.Main(args, env))
}

// gitTimeout bounds the git listing.
const gitTimeout = 2 * time.Minute

// published lists the files git would publish (tracked, or untracked and not ignored) under the
// scan paths: the gate judges nothing else.
func published(root string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	argv := append([]string{"git", "--literal-pathspecs", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--"}, teamchatseparate.ScanPaths()...)
	res, err := proc.OS{}.Run(ctx, proc.Cmd{Argv: argv, Dir: root})
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("git ls-files exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	var files []string
	for p := range strings.SplitSeq(res.Stdout, "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	return files, nil
}
