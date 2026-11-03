// Command qa-untracked-audit is the gate for work kept only where git does not carry it: old
// files in untracked/, unpushed branches, old dirty work trees and stashes (Plan 00245). It is a
// QA tool, not deployed anywhere; scripts/qa-untracked-audit.bash builds and runs it.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/untrackedaudit"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if cmdmain.RefuseClosedStdout() {
		return int(untrackedaudit.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(untrackedaudit.ExitCannotRun)
	}
	env := untrackedaudit.Env{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Cwd:     cwd,
		Resolve: filepath.EvalSymlinks,
		Open:    os.DirFS,
		Git:     proc.OS{},
		Now:     time.Now,
		Mtime:   mtime,
	}
	return int(untrackedaudit.Main(args, env))
}

// mtime is the modification time of a path, without following a final symlink.
func mtime(path string) (time.Time, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("lstat: %w", err)
	}
	return info.ModTime(), nil
}
