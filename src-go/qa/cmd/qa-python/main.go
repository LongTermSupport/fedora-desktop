// Command qa-python is the Python gate: every Python file git would publish must compile and pass
// ruff, and every Python template must compile once its expressions are substituted. It is a QA
// tool, not deployed anywhere; scripts/qa-python.bash builds and runs it.
package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/python"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if stdoutWasClosed() {
		fmt.Fprintln(os.Stderr, "ERROR: stdout is closed (>&-): the verdict cannot be delivered")
		return int(python.ExitCannotRun)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: working directory: %v\n", err)
		return int(python.ExitCannotRun)
	}
	env := python.Env{
		Getenv:    os.Getenv,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Cwd:       cwd,
		Runner:    proc.OS{},
		TempDir:   os.TempDir(),
		WriteFile: writeReport,
	}
	return int(python.Main(args, env))
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

// writeReport writes the machine report where the caller said.
func writeReport(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
