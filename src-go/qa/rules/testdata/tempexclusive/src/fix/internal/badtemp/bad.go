// Package badtemp creates a temporary that is renamed into place without O_EXCL, every way the
// rule sees.
package badtemp

import (
	"fmt"
	"os"

	"fix/internal/execx"
	"fix/internal/tempio"
	"golang.org/x/sys/unix"
)

// WriteAtomic is the defect of fsx.WriteAtomic (fedora-desktop#417 review): a temporary named
// from the target and the pid, written by os.WriteFile, which follows a link planted there.
func WriteAtomic(path string, data []byte) error {
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil { // want `created without O_EXCL`
		return err
	}
	return os.Rename(tmp, path)
}

// Created is os.Create of a fixed suffix.
func Created(path string) error {
	f, err := os.Create(path + ".new") // want `created without O_EXCL`
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(path+".new", path)
}

// Opened is OpenFile with O_CREATE and O_TRUNC but no O_EXCL.
func Opened(path string) error {
	part := path + ".partial"
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // want `created without O_EXCL`
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(part, path)
}

// Linked is a temporary put in place by a hard link.
func Linked(path string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, nil, 0o600); err != nil { // want `created without O_EXCL`
		return err
	}
	if err := os.Link(tmp, path); err != nil {
		return err
	}
	return os.Remove(tmp)
}

// InRoot is the same through an os.Root.
func InRoot(root *os.Root, name string) error {
	tmp := name + ".tmp"
	if err := root.WriteFile(tmp, nil, 0o600); err != nil { // want `created without O_EXCL`
		return err
	}
	return root.Rename(tmp, name)
}

// RootCreate is os.Root's Create.
func RootCreate(root *os.Root, name string) error {
	tmp := name + ".tmp"
	f, err := root.Create(tmp) // want `created without O_EXCL`
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return root.Rename(tmp, name)
}

// FlagsHeld keeps the flags in a variable.
func FlagsHeld(path string) error {
	flags := os.O_WRONLY | os.O_CREATE
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, flags, 0o600) // want `created without O_EXCL`
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Openat is the raw call.
func Openat(dir int, name string) error {
	tmp := name + ".tmp"
	fd, err := unix.Openat(dir, tmp, unix.OWronly|unix.OCreat|unix.OTrunc, 0o600) // want `created without O_EXCL`
	if err != nil {
		return err
	}
	_ = fd
	return unix.Renameat(dir, tmp, dir, name)
}

// InClosure is a function literal.
var InClosure = func(path string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, nil, 0o600); err != nil { // want `created without O_EXCL`
		return err
	}
	return os.Rename(tmp, path)
}

// copyFile is the helper of ftp-camera's copy: it writes its destination O_TRUNC.
func copyFile(src, dst string) error { // want copyFile:`creates its parameters \[1\] without O_EXCL`
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

// CopyIntoMount is ftp-camera's copy: the temporary is created by a helper of the package.
func CopyIntoMount(src, destDir, name string) error {
	tmp := destDir + "/.tmp." + name
	if err := copyFile(src, tmp); err != nil { // want `created without O_EXCL`
		return err
	}
	return os.Rename(tmp, destDir+"/"+name)
}

// Imported is a helper of another package.
func Imported(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := tempio.CopyTo(data, tmp); err != nil { // want `created without O_EXCL`
		return err
	}
	return os.Rename(tmp, path)
}

// Download is vmtest's fetch: curl writes the temporary.
func Download(url, dest string) error {
	partial := dest + ".partial"
	args := []string{"-fL", "-o", partial}
	args = append(args, url)
	if err := execx.Run(execx.Cmd{Name: "curl", Args: args}); err != nil { // want `created without O_EXCL`
		return err
	}
	return os.Rename(partial, dest)
}

// Resample is wsi's sox: the output is named in the literal.
func Resample(wav string) error {
	out := wav + ".resampled.wav"
	if err := execx.Run(execx.Cmd{Name: "sox", Args: []string{wav, out}}); err != nil { // want `created without O_EXCL`
		return err
	}
	return os.Rename(out, wav)
}
