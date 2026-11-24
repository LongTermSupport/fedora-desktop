// Package goodtemp puts files in place the ways the rule does not judge.
package goodtemp

import (
	"os"
	"path/filepath"

	"fix/internal/execx"
	"fix/internal/tempio"
	"golang.org/x/sys/unix"
)

// CreateTemp is the remedy: a random name created exclusively beside the target.
func CreateTemp(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Exclusive is OpenFile with O_EXCL.
func Exclusive(path string) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ExclusiveHeld holds the flags, O_EXCL among them, in a constant.
func ExclusiveHeld(root *os.Root, name string) error {
	const flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	tmp := name + ".tmp"
	f, err := root.OpenFile(tmp, flags, 0o600)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return root.Rename(tmp, name)
}

// OpenatExcl is the raw call with O_EXCL.
func OpenatExcl(dir int, name string) error {
	tmp := name + ".tmp"
	if _, err := unix.Openat(dir, tmp, unix.OWronly|unix.OCreat|unix.OExcl|unix.OCloexec, 0o600); err != nil {
		return err
	}
	return unix.Renameat(dir, tmp, dir, name)
}

// InPlace writes the target itself, which is never renamed.
func InPlace(path string) error { // want InPlace:`creates its parameters \[0\] without O_EXCL`
	return os.WriteFile(path, nil, 0o600)
}

// Exclusively is a helper of another package that creates exclusively.
func Exclusively(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := tempio.CopyExclusive(data, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// InPrivateDir has the child write inside a directory only this process made.
func InPrivateDir(url, dest string) error {
	dir, err := os.MkdirTemp(filepath.Dir(dest), ".fetch-*")
	if err != nil {
		return err
	}
	partial := filepath.Join(dir, "partial")
	if err := execx.Run(execx.Cmd{Name: "curl", Args: []string{"-o", partial, url}}); err != nil {
		return err
	}
	if err := os.Rename(partial, dest); err != nil {
		return err
	}
	return os.Remove(dir)
}

// Rotated renames an existing file aside; nothing here created it.
func Rotated(path string) error { // want Rotated:`creates its parameters \[0\] without O_EXCL`
	if err := os.Rename(path, path+".old"); err != nil {
		return err
	}
	return os.WriteFile(path, nil, 0o600)
}
