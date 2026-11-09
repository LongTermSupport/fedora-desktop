package containerimages

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Range is a span of host ids, from Start, Count of them.
type Range struct {
	Start int64 `json:"start"`
	Count int64 `json:"count"`
}

// Has is whether id is in the range.
func (r Range) Has(id int64) bool { return id >= r.Start && id < r.Start+r.Count }

func (r Range) String() string {
	return strconv.FormatInt(r.Start, 10) + "-" + strconv.FormatInt(r.Start+r.Count-1, 10)
}

func (r Range) overlaps(o Range) bool { return r.Start < o.Start+o.Count && o.Start < r.Start+r.Count }

// Owners is who may own a file of the rootless store: root, the user, and the user's current
// subordinate ranges (O1).
type Owners struct {
	UID    int64 `json:"uid"`
	GID    int64 `json:"gid"`
	SubUID Range `json:"subuid"`
	SubGID Range `json:"subgid"`
}

// foreignUID is O1: a uid the rootless user's namespace does not map.
func (o Owners) foreignUID(uid int64) bool { return uid != 0 && uid != o.UID && !o.SubUID.Has(uid) }

// shift is O10's move of one id: the same offset in cur as it had in old.
func shift(id int64, old, cur Range) (int64, bool) {
	if !old.Has(id) || id-old.Start >= cur.Count {
		return 0, false
	}
	return cur.Start + id - old.Start, true
}

var errUnshiftable = errors.New("owned outside both the old and the current range, or past the current range's size")

// move is the owner a path of uid and gid gets (O10): the uid shifted, the gid shifted when it
// is the old range's and kept otherwise.
func (o Owners) move(uid, gid int64, old Range) (int64, int64, error) {
	newUID, ok := shift(uid, old, o.SubUID)
	if !ok {
		return 0, 0, fmt.Errorf("uid %d: %w", uid, errUnshiftable)
	}
	if !old.Has(gid) {
		return newUID, gid, nil
	}
	newGID, ok := shift(gid, old, o.SubGID)
	if !ok {
		return 0, 0, fmt.Errorf("gid %d: %w", gid, errUnshiftable)
	}
	return newUID, newGID, nil
}

// checkOld is O11: an old range that holds root, the user, or any id the current ranges map
// would turn a mapped file into another.
func (o Owners) checkOld(old Range) error {
	switch {
	case old.Count <= 0 || old.Start <= 0:
		return fmt.Errorf("the old range %d+%d is empty or holds root", old.Start, old.Count)
	case old.Has(o.UID) || old.Has(o.GID):
		return fmt.Errorf("the old range %s holds the user's own uid or gid", old)
	case old.overlaps(o.SubUID):
		return fmt.Errorf("the old range %s overlaps the current subuid range %s", old, o.SubUID)
	case old.overlaps(o.SubGID):
		return fmt.Errorf("the old range %s overlaps the current subgid range %s", old, o.SubGID)
	}
	return nil
}

// parseSubIDs is the one range /etc/subuid or /etc/subgid gives the user, by login or uid.
func parseSubIDs(text, login string, id int64) (Range, error) {
	var found []Range
	for line := range strings.Lines(text) {
		f := strings.Split(strings.TrimSpace(line), ":")
		if len(f) != 3 || f[0] != login && f[0] != strconv.FormatInt(id, 10) {
			continue
		}
		start, err1 := strconv.ParseInt(f[1], 10, 64)
		count, err2 := strconv.ParseInt(f[2], 10, 64)
		if err1 != nil || err2 != nil || count <= 0 {
			return Range{}, fmt.Errorf("cannot read the range line %q", strings.TrimSpace(line))
		}
		found = append(found, Range{Start: start, Count: count})
	}
	if len(found) != 1 {
		return Range{}, fmt.Errorf("%d ranges for %s, not one, so which ids rootless podman maps is unknown", len(found), login)
	}
	return found[0], nil
}
