package checkoutpin

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
)

// The checkout files the host launcher reads, writes or binds besides the pinned ones (Plan 00266
// review findings 1 and 6), and the one way it touches them:
//
//   - C1. ReadFile reads a checkout file through an os.Root of the checkout: a link at the name,
//     anything but a regular file, and a linked directory that leaves the checkout are refused,
//     the file named, and never followed.
//   - C2. WriteFile replaces a checkout file whole: an exclusive temporary beside it, renamed into
//     place, its directories made inside the checkout. A link at the name is refused, never
//     followed and never replaced, so a link a container planted is reported, not written
//     through; a linked directory that leaves the checkout is refused by the os.Root.
//   - C3. CopyOut reads a checkout file as ReadFile does and writes its bytes into a new private
//     file of a host directory: what the launcher binds into a container is then the bytes it
//     read, not a checkout path a container (a live one of the same checkout included) can turn
//     into a link between the check and the bind.
const (
	// EnvLocal is the checkout's own ccy settings, placed by the install's IaC and bound
	// read-only into its sessions (CopyOut).
	EnvLocal = ".claude/ccy/ccy.env.local"
	// Dist is the tracked template of EnvLocal, which the launcher keeps current (WriteFile).
	Dist = ".claude/ccy/ccy.env.local.dist"
	// Gitignore keeps the session data under .claude/ccy out of git (WriteFile).
	Gitignore = ".claude/ccy/.gitignore"
)

// sharedFileMode is a file a shell redirection would make: the umask decides.
const sharedFileMode = 0o666

// ReadFile is C1: rel's content in checkout, and whether it is there.
func ReadFile(checkout, rel string) (content []byte, present bool, err error) {
	root, err := os.OpenRoot(checkout)
	if err != nil {
		return nil, false, err
	}
	defer closeRoot(root, &err)
	rel = path.Clean(rel)
	if err := refuseLink(root, checkout, rel); err != nil {
		return nil, false, err
	}
	content, present, err = readPinned(root, rel)
	return content, present, wrapRel(checkout, rel, err)
}

// refuseLink is an error naming rel when a link is at it, or when it cannot be looked at (an
// absent rel is no error: the caller decides what absent means).
func refuseLink(root *os.Root, checkout, rel string) error {
	info, err := root.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return wrapRel(checkout, rel, err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return nil
	}
	return fmt.Errorf("%s/%s is a link, which a container session can put there; ccy reads and writes it only as a plain file, so remove the link", checkout, rel)
}

// WriteFile is C2: rel in checkout replaced by text.
func WriteFile(checkout, rel string, text []byte) (err error) {
	rel = path.Clean(rel)
	root, err := os.OpenRoot(checkout)
	if err != nil {
		return err
	}
	defer closeRoot(root, &err)
	if err := root.MkdirAll(path.Dir(rel), 0o777); err != nil {
		return wrapRel(checkout, rel, err)
	}
	if err := refuseLink(root, checkout, rel); err != nil {
		return err
	}
	info, err := root.Lstat(rel)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return wrapRel(checkout, rel, err)
	case !info.Mode().IsRegular():
		return fmt.Errorf("%s/%s is not a regular file (%s); remove it", checkout, rel, info.Mode().Type())
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return err
	}
	tmp := rel + ".tmp." + hex.EncodeToString(suffix[:])
	if err := writeNewMode(root, tmp, text, sharedFileMode); err != nil {
		return errors.Join(wrapRel(checkout, rel, err), removeIfThere(root, tmp))
	}
	if err := root.Rename(tmp, rel); err != nil {
		return errors.Join(wrapRel(checkout, rel, err), removeIfThere(root, tmp))
	}
	return nil
}

// CopyOut is C3: rel of checkout copied into dir under its own name, the copy's path, and
// whether there was a file to copy.
func CopyOut(checkout, rel, dir string) (copied string, present bool, err error) {
	content, present, err := ReadFile(checkout, rel)
	if err != nil || !present {
		return "", false, err
	}
	copied, err = writeCopy(dir, path.Base(rel), content)
	if err != nil {
		return "", false, fmt.Errorf("could not copy %s/%s into %s: %w", checkout, rel, dir, err)
	}
	return copied, true, nil
}

// writeCopy writes content into dir as a new private (0600) file name, created exclusively
// through an os.Root of dir, and is its path.
func writeCopy(dir, name string, content []byte) (copied string, err error) {
	into, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer closeRoot(into, &err)
	if err := writeNewMode(into, name, content, 0o600); err != nil {
		return "", err
	}
	return into.Name() + "/" + name, nil
}

// writeNewMode creates name exclusively with perm and writes text into it.
func writeNewMode(root *os.Root, name string, text []byte, perm os.FileMode) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	_, err = f.Write(text)
	return errors.Join(err, f.Close())
}

// wrapRel names the checkout file an error is about.
func wrapRel(checkout, rel string, err error) error {
	if err == nil || strings.HasPrefix(err.Error(), checkout+"/"+rel+" ") {
		return err
	}
	return fmt.Errorf("%s/%s: %w", checkout, rel, err)
}
