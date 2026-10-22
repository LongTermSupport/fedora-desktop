package restore

import (
	"context"
	"io"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// UnitState is a unit as systemd shows it (SD10): its LoadState, ActiveState, the
// InvocationID in hex, NRestarts and Result.
type UnitState struct {
	Load, Active, InvocationID string
	NRestarts                  int64
	Result                     string
}

// The states the engine judges.
const (
	loadNotFound   = "not-found"
	activeActive   = "active"
	activeInactive = "inactive"
	activeFailed   = "failed"
)

// NotLoaded is a unit systemd does not have loaded, so systemd-run may make it again.
func (s UnitState) NotLoaded() bool { return s.Load == loadNotFound }

// Stopped is RS18: inactive, failed or not loaded. systemd keeps a crash-looping unit, or one
// an AssertPathExists= refused, failed after a stop, so failed counts.
func (s UnitState) Stopped() bool {
	return s.NotLoaded() || s.Active == activeInactive || s.Active == activeFailed
}

// Units is the unit manager the engine uses (SM4): units.Manager's State and Stop.
type Units interface {
	State(ctx context.Context, unit string) (UnitState, error)
	Stop(ctx context.Context, unit string) error
}

// Readiness is hostnet's probe of a homeserver's server_version on the team's port.
type Readiness interface {
	ServerVersion(ctx context.Context, port int) error
}

// Status is host.Status's refresh with the events the restore adds (RS8 step 7).
type Status interface {
	Refresh(ctx context.Context, team paths.TeamID, first bool, events []string) error
}

// Clock is the time the engine's polls are measured by.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// Tar is backup N's tar as BK3 validated it: the manifest's time and server name, and its
// members.
type Tar struct {
	TimeMS     int64
	ServerName string
	TeamFile   []byte
	Config     []byte
	Roster     apply.RosterFile
	Rooms      proto.RoomIDs
	Token      string
}

// Deps is every effect of the restore (SM4). Root prefixes every host path ("" on a host);
// CtlUID owns ctl/<team>/, StateOwnerUID the state root and root's own files. Tar reads and
// validates root/<team>/backups/<n>.tar (backup.Decode); Backup is backup.Take under the held
// lock; Serve is host.Serve; Bootstrap is host.Deps.Bootstrap and RegKey the registration key
// a config holds. Sandbox is host.BaseSandbox for the team and Version the server version
// units.json names. Self is teamchat-admin's own copy, which the secure unit runs.
type Deps struct {
	Root                  string
	CtlUID, StateOwnerUID uint32
	Units                 Units
	Ready                 Readiness
	Run                   execx.Runner
	Child                 Child
	FS                    FS
	Tar                   func(team paths.TeamID, n int64) (Tar, error)
	Backup                func(ctx context.Context, held *teamlock.Lock, keep []int64) (int64, error)
	Serve                 func(ctx context.Context, team paths.TeamID) error
	Bootstrap             func(ctx context.Context, b host.Bootstrap) ([]string, error)
	RegKey                func(config []byte) (string, error)
	Approvals             declare.Approvals
	StatusRecord          func(team paths.TeamID) (*status.Record, error)
	Status                Status
	Rand                  io.Reader
	Clock                 Clock
	Sandbox               func(team paths.TeamID) []string
	Version               func(team paths.TeamID) (paths.ServerVersion, error)
	Self                  string
}

// Request is a fresh restore of backup Backup: offline (RO), or with the backup's roster as
// the base (RM11).
type Request struct {
	Team                paths.TeamID
	Backup              int64
	Offline, FromBackup bool
}
