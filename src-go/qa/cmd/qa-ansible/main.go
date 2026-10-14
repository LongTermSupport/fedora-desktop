// Command qa-ansible is the static Ansible QA gate: error-hiding directives, playbook hygiene,
// self-default variables, scope and guard declarations, deprecated fact variables and guessed
// runtime-directory uids. It is a QA tool, not deployed anywhere; scripts/qa-ansible.bash
// builds and runs it.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ansible"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(ansible.ExitCannotRun)
	}
	env := ansible.Env{
		Getenv:    os.Getenv,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Cwd:       cwd,
		Open:      os.DirFS,
		Ignored:   gitIgnored,
		WriteFile: writeReport,
	}
	return int(ansible.Main(args, env))
}

// gitIgnoredTimeout bounds the git listing.
const gitIgnoredTimeout = 2 * time.Minute

// gitIgnored lists the gitignored files under the scanned directories of a root, relative to it:
// the gate judges what git would publish (tracked, or untracked and not ignored), nothing else.
func gitIgnored(root string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitIgnoredTimeout)
	defer cancel()
	argv := append([]string{"git", "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--"}, ansible.SearchDirs...)
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

// writeReport writes the machine report where the caller said.
func writeReport(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
