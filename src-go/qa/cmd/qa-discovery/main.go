// Command qa-discovery prints the files a QA gate reads: the repo-owned shell scripts, Python
// files and Python templates git would publish, and the tracked files a gate measures its
// coverage against. It is a QA tool, not deployed anywhere; the gates import
// src-go/qa/discovery directly.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/cmdmain"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if cmdmain.RefuseClosedStdout() {
		return int(discovery.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return int(discovery.Fail(stderr, fmt.Errorf("working directory: %w", err)))
	}
	return int(discovery.Main(args, discovery.Env{Stdout: stdout, Stderr: stderr, Cwd: cwd, Runner: proc.OS{}}))
}
