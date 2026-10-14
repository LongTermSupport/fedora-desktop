// Package fsx holds the file primitives the installer writes as root with: every write
// goes through an os.Root (so a symlink swapped into a directory another user can write
// cannot redirect it), is atomic (temp file and rename), refuses to follow a symlink at
// the destination, and reports each change as a CHANGED line.
package fsx

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
)

// Owner is a numeric uid and gid.
type Owner struct {
	UID, GID int
}

// LookupOwner resolves a user name and a group name to ids. Names only: a numeric string
// that is not also a name is refused.
func LookupOwner(userName, groupName string) (Owner, error) {
	u, err := user.Lookup(userName)
	if err != nil {
		return Owner{}, fmt.Errorf("user %q: %w", userName, err)
	}
	g, err := user.LookupGroup(groupName)
	if err != nil {
		return Owner{}, fmt.Errorf("group %q: %w", groupName, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return Owner{}, fmt.Errorf("user %q has uid %q: %w", userName, u.Uid, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return Owner{}, fmt.Errorf("group %q has gid %q: %w", groupName, g.Gid, err)
	}
	return Owner{UID: uid, GID: gid}, nil
}

// CurrentOwner is the running user and its primary group: every owner in test mode.
func CurrentOwner() (Owner, error) {
	u, err := user.Current()
	if err != nil {
		return Owner{}, fmt.Errorf("current user: %w", err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return Owner{}, fmt.Errorf("current user has gid %q: %w", u.Gid, err)
	}
	return Owner{UID: os.Getuid(), GID: gid}, nil
}
