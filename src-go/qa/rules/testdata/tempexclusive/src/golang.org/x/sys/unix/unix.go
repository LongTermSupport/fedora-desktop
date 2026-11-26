// Package unix is a stand-in for golang.org/x/sys/unix, with the calls and flags the rule names.
package unix

// The open flags, as Linux numbers them (the real names are O_WRONLY and so on; the stand-in
// spells them so the linter passes it, and the rule judges the value).
const (
	OWronly  = 0x1
	OCreat   = 0x40
	OExcl    = 0x80
	OTrunc   = 0x200
	OCloexec = 0x80000
)

// Openat opens path relative to dirfd.
func Openat(_ int, _ string, _ int, _ uint32) (int, error) { return 0, nil }

// Renameat renames oldpath to newpath, each relative to its directory.
func Renameat(_ int, _ string, _ int, _ string) error { return nil }
