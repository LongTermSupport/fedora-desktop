package fsx

import (
	"errors"
	"fmt"
	"os"
)

// WriteAtomic stores data at path, written to a temporary beside it and renamed into place so a
// reader never sees half a file. what names the file in the errors ("session record"). The
// temporary is path plus ".tmp.<pid>"; readers that list a directory skip names containing
// ".tmp.".
func WriteAtomic(path, what string, data []byte, mode os.FileMode) error {
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, data, mode); err != nil {
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
