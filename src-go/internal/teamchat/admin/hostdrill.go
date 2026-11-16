package admin

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore/drill"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// HostDrill is the Driller over the host: Sys's host steps, and Engine, the restore engine
// without the proof a restore needs (the drill's own restores and discards make the proof).
type HostDrill struct {
	Sys    *System
	Engine func(ctx context.Context) (Restorer, error)
}

// Run implements Driller. The rig the drill runs on (the scratch team's apply, its members and
// messages, and the Matrix probes of each check) is not built, so every drill is refused and
// restore stays refused with it.
func (h HostDrill) Run(context.Context, int) (drill.Outcome, error) {
	return drill.Outcome{}, fmt.Errorf("%w: the drill's machine rig is not built yet (issue #324), so no drill runs and restore stays refused", drill.ErrRefused)
}

// Clean implements Driller: drill.Clean over the host, a recorded restore discarded by the
// engine under the team lock (the engine reads the record by the trust rule), then the team removed with purge (host.Remove, its own lock).
func (h HostDrill) Clean(ctx context.Context) ([]paths.TeamID, error) {
	d := h.Sys.Host
	return drill.Clean(ctx, d.Root, d.StateOwnerUID, drill.Cleaner{
		Recorded: func(id paths.TeamID) (bool, error) {
			_, err := os.Lstat(d.Root + paths.RestoreRecord(id))
			if errors.Is(err, fs.ErrNotExist) {
				return false, nil
			}
			return err == nil, err
		},
		Discard: h.discard,
		Remove: func(ctx context.Context, id paths.TeamID) error {
			rd := d
			rd.LockWait = host.PlayWait
			_, err := host.Remove(ctx, rd, id, true)
			return err
		},
	})
}

func (h HostDrill) discard(ctx context.Context, id paths.TeamID) error {
	engine, err := h.Engine(ctx)
	if err != nil {
		return err
	}
	d := h.Sys.Host
	lock, err := teamlock.Take(ctx, d.LockDir, id, "restore-drill", host.PlayWait, d.Clock, restore.Recorded(d.Root))
	if err != nil {
		return err
	}
	_, err = engine.Discard(ctx, lock, id)
	return errors.Join(err, lock.Release())
}
