package paths

import (
	"fmt"
	"strconv"
)

// FloorAccountData is the room account data type of a seat's resume marker (B 2.3.2).
const FloorAccountData = MatrixPrefix + "floor"

// storeAsidePrefix starts the name a set-aside store is renamed to, followed by the time in
// milliseconds (B 2.3.2, SPEC-n3 SA1).
const storeAsidePrefix = storeFile + ".aside-"

const storeFile = "store.json"

const passwordFile = "password"

// StoreAside is the name the seat's store is set aside as at ms, a positive time.
func (s Seat) StoreAside(ms int64) string {
	if ms <= 0 {
		panic("paths: a store set aside at a time that is not positive")
	}
	return s.Dir() + "/" + storeAsidePrefix + strconv.FormatInt(ms, 10)
}

// Password is where a password credential would sit in a member's seat directory, beside
// seat.json, as it sits in a pool seat. No member file is ever written there: a password
// bundle in a member's seat directory is refused (SPEC-n3 MS3).
func (s Seat) Password() string { return s.Dir() + "/" + passwordFile }

// InboxSocket is Claude Code's per-session inbox socket for the session process pid, under the
// runtime directory it was started with: `<runtime>/cc-socks/<pid>.sock` (B 2.4.4, 2.7.1).
func InboxSocket(runtimeDir string, pid int) (string, error) {
	if err := checkBase("runtime", runtimeDir); err != nil {
		return "", err
	}
	if pid <= 0 {
		return "", fmt.Errorf("%w pid: must be positive", ErrInvalid)
	}
	return runtimeDir + "/cc-socks/" + strconv.Itoa(pid) + ".sock", nil
}

// GuestUnitFile is the guest's session unit file, which the host writes (n3-D3, C 3.9.4).
func GuestUnitFile(u GuestUser) string { return systemdDir + "/" + SessionUnit(u) }

// GuestUnitWantsLink is the unit's link in multi-user.target.wants, so it starts at the next
// boot (n3-D3, DC13 (b)).
func GuestUnitWantsLink(u GuestUser) string {
	return systemdDir + "/multi-user.target.wants/" + SessionUnit(u)
}

// GuestRuntimeParents are the two run roots a guest user's sessions are under: its login
// runtime directory's and the session unit's RuntimeDirectory= (n3-D8, C 3.9.2 step 4).
func GuestRuntimeParents(u GuestUser, uid int) ([]string, error) {
	unit := GuestRuntimeDir(u) + "/" + Product + "/run"
	if uid <= 0 {
		return nil, fmt.Errorf("%w uid: must be positive", ErrInvalid)
	}
	return []string{"/run/user/" + strconv.Itoa(uid) + "/" + Product + "/run", unit}, nil
}
