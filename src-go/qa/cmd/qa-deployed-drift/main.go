// Command qa-deployed-drift is the deployed-drift QA gate: repo-owned files that a play deploys
// against their deployed copies on the host. It is a QA tool, not deployed anywhere;
// scripts/qa-deployed-drift.bash builds and runs it.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/deployeddrift"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(deployeddrift.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(deployeddrift.ExitCannotRun)
	}
	env := deployeddrift.Env{
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
		Cwd:            cwd,
		Home:           os.Getenv("HOME"),
		Open:           os.DirFS,
		Resolve:        filepath.EvalSymlinks,
		Host:           os.DirFS("/"),
		LinkedWorktree: linkedWorktree,
	}
	return int(deployeddrift.Main(args, env))
}

// gitTimeout bounds each git question.
const gitTimeout = time.Minute

// linkedWorktree is true when root's git directory differs from the repository's common one,
// which is what a linked worktree is. Git failing to answer is an error, not "not linked".
func linkedWorktree(root string) (bool, error) {
	gitDir, err := gitOutput(root, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return false, err
	}
	common, err := gitOutput(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return false, err
	}
	return gitDir != common, nil
}

func gitOutput(root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	res, err := proc.OS{}.Run(ctx, proc.Cmd{Argv: append([]string{"git"}, args...), Dir: root})
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	if res.Code != 0 {
		return "", fmt.Errorf("git %s exited %d: %s", strings.Join(args, " "), res.Code, strings.TrimSpace(res.Stderr))
	}
	return strings.TrimSpace(res.Stdout), nil
}
