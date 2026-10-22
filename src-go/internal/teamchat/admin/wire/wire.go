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
	"math"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/admin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/backup"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/ctl"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/hostnet"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/units"
)

// Bridge is the LXC bridge the members' allow rule is rendered from (NW4).
const Bridge = "lxcbr0"

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
	dial       func(context.Context) (units.Conn, error)
	// restoreProven is set only once host checks H-C5 and H-C15 have passed (DC8, n2g-D8); a
	// test sets it to run the engine over these adapters. Until then System.Restore refuses.
	restoreProven bool
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
	backups := func() (backup.Deps, error) {
		uid, err := m.ctlUID()
		return backup.Deps{Root: root, Units: backupUnits{systemd}, Ready: hostnet.Readiness{}, Clock: clock, CtlUID: uid, StateOwnerUID: stateOwnerUID}, err
	}
	tick := sync.OnceValues(func() (*health.Tick, error) {
		gid, err := ownerGroup()
		if err != nil {
			return nil, fmt.Errorf("the owner group: %w", err)
		}
		return health.New(health.Config{
			Clock: clock, Teams: health.Stamps{Root: root}, Units: systemd.Health(),
			Prober: hostnet.Prober{Port: hostnet.TeamPort(root)}, Viewer: admin.ChildViewer{Child: child, Root: root},
			// HB1 reads only root's tars and records and the homeserver tree, never ctl/<team>/.
			Backups: backup.States(backup.Deps{Root: root, StateOwnerUID: stateOwnerUID}), Restores: restore.Records{Root: root}, Space: health.StatfsSpace{Path: root + "/var/lib"}, Guests: health.GuestsNotBuilt{},
			Announcer: admin.ChildAnnouncer{Child: child, Root: root}, Locks: health.TeamLocks{Dir: run, Clock: clock},
			StatusPath:    func(t paths.TeamID) string { return root + paths.StatusFile(t) },
			LXCOnlinePath: func(t paths.TeamID) string { return root + paths.LXCOnlineFile(t) },
			GID:           gid,
		})
	})
	sys := &admin.System{
		Host: host.Deps{
			Root: root, LockDir: run, Clock: clock, Rand: rand.Reader, Software: digest,
			// Load only: team approve writes approved.json with the owner group's gid.
			Approvals: declare.FileApprovals{Path: func(t paths.TeamID) string { return root + paths.ApprovedFile(t) }, GID: -1},
			Systemd:   systemd.Host(), Firewall: hostnet.Firewall{Runner: m.runner, Proc: m.proc},
			Network: hostnet.Network{Proc: m.proc, BridgeName: Bridge}, Ready: hostnet.Readiness{},
			Status: statusFiles{root: root, tick: tick},
			Guard:  restore.Guard(root),
			Backup: func(ctx context.Context, held *teamlock.Lock) error {
				d, err := backups()
				if err == nil {
					_, err = backup.Take(ctx, d, held, nil)
				}
				return err
			},
		},
		Child:      child,
		Bundles:    admin.SeatBundles{Clock: clock.Now, Digest: d},
		OwnerGroup: ownerGroup,
		Open:       ctl.Config{Root: root}.Opener(),
		Pin:        m.readPin,
		CtlIDs:     m.ctlIDs,
		Health:     func() (admin.HealthTick, error) { return tick() },
		Home:       admin.LookupHome,
		Backup:     backups,
	}
	sys.Restore = func() (admin.Restorer, error) {
		if !m.restoreProven {
			return nil, restore.ErrUnproven
		}
		return m.engine(sys, self, systemd, backups)
	}
	return sys, nil
}

// stateOwnerUID owns the state root and root's own files on a host.
const stateOwnerUID = 0

