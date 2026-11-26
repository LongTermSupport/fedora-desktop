// Package badroot creates through an os.Root without O_EXCL, every way the rule sees.
package badroot

import (
	"os"
	"syscall"
)

// Lock is the defect of the seat-record lock: a lock file opened O_RDWR|O_CREATE, so a dangling
// link at its name gets its target created.
func Lock(root *os.Root) (*os.File, error) {
	return root.OpenFile(".lock", os.O_RDWR|os.O_CREATE, 0o600) // want `os\.Root\.OpenFile creates without O_EXCL`
}

// NoFollow adds O_NOFOLLOW, which an os.Root ignores at the last name.
func NoFollow(root *os.Root) (*os.File, error) {
	return root.OpenFile(".lock", os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600) // want `os\.Root\.OpenFile creates without O_EXCL`
}

// Held keeps the flags in a local.
func Held(root *os.Root) (*os.File, error) {
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	return root.OpenFile("log", flags, 0o600) // want `os\.Root\.OpenFile creates without O_EXCL`
}

// Created is os.Root's Create, which opens O_CREATE|O_TRUNC.
func Created(root *os.Root) (*os.File, error) {
	return root.Create("out") // want `os\.Root\.Create creates without O_EXCL`
}

// Written is os.Root's WriteFile, which opens O_CREATE|O_TRUNC.
func Written(root *os.Root, data []byte) error {
	return root.WriteFile("out", data, 0o600) // want `os\.Root\.WriteFile creates without O_EXCL`
}

// Literal creates inside a function literal.
func Literal(root *os.Root) func() error {
	return func() error {
		return root.WriteFile("out", nil, 0o600) // want `os\.Root\.WriteFile creates without O_EXCL`
	}
}
