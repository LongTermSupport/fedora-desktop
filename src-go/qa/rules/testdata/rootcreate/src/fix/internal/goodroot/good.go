// Package goodroot creates through an os.Root only exclusively, and opens what exists without
// O_CREATE.
package goodroot

import (
	"errors"
	"io/fs"
	"os"
)

const exclusive = os.O_RDWR | os.O_CREATE | os.O_EXCL

// Lock creates the lock exclusively, which refuses any link at the name, and opens one that is
// already there without O_CREATE, so a dangling link is an error rather than a new file.
func Lock(root *os.Root) (*os.File, error) {
	f, err := root.OpenFile(".lock", os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return root.OpenFile(".lock", os.O_RDWR, 0)
	}
	return f, err
}

// Constant spells O_EXCL through a constant.
func Constant(root *os.Root) (*os.File, error) {
	return root.OpenFile("new", exclusive, 0o600)
}

// Read opens for reading only.
func Read(root *os.Root) (*os.File, error) {
	return root.OpenFile("in", os.O_RDONLY, 0)
}

// Outside is os.OpenFile, which honours O_NOFOLLOW and is not this rule's.
func Outside(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
}

// Passed takes its flags as a parameter, which the rule cannot read.
func Passed(root *os.Root, flags int) (*os.File, error) {
	return root.OpenFile("any", flags, 0o600)
}
