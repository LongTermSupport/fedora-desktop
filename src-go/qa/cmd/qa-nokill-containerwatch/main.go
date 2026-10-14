// Command qa-nokill-containerwatch is the L0 no-kill gate for the container-watch watchdog:
// no executable call site may signal a process or end a container, bar the one audited stop in
// containment.py. It is a QA tool, not deployed anywhere; scripts/qa-nokill-containerwatch.bash
// builds and runs it.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/nokill"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(nokill.ExitCannotRun)
	}
	env := nokill.Env{
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Cwd:       cwd,
		Open:      os.DirFS,
		Published: published,
	}
	return int(nokill.Main(args, env))
}

// gitTimeout bounds the git listing.
const gitTimeout = 2 * time.Minute

// published lists the files git would publish (tracked, or untracked and not ignored) under the
// watchdog's directories: the gate judges nothing else.
func published(root string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	argv := append([]string{"git", "--literal-pathspecs", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--"}, nokill.ScanPaths()...)
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
