// Package tempio writes a file at a path its caller names.
package tempio

import "os"

// CopyTo writes data at dst, truncating what is there.
func CopyTo(data []byte, dst string) error { // want CopyTo:`creates its parameters \[1\] without O_EXCL`
	return os.WriteFile(dst, data, 0o600)
}

// CopyExclusive writes data at dst only when nothing is there.
func CopyExclusive(data []byte, dst string) error {
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Close()
}
