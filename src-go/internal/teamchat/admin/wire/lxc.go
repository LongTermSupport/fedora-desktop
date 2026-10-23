package wire

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestfacts"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// guestSystemdSocket is the guest systemd's private socket, which root dials (SU2, n3-D24).
const guestSystemdSocket = "/run/systemd/private"

// hostPasswd is the host's account file, every uid of which a guest member may not have (n3-D29).
const hostPasswd = "/etc/passwd"

// lxcDeps is System.LXC: the lxc programs as root, the guest pin, the guest's systemd, and the
// host's facts, read when an lxc verb first needs them. The verbs add the Roster and Refresh.
func (m machine) lxcDeps(clock systemClock, ownerGroup func() (int, error)) (*lxc.Deps, error) {
	gid, err := ownerGroup()
	if err != nil {
		return nil, fmt.Errorf("the owner group: %w", err)
	}
	uids, err := m.hostUIDs()
	if err != nil {
		return nil, err
	}
	kit, err := m.currentKit()
	if err != nil {
		return nil, err
	}
	root := m.root
	return &lxc.Deps{
		Root: root, LockDir: root + paths.AdminRunDir, Clock: clock, LXC: m.containers(), Pin: m.pin(), Bus: guestSystemd,
		Approvals: declare.FileApprovals{Path: func(t paths.TeamID) string { return root + paths.ApprovedFile(t) }, GID: -1},
		Origin:    health.Stamps{Root: root}.Origin, V1: v1NotBuilt{},
		Kit: kit, HostKit: root + paths.HostKitDir(kit).String(), HostUIDs: uids, OwnerGID: gid, Bridge: Bridge,
	}, nil
}

// containers is the lxc programs as root. The desktop account's uid is one of HostUIDs, which
// the apply refuses for a guest member before any attach (n3-D29); 0 leaves lxccmd's own uid
// check to refuse root.
func (m machine) containers() lxccmd.LXC { return lxccmd.LXC{Runner: m.runner} }

// pin is a container's guest root, pinned through its init pid (LX3).
func (m machine) pin() func(ctx context.Context, container string) (lxc.Root, error) {
	boxes, pinner := m.containers(), guestroot.Pinner{Proc: m.proc, Kernel: m.kernel}
	return func(ctx context.Context, container string) (lxc.Root, error) {
		r, err := pinner.Pin(ctx, func(ctx context.Context) (int, error) { return boxes.InitPID(ctx, container) })
		if err != nil {
			return nil, err
		}
		return lxc.Pinned(r), nil
	}
}

// guests is the health tick's L3 probe (LP1) over the same seams as the apply's.
func (m machine) guests() lxc.Guests {
	return lxc.Guests{Root: m.root, LXC: m.containers(), Pin: m.pin(), Clock: clock.System()}
}

// guestSystemd is the guest's systemd on its private socket, dialled through the root (TB1).
func guestSystemd(ctx context.Context, r lxc.Root) (guestbus.Systemd, error) {
	c, err := r.DialSocket(ctx, guestSystemdSocket)
	if err != nil {
		return nil, err
	}
	conn, err := guestbus.Dial(ctx, c)
	if err != nil {
		return nil, err
	}
	return guestbus.New(conn), nil
}

// hostUIDs is every uid of the host's passwd, read by the trust rule.
func (m machine) hostUIDs() (map[int]bool, error) {
	file := m.root + hostPasswd
	raw, found, err := state.ReadFile(file, guestfacts.MaxPasswd)
	if err != nil {
		return nil, fmt.Errorf("the host's %s: %w", hostPasswd, err)
	}
	if !found {
		return nil, fmt.Errorf("the host has no %s", hostPasswd)
	}
	uids, err := guestfacts.HostUIDs(raw)
	if err != nil {
		return nil, fmt.Errorf("the host's %s: %w", hostPasswd, err)
	}
	return uids, nil
}

// currentKit is the digest the host kit's current link names (n3-D26): the kit root a directory
// of kitOwner's that neither its group nor others may write, the link kitOwner's, read by
// readlinkat beneath it, and its target a digest.
func (m machine) currentKit() (paths.Digest, error) {
	dir := m.root + paths.HostKitRoot
	fd, err := unix.Open(dir, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return paths.Digest{}, fmt.Errorf("the host kit %s: %w", dir, err)
	}
	digest, err := m.linkedKit(fd, dir)
	if cerr := unix.Close(fd); cerr != nil {
		err = errors.Join(err, fmt.Errorf("the host kit %s: %w", dir, cerr))
	}
	return digest, err
}

func (m machine) linkedKit(fd int, dir string) (paths.Digest, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return paths.Digest{}, fmt.Errorf("the host kit %s: %w", dir, err)
	}
	if int64(st.Uid) != int64(m.kitOwner) || st.Mode&0o022 != 0 {
		return paths.Digest{}, fmt.Errorf("the host kit %s is owned by uid %d with mode %o: it must be uid %d's, unwritable by group or others", dir, st.Uid, st.Mode&0o7777, m.kitOwner)
	}
	link := dir + "/current"
	if err := unix.Fstatat(fd, "current", &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return paths.Digest{}, fmt.Errorf("the host kit's %s: %w", link, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFLNK {
		return paths.Digest{}, fmt.Errorf("the host kit's %s is not a link", link)
	}
	if int64(st.Uid) != int64(m.kitOwner) {
		return paths.Digest{}, fmt.Errorf("the host kit's %s is owned by uid %d, not %d", link, st.Uid, m.kitOwner)
	}
	buf := make([]byte, 256)
	n, err := unix.Readlinkat(fd, "current", buf)
	if err != nil {
		return paths.Digest{}, fmt.Errorf("the host kit's %s: %w", link, err)
	}
	d, err := paths.ParseDigest(string(buf[:n]))
	if err != nil {
		return paths.Digest{}, fmt.Errorf("the host kit's %s does not name a digest: %w", link, err)
	}
	return d, nil
}

// v1NotBuilt is the v1 holdback until N6 builds it: asked only for a migration-origin team,
// it fails, so no migrated member is joined before its v1 session is known to be gone.
type v1NotBuilt struct{}

func (v1NotBuilt) Holds(context.Context, lxc.Root, paths.GuestUser, paths.TeamID) (bool, error) {
	return false, errors.New("the v1 holdback of a migration-origin team is not built yet (N6)")
}
