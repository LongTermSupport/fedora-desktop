// Package wire is `teamchat-admin`'s production System (Plan 00209 SPEC-n2f.md WR2): every
// seam the verbs reach, built from the adapters with the host's paths. Building it reads only
// the running binary (CS2); every other host read (the bus, the teamchat-ctl account, the owner
// group, the pin) happens when a verb calls the seam that needs it, so a part that cannot be
// built fails only the verbs that need it (n2f-W1).
package wire

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/admin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/ctl"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/hostnet"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/units"
)

// Bridge is the LXC bridge the members' allow rule is rendered from (NW4).
const Bridge = "lxcbr0"

// ErrBackupNotBuilt is step 6's backup until C 3.12 is built (n2f-D6).
var ErrBackupNotBuilt = errors.New("backups are not built yet (C 3.12)")

// New is the production wiring for a build of digest: Env.Wire of `teamchat-admin`'s main.
func New(digest string) func() (*admin.System, error) {
	return func() (*admin.System, error) { return production().system(digest) }
}

// machine is what the wiring reads of the host, a seam each so the tests run on a scratch root.
type machine struct {
	root, proc string
	self       func() (string, error)
	lookupUser func(name string) (admin.User, bool, error)
	ownerGroup func() (int, error)
	readPin    func() (host.Pin, error)
	runner     execx.Runner
	dial       func() (units.Conn, error)
}

func production() machine {
	return machine{
		proc: "/proc", self: admin.SelfPath, lookupUser: admin.OSProcess().LookupUser,
		ownerGroup: admin.OwnerGroupFile("", admin.LookupGroup),
		readPin:    func() (host.Pin, error) { return admin.ReadPin("") },
		runner:     execx.OS{}, dial: units.SystemBus,
	}
}

func (m machine) system(digest string) (*admin.System, error) {
	d, err := paths.ParseDigest(digest)
	if err != nil {
		return nil, fmt.Errorf("this build carries no valid source digest: %w", err)
	}
	self, err := m.self()
	if err != nil {
		return nil, fmt.Errorf("the running binary: %w", err)
	}
	clock := systemClock{}
	root, run := m.root, m.root+paths.AdminRunDir
	ownerGroup := sync.OnceValues(m.ownerGroup)
	systemd := units.NewManager(m.dial, m.runner)
	child := admin.ExecChild{Runner: m.runner, Self: self, Lookup: m.lookupUser}
	tick := sync.OnceValues(func() (*health.Tick, error) {
		gid, err := ownerGroup()
		if err != nil {
			return nil, fmt.Errorf("the owner group: %w", err)
		}
		return health.New(health.Config{
			Clock: clock, Teams: health.Stamps{Root: root}, Units: systemd.Health(),
			Prober: hostnet.Prober{Port: hostnet.TeamPort(root)}, Viewer: admin.ChildViewer{Child: child, Root: root},
			Backups: health.NoBackups{}, Space: health.StatfsSpace{Path: root + "/var/lib"}, Guests: health.GuestsNotBuilt{},
			Announcer: admin.ChildAnnouncer{Child: child, Root: root}, Locks: health.TeamLocks{Dir: run, Clock: clock},
			StatusPath:    func(t paths.TeamID) string { return root + paths.StatusFile(t) },
			LXCOnlinePath: func(t paths.TeamID) string { return root + paths.LXCOnlineFile(t) },
			GID:           gid,
		})
	})
	return &admin.System{
		Host: host.Deps{
			Root: root, LockDir: run, Clock: clock, Rand: rand.Reader, Software: digest,
			// Load only: team approve writes approved.json with the owner group's gid.
			Approvals: declare.FileApprovals{Path: func(t paths.TeamID) string { return root + paths.ApprovedFile(t) }, GID: -1},
			Systemd:   systemd.Host(), Firewall: hostnet.Firewall{Runner: m.runner, Proc: m.proc},
			Network: hostnet.Network{Proc: m.proc, BridgeName: Bridge}, Ready: hostnet.Readiness{},
			Status: statusFiles{root: root, tick: tick},
			Backup: func(context.Context, *teamlock.Lock) error { return ErrBackupNotBuilt },
		},
		Child:      child,
		Bundles:    admin.SeatBundles{Clock: clock.Now, Digest: d},
		OwnerGroup: ownerGroup,
		Open:       ctl.Config{Root: root}.Opener(),
		Pin:        m.readPin,
		CtlIDs:     m.ctlIDs,
		Health:     func() (admin.HealthTick, error) { return tick() },
		Home:       admin.LookupHome,
	}, nil
}

func (m machine) ctlIDs() (admin.User, error) {
	u, found, err := m.lookupUser(paths.CtlUser)
	if err != nil {
		return admin.User{}, fmt.Errorf("cannot look up the %s account: %w", paths.CtlUser, err)
	}
	if !found {
		return admin.User{}, fmt.Errorf("no %s account: the install makes it", paths.CtlUser)
	}
	return u, nil
}

// statusFiles is host.Status as the health tick's status files, the tick made at the first call.
type statusFiles struct {
	root string
	tick func() (*health.Tick, error)
}

func (s statusFiles) Refresh(ctx context.Context, team paths.TeamID, first bool) error {
	f, err := s.files()
	if err != nil {
		return err
	}
	return f.Refresh(ctx, team, first)
}

func (s statusFiles) Remove(ctx context.Context, team paths.TeamID) (string, error) {
	f, err := s.files()
	if err != nil {
		return "", err
	}
	return f.Remove(ctx, team)
}

func (s statusFiles) files() (health.StatusFiles, error) {
	t, err := s.tick()
	if err != nil {
		return health.StatusFiles{}, fmt.Errorf("the status record: %w", err)
	}
	return health.StatusFiles{Root: s.root, Tick: t}, nil
}

// systemClock is the wall clock: host.Clock and health.Clock.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
