// Package cmdmain is what the QA commands' main packages share: refusing to run with a closed
// stdout, and writing the machine report where qa-all and other users can read it. A command's
// main stays thin; this is the part every one of them needs and none should copy.
package cmdmain

import (
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