// engine is the restore engine over the host's adapters (RW1, SM4), run by the verb under the
// team lock it holds.
func (m machine) engine(sys *admin.System, self string, systemd *units.Manager, backups func() (backup.Deps, error)) (admin.Restorer, error) {
	b, err := backups()
	if err != nil {
		return nil, err
	}
	root := m.root
	return admin.Engine{Deps: restore.Deps{
		Root: root, CtlUID: b.CtlUID, StateOwnerUID: stateOwnerUID,
		Units: restoreUnits{systemd}, Ready: b.Ready, Run: m.runner,
		Child: admin.RestoreChild{Live: sys.Child, Unit: func(argv []string) admin.Child { return admin.UnitChild{Runner: m.runner, Argv: argv} }},
		FS:    restore.OS{Root: root, StateOwnerUID: stateOwnerUID},
		Tar: func(team paths.TeamID, n int64) (restore.Tar, error) {
			c, err := backup.ReadTar(b, team, n)
			return restore.Tar{
				TimeMS: c.Manifest.TimeMS, ServerName: c.Manifest.ServerName, TeamFile: c.TeamJSON, Config: c.Config,
				Roster: c.Roster, Rooms: c.Rooms, Token: c.Token,
			}, err
		},
		Backup: func(ctx context.Context, held *teamlock.Lock, keep []int64) (int64, error) {
			res, err := backup.Take(ctx, b, held, keep)
			return res.Backup, err
		},
		Serve:     func(ctx context.Context, team paths.TeamID) error { return host.Serve(ctx, sys.Host, team) },
		Bootstrap: admin.Bootstrapper(sys),
		RegKey:    regKey,
		Approvals: sys.Host.Approvals,
		StatusRecord: func(team paths.TeamID) (*status.Record, error) {
			rec, found, err := status.Read(root+paths.StatusFile(team), team)
			if err != nil || !found {
				return nil, err
			}
			return &rec, nil
		},
		Status: sys.Host.Status, Rand: rand.Reader, Clock: b.Clock,
		Sandbox: host.TeamSandbox,
		Version: func(team paths.TeamID) (paths.ServerVersion, error) { return host.ReadUnitsVersion(root, team) },
		Self:    self,
	}}, nil
}

// regKey is the registration key an applied config holds; a config without one was not
// rendered by the apply.
func regKey(config []byte) (string, error) {
	if key := host.RegKeyOf(config); key != "" {
		return key, nil
	}
	return "", errors.New("config.toml holds no registration key")
}

// ctlUID is the teamchat-ctl account's uid, which owns ctl/<team>/ (the trust rule).
func (m machine) ctlUID() (uint32, error) {
	u, err := m.ctlIDs()
	if err != nil {
		return 0, err
	}
	return ctlUID(u)
}

// ctlUID refuses a teamchat-ctl uid that is root's, which would make ctl/<team>/ root's own, or
// that is no uid.
func ctlUID(u admin.User) (uint32, error) {
	if u.UID <= 0 || u.UID > math.MaxUint32 {
		return 0, fmt.Errorf("the %s account has uid %d, not an unprivileged uid", paths.CtlUser, u.UID)
	}
	return uint32(u.UID), nil
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

func (s statusFiles) Refresh(ctx context.Context, team paths.TeamID, first bool, events []string) error {
	f, err := s.files()
	if err != nil {
		return err
	}
	return f.Refresh(ctx, team, first, events)
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

// backupUnits is the units Manager as backup.Units (SD10).
type backupUnits struct{ m *units.Manager }

func (u backupUnits) State(ctx context.Context, unit string) (backup.UnitState, error) {
	s, err := u.m.State(ctx, unit)
	return backup.UnitState{ActiveState: s.Active, InvocationID: s.InvocationID, NRestarts: s.NRestarts, Result: s.Result}, err
}

func (u backupUnits) Kill(ctx context.Context, unit, whom string, signal int32) error {
	return u.m.Kill(ctx, unit, whom, signal)
}

// restoreUnits is the units Manager as restore.Units (SD10).
type restoreUnits struct{ m *units.Manager }

func (u restoreUnits) State(ctx context.Context, unit string) (restore.UnitState, error) {
	s, err := u.m.State(ctx, unit)
	return restore.UnitState{Load: s.Load, Active: s.Active, InvocationID: s.InvocationID, NRestarts: s.NRestarts, Result: s.Result}, err
}

func (u restoreUnits) Stop(ctx context.Context, unit string) error { return u.m.Stop(ctx, unit) }

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
