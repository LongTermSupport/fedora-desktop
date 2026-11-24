// Package fsx is the file writes the ccy tools share: a file replaced whole or not at all, and
// a removal that does not mind the file already being gone.
package fsx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// WriteAtomic stores data at path, written to a temporary beside it and renamed into place so a
// reader never sees half a file. what names the file in the errors ("session record"). The
// temporary is path plus ".tmp.<random>", created exclusively (os.CreateTemp), so a link
// planted at a name another party could predict is never followed (rule
// project_temp_not_exclusive, issue 417); readers that list a directory skip names containing
// ".tmp.".
func WriteAtomic(path, what string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("could not write the %s %s: %w", what, path, err)
	}
	tmp := f.Name()
	err = f.Chmod(mode)
	if err == nil {
		_, err = f.Write(data)
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return errors.Join(fmt.Errorf("could not write the %s %s: %w", what, path, err), RemoveIfPresent(tmp))
	}
	if err := os.Rename(tmp, path); err != nil {
		return errors.Join(fmt.Errorf("could not move the %s into place at %s: %w", what, path, err), RemoveIfPresent(tmp))
	}
	return nil
}

// RemoveIfPresent removes path; a file that is already gone is success.
func RemoveIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
