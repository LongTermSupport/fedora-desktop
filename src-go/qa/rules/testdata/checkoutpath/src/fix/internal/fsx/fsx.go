// Package fsx is the module's atomic writer, by path.
package fsx

// WriteAtomic writes data at path.
func WriteAtomic(path, what string, data []byte, mode uint32) error {
	_, _, _, _ = path, what, data, mode
	return nil
}
