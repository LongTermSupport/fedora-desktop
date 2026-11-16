package admin

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore/drill"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// HostDrill is the Driller over the host: Sys's host steps, and the restore engine without the
// proof a restore needs (the drill's own restores and discards make the proof). Engine is that
// engine as the clean-up's discard runs it, Deps its Deps with the hooks unset, which the drill
// sets on each of its own restores. Key is what the proof is evidence about, read as sys.Restore
// reads it; TakeBackup is backup.Take under the held lock, answering the backup's number; Dial is
// whether a TCP connect is answered; Drill is drill.Run.
type HostDrill struct {
	Sys        *System
	Engine     func(ctx context.Context) (Restorer, error)
	Deps       func(ctx context.Context) (restore.Deps, error)
	Key        func(ctx context.Context) (restore.Key, error)
	TakeBackup func(ctx context.Context, held *teamlock.Lock) (int64, error)
	Dial       func(ctx context.Context, addr netip.AddrPort) (bool, error)
	Drill      func(ctx context.Context, cfg drill.Config, rig drill.Rig) (drill.Outcome, error)
}

// Run implements Driller: refused while anything listens on the drill port, then the drill on a
// fresh rig, recording where it ran.
func (h HostDrill) Run(ctx context.Context, port int, where restore.Where) (drill.Outcome, error) {
	if err := h.portFree(port); err != nil {
		return drill.Outcome{}, err
	}
	key, err := h.Key(ctx)
	if err != nil {
		return drill.Outcome{}, fmt.Errorf("what the proof is keyed to: %w", err)
	}
	deps, err := h.Deps(ctx)
	if err != nil {
		return drill.Outcome{}, err
	}
	d := h.Sys.Host
	cfg := drill.Config{Root: d.Root, UID: d.StateOwnerUID, Rand: d.Rand, Clock: d.Clock, Key: key, Where: where, Port: port}
	return h.Drill(ctx, cfg, h.rig(deps))
}

// Clean implements Driller: drill.Clean over the host.
func (h HostDrill) Clean(ctx context.Context) ([]paths.TeamID, error) {
	d := h.Sys.Host
	return drill.Clean(ctx, d.Root, d.StateOwnerUID, h.cleaner())
}

// portFree refuses a drill port a socket of this host listens on, at any address: the scratch
// team's server could not bind it, or a probe would reach another server.
func (h HostDrill) portFree(port int) error {
	listeners, err := h.Sys.Host.Network.Listeners()
	if err != nil {
		return fmt.Errorf("the host's listening sockets: %w", err)
	}
	for _, l := range listeners {
		if int(l.Addr.Port()) != port {
			continue
		}
		by := ""
		if l.Unit != "" {
			by = " by " + l.Unit
		}
		return fmt.Errorf("%w: the drill port %d is in use (%s%s)", drill.ErrRefused, port, l.Addr, by)
	}
	return nil
}

// rig is one drill's rig over deps, the engine's hooks unset.
func (h HostDrill) rig(deps restore.Deps) *DrillRig {
	return &DrillRig{
		Sys: h.Sys, Engine: deps,
		RunRestore: restore.Run, RunDiscard: restore.Discard, StagedUp: restore.Staged,
		TakeBackup: h.TakeBackup,
		OwnersOf:   restore.OS{Root: deps.Root, StateOwnerUID: deps.StateOwnerUID}.Owners,
		Base:       loopbackBase, Dial: h.Dial, Clock: h.Sys.Host.Clock,
		Clean: h.cleaner(),
	}
}

// loopbackBase is the scratch team's server as the host reaches it.
func loopbackBase(port int) string { return "http://127.0.0.1:" + strconv.Itoa(port) }

// cleaner removes a marked scratch team: a recorded restore discarded by the engine under the
// team lock (the engine reads the record by the trust rule), then the team removed with purge
// (host.Remove, its own lock).
func (h HostDrill) cleaner() drill.Cleaner {
	d := h.Sys.Host
	return drill.Cleaner{
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
	}
}

func (h HostDrill) discard(ctx context.Context, id paths.TeamID) error {
	engine, err := h.Engine(ctx)
	if err != nil {
		return err
	}
	d := h.Sys.Host
	lock, err := teamlock.Take(ctx, d.LockDir, id, drillVerb, host.PlayWait, d.Clock, restore.Recorded(d.Root))
	if err != nil {
		return err
	}
	_, err = engine.Discard(ctx, lock, id)
	return errors.Join(err, lock.Release())
}
