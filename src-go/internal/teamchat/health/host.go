package health

import (
	"context"
	"errors"
	"fmt"
	"math"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// StatfsSpace is Space by statfs(2) of Path (/var/lib on the host): the blocks an unprivileged
// writer may still use, as Tuwunel's database and the backups are.
type StatfsSpace struct{ Path string }

// Space is Path's filesystem's free and total bytes.
func (s StatfsSpace) Space() (int64, int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(s.Path, &st); err != nil {
		return 0, 0, fmt.Errorf("statfs %s: %w", s.Path, err)
	}
	if st.Bsize <= 0 {
		return 0, 0, errors.New("statfs " + s.Path + ": no block size")
	}
	bsize := uint64(st.Bsize)
	free, freeOK := bytesOf(st.Bavail, bsize)
	total, totalOK := bytesOf(st.Blocks, bsize)
	if !freeOK || !totalOK {
		return 0, 0, errors.New("statfs " + s.Path + ": a size out of range")
	}
	return free, total, nil
}

// bytesOf is blocks of bsize bytes, false when that does not fit an int64.
func bytesOf(blocks, bsize uint64) (int64, bool) {
	if blocks > math.MaxInt64/bsize {
		return 0, false
	}
	n := blocks * bsize
	if n > math.MaxInt64 {
		return 0, false
	}
	return int64(n), true
}

// TeamLocks takes a team's lock in Dir (paths.AdminRunDir on the host) as the health tick.
type TeamLocks struct {
	Dir   string
	Clock Clock
}

// TryLock takes team's lock without waiting (C 3.4: the health timer never waits).
func (l TeamLocks) TryLock(ctx context.Context, team paths.TeamID) (Releaser, error) {
	return teamlock.Take(ctx, l.Dir, team, "health", 0, l.Clock)
}
