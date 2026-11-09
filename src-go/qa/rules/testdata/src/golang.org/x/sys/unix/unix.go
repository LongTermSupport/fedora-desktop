// Package unix is the fixtures' stand-in for golang.org/x/sys/unix: the calls that change an
// owner, with the real signatures.
package unix

// Chown changes the owner of path.
func Chown(_ string, _, _ int) error { return nil }

// Lchown changes the owner of path, never following a link.
func Lchown(_ string, _, _ int) error { return nil }

// Fchown changes the owner of an open descriptor.
func Fchown(_, _, _ int) error { return nil }

// Fchownat changes the owner of path in the directory dirfd.
func Fchownat(_ int, _ string, _, _, _ int) error { return nil }
