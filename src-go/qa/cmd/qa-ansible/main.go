// Command qa-ansible is the static Ansible QA gate: error-hiding directives, playbook hygiene,
// self-default variables, scope and guard declarations, deprecated fact variables and guessed
// runtime-directory uids. It is a QA tool, not deployed anywhere; scripts/qa-ansible.bash
// builds and runs it.
package main

import (
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ansible"
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
		WriteFile: writeReport,
	}
	return int(ansible.Main(args, env))
}

// writeReport writes the machine report where the caller said.
func writeReport(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
