package ccylaunch

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
)

// hashLen is how many hex digits of the md5 the launcher keeps.
const hashLen = 16

// hashLinePrefix starts the lines the launcher leaves out of its own hash: the assignment that
// holds the hash cannot be part of what it hashes.
var hashLinePrefix = []byte("CCY_HASH=")

// IntegrityHash is CCY_HASH as the bash launcher computes it (the rule its comment above
// CCY_HASH explains): the first 16 hex digits of the md5 of the launcher's text with every line
// that starts with CCY_HASH= left out, each kept line ending in a newline, followed by every
// regular file directly in libDir named *.bash or *.py, in byte order of name. A link is not a
// regular file. libs is how many of those are *.bash, the count the launcher refuses at zero.
// The Go front prints this same hash, so a Quick Launch file the bash wrote compares with it.
func IntegrityHash(bashText []byte, libDir string) (string, int, error) {
	h := md5.New()
	lines := bytes.Split(bashText, []byte("\n"))
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	for _, line := range lines {
		if bytes.HasPrefix(line, hashLinePrefix) {
			continue
		}
		h.Write(line)
		h.Write([]byte("\n"))
	}
	entries, err := os.ReadDir(libDir)
	if err != nil {
		return "", 0, fmt.Errorf("read the launcher's libraries: %w", err)
	}
	var names []string
	libs := 0
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if !e.Type().IsRegular() || (ext != ".bash" && ext != ".py") {
			continue
		}
		names = append(names, e.Name())
		if ext == ".bash" {
			libs++
		}
	}
	slices.Sort(names)
	for _, name := range names {
		if err := appendFile(h, filepath.Join(libDir, name)); err != nil {
			return "", 0, err
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:hashLen], libs, nil
}

func appendFile(w io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	_, copyErr := io.Copy(w, f)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("read %s: %w", path, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", path, closeErr)
	}
	return nil
}
