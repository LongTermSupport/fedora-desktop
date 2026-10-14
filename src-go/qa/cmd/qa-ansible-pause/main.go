// Command qa-ansible-pause is the rule pause-without-register: an ansible.builtin.pause task needs
// a register or a PAUSE-OK reason. It is a QA tool, not deployed anywhere;
// scripts/qa-ansible-pause.bash builds and runs it.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ansiblepause"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(ansiblepause.ExitCannotRun)
	}
	env := ansiblepause.Env{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Cwd:     cwd,
		Resolve: filepath.EvalSymlinks,
		Open:    os.DirFS,
		Yaml:    yaml,
	}
	return int(ansiblepause.Main(args, env))
}

// yaml is the shared discovery's YAML list (what git would publish, less the excluded trees) as
// paths relative to the root. The discovery bounds its own git processes.
func yaml(root string) ([]string, error) {
	var stdout, stderr bytes.Buffer
	denv := discovery.Env{Stdout: &stdout, Stderr: &stderr, Cwd: root, Runner: proc.OS{}}
	args := []string{"--root", root, "--scripts", filepath.Join(root, "scripts"), "yaml"}
	if code := discovery.Main(args, denv); code != discovery.ExitOK {
		return nil, fmt.Errorf("discovery of YAML files failed: %s", strings.TrimSpace(strings.TrimPrefix(stderr.String(), "ERROR: ")))
	}
	var rels []string
	for abs := range strings.SplitSeq(stdout.String(), "\x00") {
		if abs != "" {
			rels = append(rels, strings.TrimPrefix(abs, root+"/"))
		}
	}
	return rels, nil
}
