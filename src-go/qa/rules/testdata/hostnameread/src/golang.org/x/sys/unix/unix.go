// Package unix is a stand-in for golang.org/x/sys/unix, with the one function the rule names.
package unix

// Utsname is the kernel's identification of the machine, its node name among it.
type Utsname struct{ Nodename [65]byte }

// Uname fills buf.
func Uname(_ *Utsname) error { return nil }
