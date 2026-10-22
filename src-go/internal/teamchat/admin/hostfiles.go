package admin

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// maxHostFile caps the owner group file, the pin and /etc/os-release.
const maxHostFile = 4096

// osRelease is the distribution's identification, under the host root (C 3.5.2 step 1).
const osRelease = "/etc/os-release"

var groupNameRE = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// hostOpen is how root opens a file it decides from: never following a link, never blocking on
// a FIFO or a device, and not inherited by a child.
const hostOpen = os.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC

// readRootFile is path's content when it is a regular file owned by owner and writable by
// neither group nor other, at most maxHostFile bytes; found is false when there is none.
func readRootFile(path string, owner uint32, flags int) ([]byte, bool, error) {
	f, err := os.OpenFile(filepath.Clean(path), flags, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cannot open %s: %w", path, err)
	}
	data, err := readOwned(f, path, owner)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("cannot close %s: %w", path, cerr)
	}
	return data, err == nil, err
}

func readOwned(f *os.File, path string, owner uint32) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("cannot stat %s: %w", path, err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	switch {
	case !info.Mode().IsRegular() || !ok:
		return nil, fmt.Errorf("%s is not a regular file", path)
	case st.Uid != owner:
		return nil, fmt.Errorf("%s is owned by uid %d, not %d", path, st.Uid, owner)
	case info.Mode().Perm()&0o022 != 0:
		return nil, fmt.Errorf("%s is writable by its group or by others", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxHostFile+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	if len(data) > maxHostFile {
		return nil, fmt.Errorf("%s is over %d bytes", path, maxHostFile)
	}
	return data, nil
}

// readConfigFile is readRootFile with every failure, a missing file included, the install's
// (78).
func readConfigFile(path string, owner uint32, flags int) ([]byte, error) {
	data, found, err := readRootFile(path, owner, flags)
	if err == nil && !found {
		err = fmt.Errorf("there is no %s: run %s", path, paths.PlayFile)
	}
	if err != nil {
		return nil, &configError{err.Error()}
	}
	return data, nil
}

// OwnerGroupFile is the owner group's gid (HF1): paths.OwnerGroupFile under root, root-owned,
// one group name and at most one newline, looked up by name. Every failure is 78.
func OwnerGroupFile(root string, lookupGroup func(name string) (int, error)) func() (int, error) {
	return ownerGroupFile(root, lookupGroup, 0)
}

func ownerGroupFile(root string, lookupGroup func(string) (int, error), owner uint32) func() (int, error) {
	return func() (int, error) {
		path := root + paths.OwnerGroupFile
		raw, err := readConfigFile(path, owner, hostOpen)
		if err != nil {
			return 0, err
		}
		name := strings.TrimSuffix(string(raw), "\n")
		if !groupNameRE.MatchString(name) {
			return 0, &configError{path + " does not hold one group name"}
		}
		gid, err := lookupGroup(name)
		if err != nil {
			return 0, &configError{fmt.Sprintf("the owner group %s from %s: %v", name, path, err)}
		}
		return gid, nil
	}
}

// LookupGroup is a local group's gid by name.
func LookupGroup(name string) (int, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, err
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("group %s has gid %q: %w", name, g.Gid, err)
	}
	return gid, nil
}

// ReadPin is the homeserver pin (HF2, n2f-D3): paths.ServerPinFile under root, root-owned,
// strict JSON whose values render judges. Every failure is 78.
func ReadPin(root string) (host.Pin, error) { return readPin(root, 0) }

func readPin(root string, owner uint32) (host.Pin, error) {
	path := root + paths.ServerPinFile
	raw, err := readConfigFile(path, owner, hostOpen)
	if err != nil {
		return host.Pin{}, err
	}
	pin, err := decodePin(raw)
	if err != nil {
		return host.Pin{}, &configError{fmt.Sprintf("%s: %v", path, err)}
	}
	return pin, nil
}

func decodePin(raw []byte) (host.Pin, error) {
	obj, err := strictObject(raw, "v", "version", "memory_high", "cache_capacity_modifier")
	if err != nil {
		return host.Pin{}, err
	}
	if err = checkMajor(obj["v"]); err != nil {
		return host.Pin{}, err
	}
	for _, key := range []string{"version", "memory_high", "cache_capacity_modifier"} {
		if obj[key].Kind() != proto.KindString {
			return host.Pin{}, fmt.Errorf("%s is not a string", key)
		}
	}
	v, err := paths.ParseServerVersion(obj["version"].Str())
	if err != nil {
		return host.Pin{}, err
	}
	return host.Pin{Version: v, MemoryHigh: obj["memory_high"].Str(), CacheCapacityModifier: obj["cache_capacity_modifier"].Str()}, nil
}

// checkFedora is C 3.5.2 step 1 (HF3): root's /etc/os-release, owned by owner and writable by
// neither group nor other, says ID=fedora, quoted or not. The distribution ships the file as a
// link into /usr/lib, so this one read follows a link.
func checkFedora(root string, owner uint32) error {
	raw, err := readConfigFile(root+osRelease, owner, hostOpen&^unix.O_NOFOLLOW)
	if err != nil {
		return &configError{"team apply needs Fedora: " + err.Error()}
	}
	for line := range strings.Lines(string(raw)) {
		value, ok := strings.CutPrefix(strings.TrimSuffix(line, "\n"), "ID=")
		if ok && (value == "fedora" || value == `"fedora"` || value == "'fedora'") {
			return nil
		}
	}
	return &configError{"team apply needs Fedora: " + osRelease + " does not say ID=fedora"}
}

// rootOwner is the owner root's own files have under root: uid 0 on the host (root ""), and
// the owner of a test root, whose files the test wrote.
func rootOwner(root string) (uint32, error) {
	if root == "" {
		return 0, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		return 0, &configError{err.Error()}
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, &configError{"the owner of " + root + " is not known"}
	}
	return st.Uid, nil
}
