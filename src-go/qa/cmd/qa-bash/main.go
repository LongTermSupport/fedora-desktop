// Command qa-bash is the shell gate: every shell script git would publish must parse (bash -n) and
// have no shellcheck finding of level error or warning. It is a QA tool, not deployed anywhere;
// scripts/qa-bash.bash builds and runs it.
package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/bash"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if stdoutWasClosed() {
		fmt.Fprintln(os.Stderr, "ERROR: stdout is closed (>&-): the verdict cannot be delivered")
		return int(bash.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(bash.ExitCannotRun)
	}
	env := bash.Env{
		Getenv:    os.Getenv,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Cwd:       cwd,
		Runner:    proc.OS{},
		WriteFile: writeReport,
	}
	return int(bash.Main(args, env))
}

// devNullDevice is the device number of /dev/null on Linux (major 1, minor 3).
const devNullDevice = 1<<8 | 3

// stdoutWasClosed is whether fd 1 was closed when the command started. The Go runtime reopens a
// closed standard descriptor on /dev/null opened read-write, so the write would succeed and the
// verdict vanish; a shell's `>/dev/null` opens it write-only, which is how the two are told apart.
func stdoutWasClosed() bool {
	var st syscall.Stat_t
	if err := syscall.Fstat(syscall.Stdout, &st); err != nil {
		return true
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFCHR || st.Rdev != devNullDevice {
		return false
	}
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(syscall.Stdout), syscall.F_GETFL, 0)
	return errno == 0 && int(flags)&syscall.O_ACCMODE == syscall.O_RDWR
}

// reportMode is the report's mode: readable by other accounts, as the bash's umask made it.
const reportMode = 0o644

// writeReport writes the machine report where the caller said. The mode is set after the write
// so the umask, and an older file's mode, cannot change it.
func writeReport(path string, data []byte) error {
	if err := os.WriteFile(path, data, reportMode); err != nil {
		return err
	}
	return os.Chmod(path, reportMode)
}
