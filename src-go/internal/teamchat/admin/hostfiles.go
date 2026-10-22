package admin

import (
	"fmt"
	"os"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// maxHostFile caps the owner group file, the pin and /etc/os-release.
const maxHostFile = 4096

// osRelease is the distribution's identification, under the host root (C 3.5.2 step 1).
const osRelease = "/etc/os-release"

var groupNameRE = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// rootFile reads a file root decides from (state.ReadOwnedFile or state.ReadOwnedTarget).
type rootFile func(path string, limit int64, uid uint32) ([]byte, bool, error)

// readConfigFile is path read by read, owned by owner and capped at maxHostFile, with every
// failure, a missing file included, the install's (78).
func readConfigFile(path string, owner uint32, read rootFile) ([]byte, error) {
	data, found, err := read(path, maxHostFile, owner)
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
		raw, err := readConfigFile(path, owner, state.ReadOwnedFile)
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
	raw, err := readConfigFile(path, owner, state.ReadOwnedFile)
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
	raw, err := readConfigFile(root+osRelease, owner, state.ReadOwnedTarget)
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
