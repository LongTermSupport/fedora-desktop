package kit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// rename moves the assembled staging directory into place; a test replaces it.
var rename = os.Rename

// Staging is the sibling directory kit is assembled in before it is renamed into place. Its
// name is fixed, so a run that was killed leaves one known directory the next run clears.
func Staging(kit paths.Kit) string { return kit.String() + ".assembling" }

// Parts is every file and directory of the kit, relative to its directory, in the order they
// are made.
func Parts(kit paths.Kit) []string {
	list := names(kit)
	parts := make([]string, len(list))
	for i, e := range list {
		parts[i] = e.path
	}
	return parts
}

// Missing is the parts not at kit's directory as the kind they must be (a directory, or a
// regular file, never a link); every part when the directory itself is not there.
func Missing(kit paths.Kit) []string {
	var missing []string
	for _, e := range names(kit) {
		info, err := os.Lstat(filepath.Join(kit.String(), e.path))
		if err != nil || !kindIs(info, e.content == nil) {
			missing = append(missing, e.path)
		}
	}
	return missing
}

// names is the kit's entries where only their names and kinds are read, never their content.
func names(kit paths.Kit) []entry { return entries(kit, bytes.NewReader(nil)) }

// same is whether every file of the kit at kit's directory holds the bytes this build would
// write there, the binary's being own: a kit judged by its parts' presence alone keeps a binary
// another toolchain built from the same source.
func same(kit paths.Kit, own []byte) bool {
	for _, e := range entries(kit, bytes.NewReader(own)) {
		if e.content == nil {
			continue
		}
		want, err := io.ReadAll(io.LimitReader(e.content, e.limit+1))
		if err != nil {
			return false
		}
		got, err := os.ReadFile(filepath.Join(kit.String(), e.path))
		if err != nil || !bytes.Equal(got, want) {
			return false
		}
	}
	return true
}

func kindIs(info fs.FileInfo, dir bool) bool {
	if dir {
		return info.IsDir()
	}
	return info.Mode().IsRegular()
}

// Place makes kit's directory a whole kit, assembled from binary. A whole kit already there is
// left alone (placed is false). Otherwise the kit is assembled in Staging, cleared first, and
// renamed into place, after a kit that is not whole is removed; so the kit's path holds either
// no kit or a whole one, and a failure at any point is repaired by the next run.
func Place(kit paths.Kit, binary io.Reader) (placed bool, err error) {
	final := kit.String()
	own, err := io.ReadAll(io.LimitReader(binary, maxBinary+1))
	if err != nil {
		return false, fmt.Errorf("reading the binary for the kit: %w", err)
	}
	if _, err := os.Lstat(final); err == nil && len(Missing(kit)) == 0 && same(kit, own) {
		return false, nil
	}
	binary = bytes.NewReader(own)
	staging := Staging(kit)
	if err := os.RemoveAll(staging); err != nil {
		return false, fmt.Errorf("clearing the leftover staging %s: %w", staging, err)
	}
	if err := os.Mkdir(staging, dirMode); err != nil {
		return false, fmt.Errorf("making the staging %s: %w", staging, err)
	}
	if err := Assemble(staging, kit, binary); err != nil {
		return false, err
	}
	if err := os.RemoveAll(final); err != nil {
		return false, fmt.Errorf("removing the kit that is not whole at %s: %w", final, err)
	}
	if err := rename(staging, final); err != nil {
		return false, fmt.Errorf("renaming the kit into place at %s: %w", final, err)
	}
	return true, syncDir(filepath.Dir(final))
}

// syncDir makes the rename in path durable.
func syncDir(path string) error {
	fd, err := unix.Open(path, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return &fs.PathError{Op: "open", Path: path, Err: err}
	}
	return errors.Join(unix.Fsync(fd), unix.Close(fd))
}
