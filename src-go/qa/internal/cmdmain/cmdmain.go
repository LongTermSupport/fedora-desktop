// Package cmdmain is what the QA commands' main packages share: refusing to run with a closed
// stdout, and writing the machine report where qa-all and other users can read it. A command's
// main stays thin; this is the part every one of them needs and none should copy.
package cmdmain

import (
	"fmt"
	"os"
	"syscall"
)

// devNullDevice is the device number of /dev/null on Linux (major 1, minor 3).
const devNullDevice = 1<<8 | 3

// StdoutWasClosed is whether fd 1 was closed when the command started. The Go runtime reopens a
// closed standard descriptor on /dev/null opened read-write, so a write would succeed and the
// verdict vanish; a shell's `>/dev/null` opens it write-only, which is how the two are told apart.
func StdoutWasClosed() bool {
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

// closedStdoutMessage is what a gate says, on stderr, when it is refused for a closed stdout.
const closedStdoutMessage = "ERROR: stdout is closed (>&-): the verdict cannot be delivered"

// RefuseClosedStdout says on stderr that the command cannot run when stdout was closed at start,
// and reports whether it did; the caller then exits with its cannot-run code.
func RefuseClosedStdout() bool {
	if !StdoutWasClosed() {
		return false
	}
	fmt.Fprintln(os.Stderr, closedStdoutMessage)
	return true
}

// privateReportMode is the permission of a report that only its writer reads back.
const privateReportMode = 0o600

// WriteReportPrivate writes the machine report readable by its owner only, as the gates whose
// report is read back by the same account do. The umask can only narrow it further.
func WriteReportPrivate(path string, data []byte) error {
	return os.WriteFile(path, data, privateReportMode)
}

// reportMode is the report's permission: it holds repository paths and finding text, no secrets.
const reportMode = 0o644

// WriteReport writes the machine report where the caller said.
func WriteReport(path string, data []byte) error {
	if err := os.WriteFile(path, data, reportMode); err != nil {
		return err
	}
	// The umask must not narrow it: other users (a CI step, qa-all) read the report.
	return os.Chmod(path, reportMode)
}
