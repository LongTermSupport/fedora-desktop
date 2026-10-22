package admin

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"
)

// Process is what `teamchat-admin` reads of its own process: the uids the caller class is
// decided from, and what the child checks of how it was started.
type Process interface {
	Getuid() int
	Geteuid() int
	Getgroups() ([]int, error)
	Getwd() (string, error)
	Environ() []string
	// LookupUser is a local account's uid and gid; found is false when there is none.
	LookupUser(name string) (User, bool, error)
}

// User is a local account's ids.
type User struct {
	UID, GID int
}

type osProcess struct{}

// OSProcess is this process.
func OSProcess() Process { return osProcess{} }

func (osProcess) Getuid() int { return os.Getuid() }

func (osProcess) Geteuid() int { return os.Geteuid() }

func (osProcess) Getgroups() ([]int, error) { return os.Getgroups() }

func (osProcess) Getwd() (string, error) { return os.Getwd() }

func (osProcess) Environ() []string { return os.Environ() }

func (osProcess) LookupUser(name string) (User, bool, error) {
	u, err := user.Lookup(name)
	if errors.As(err, new(user.UnknownUserError)) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return User{}, false, fmt.Errorf("account %s has uid %q: %w", name, u.Uid, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return User{}, false, fmt.Errorf("account %s has gid %q: %w", name, u.Gid, err)
	}
	return User{UID: uid, GID: gid}, true, nil
}
