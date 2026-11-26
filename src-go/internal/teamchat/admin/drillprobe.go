package admin

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore/drill"
)

// DrillRig is the whole rig: the machine's steps and every probe.
var _ drill.Rig = (*DrillRig)(nil)

// Owners implements drill.Probe: the owner of each entry of the team's private state directory.
func (r *DrillRig) Owners(_ context.Context, team paths.TeamID) (map[string]uint32, error) {
	return r.OwnersOf(team)
}

// JoinedEpoch implements drill.Probe: the staged copy brought up, and restore.check through the
// unit joined to its namespace (H-C15.reach).
func (r *DrillRig) JoinedEpoch(ctx context.Context, team paths.TeamID) (string, error) {
	var e restore.Epoch
	err := r.StagedUp(ctx, r.Engine, team, func(ctx context.Context, check func(context.Context) (restore.Epoch, error)) error {
		var err error
		e, err = check(ctx)
		return err
	})
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(e.RestoreEpoch, 10), nil
}

// MemberReach implements drill.Probe: with the staged copy up, whether anything answers on the
// team's port at any address of the host a member could reach, every address but loopback. A
// host with loopback only is refused: probing no address would pass by default.
func (r *DrillRig) MemberReach(ctx context.Context, team paths.TeamID) (bool, error) {
	addrs, err := r.memberAddrs()
	if err != nil {
		return false, err
	}
	reached := false
	err = r.StagedUp(ctx, r.Engine, team, func(ctx context.Context, _ func(context.Context) (restore.Epoch, error)) error {
		for _, a := range addrs {
			ok, derr := r.Dial(ctx, a)
			if derr != nil {
				return derr
			}
			reached = reached || ok
		}
		return nil
	})
	return reached, err
}

// memberAddrs is the team's port on each address of the host but loopback.
func (r *DrillRig) memberAddrs() ([]netip.AddrPort, error) {
	ifaces, err := r.Sys.Host.Network.Interfaces()
	if err != nil {
		return nil, err
	}
	port, err := netip.ParseAddrPort("0.0.0.0:" + strconv.Itoa(r.port))
	if err != nil {
		return nil, err
	}
	var addrs []netip.AddrPort
	for _, i := range ifaces {
		for _, a := range i.Addrs {
			if !a.IsLoopback() {
				addrs = append(addrs, netip.AddrPortFrom(a, port.Port()))
			}
		}
	}
	if len(addrs) == 0 {
		return nil, errors.New("this host has no address but loopback, so a member's reach cannot be probed")
	}
	return addrs, nil
}

// Staged implements drill.Probe: the record at staged, and backup N's staged copy kept as a
// directory.
func (r *DrillRig) Staged(ctx context.Context, team paths.TeamID) (bool, error) {
	st, err := restore.Records{Root: r.Engine.Root, OwnerUID: r.Engine.StateOwnerUID}.State(ctx, team)
	if err != nil || st.Restore == nil || st.Restore.Step != string(restore.StepStaged) {
		return false, err
	}
	k, err := r.Engine.FS.Kind(team, filepath.Base(paths.HomeserverPrivateRestoreDatabase(team, st.Restore.Backup)))
	return err == nil && k == restore.KindDirectory, err
}
