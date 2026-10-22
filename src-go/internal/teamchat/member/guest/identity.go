// Package guest is the guest operations an LXC member's user runs inside its container (Plan
// 00209 B 2.13, SPEC-n3 GU1-GU5): the host's `lxc-attach` runs `teamchat guest <op> --user U`
// as that user, and each op acts only on that user's own home.
//
// Every op starts from Establish (GU1, identity.go): umask 077 first, then the caller must be
// the user named, by uid and by passwd name, not root, with no group but the user's own, and
// the passwd home is opened as the homefs root everything after goes through. Every op writes
// through an Output (output.go): stdout carries only `CHANGED<TAB><what>` lines, each what from
// the op's fixed set, which the host parses (TB2.4); an op ends through Finish, so its exit is
// 0, 64, 75 (only an op that takes a seat lock) or 78, and its error object is the last line
// of stderr.
package guest

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Account is one passwd entry.
type Account struct {
	Name string
	UID  int
	GID  int
	Home string
}

// System is the process's ids and umask and the passwd database, a seam so a test need not run
// as another user. OS is the real one.
type System interface {
	Umask(mask int) int
	Getuid() int
	Geteuid() int
	Getgid() int
	Getegid() int
	Getgroups() ([]int, error)
	LookupName(name string) (Account, error)
	LookupID(uid int) (Account, error)
}

// Identity is the caller once Establish has checked it is the user named: its uid and gid, and
// its home held open as the root every file of the op is reached through. Close it when done.
type Identity struct {
	User paths.GuestUser
	UID  int
	GID  int
	Home *homefs.Root
}

// Close releases the home's fd.
func (id *Identity) Close() error { return id.Home.Close() }

const (
	// umask is set before anything else, so nothing an op makes is ever group or other
	// readable, whatever mode a later call forgets to name.
	umask = 0o077

	msgRoot      exits.Template = "teamchat guest refuses to run as root; the host runs it as guest user {}"
	msgSetID     exits.Template = "teamchat guest refuses a set-id caller; the host runs it as guest user {}"
	msgNotUser   exits.Template = "the caller is not guest user {} by uid and passwd name; the host runs it as that user"
	msgGroups    exits.Template = "the caller holds a group that is not guest user {}'s own; the host runs it with that group only"
	msgHome      exits.Template = "guest user {}'s passwd home is not a directory of its own that no one else can write"
	msgNoAccount exits.Template = "guest user {} has no passwd entry"
)

// Establish checks the caller is user and opens its home (GU1). Every refusal is 78
// `identity`, or 78 `home` for the home; none quotes a value read from passwd.
func Establish(sys System, u paths.GuestUser) (*Identity, error) {
	sys.Umask(umask)
	name := exits.Name(u.String())
	uid := sys.Getuid()
	if uid == 0 {
		return nil, identityError(msgRoot, name)
	}
	if sys.Geteuid() != uid || sys.Getegid() != sys.Getgid() {
		return nil, identityError(msgSetID, name)
	}
	acct, err := sys.LookupName(u.String())
	if err != nil {
		return nil, identityError(msgNoAccount, name)
	}
	byID, err := sys.LookupID(uid)
	if err != nil || acct.UID != uid || byID.Name != u.String() {
		return nil, identityError(msgNotUser, name)
	}
	if ownGroupOnly(sys, acct.GID) != nil {
		return nil, identityError(msgGroups, name)
	}
	root, err := homefs.Open(acct.Home, homefs.Owned)
	if err != nil {
		return nil, exits.New(exits.CodeHome, nil, msgHome, name)
	}
	return &Identity{User: u, UID: uid, GID: acct.GID, Home: root}, nil
}

func identityError(t exits.Template, name exits.Fill) error {
	return exits.New(exits.CodeIdentity, nil, t, name)
}

var errGroups = errors.New("a group that is not the user's own")

// ownGroupOnly is the gid the user's and every supplementary group that gid too (getgroups may
// list the primary group or not).
func ownGroupOnly(sys System, gid int) error {
	if sys.Getgid() != gid {
		return errGroups
	}
	groups, err := sys.Getgroups()
	if err != nil {
		return fmt.Errorf("getgroups: %w", err)
	}
	for _, g := range groups {
		if g != gid {
			return errGroups
		}
	}
	return nil
}

// OS is the real process and the system's passwd database.
type OS struct{}

// Umask sets the process's umask and is the old one.
func (OS) Umask(mask int) int { return unix.Umask(mask) }

// Getuid is the real uid.
func (OS) Getuid() int { return os.Getuid() }

// Geteuid is the effective uid.
func (OS) Geteuid() int { return os.Geteuid() }

// Getgid is the real gid.
func (OS) Getgid() int { return os.Getgid() }

// Getegid is the effective gid.
func (OS) Getegid() int { return os.Getegid() }

// Getgroups is the supplementary groups.
func (OS) Getgroups() ([]int, error) {
	groups, err := os.Getgroups()
	if err != nil {
		return nil, fmt.Errorf("getgroups: %w", err)
	}
	if groups == nil {
		groups = []int{}
	}
	return groups, nil
}

// LookupName is the passwd entry named name.
func (OS) LookupName(name string) (Account, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return Account{}, fmt.Errorf("passwd: %w", err)
	}
	return account(u)
}

// LookupID is the passwd entry of uid.
func (OS) LookupID(uid int) (Account, error) {
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return Account{}, fmt.Errorf("passwd: %w", err)
	}
	return account(u)
}

func account(u *user.User) (Account, error) {
	uid, uerr := strconv.Atoi(u.Uid)
	gid, gerr := strconv.Atoi(u.Gid)
	if err := errors.Join(uerr, gerr); err != nil {
		return Account{}, fmt.Errorf("passwd: a non-numeric id: %w", err)
	}
	return Account{Name: u.Username, UID: uid, GID: gid, Home: u.HomeDir}, nil
}
