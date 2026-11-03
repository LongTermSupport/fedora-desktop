package restore

import (
	"context"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// Child is the teamchat-ctl child's restore ops (RC1-RC8), which admin.RestoreChild
// implements. teamFile is the applied team.json; a failure the child answered is an
// *admin.ChildError, whose class decides the exit code.
type Child interface {
	// Secure runs restore.secure through the secure unit's argv unit (RU2), as ExecChild
	// judges a child (RC1).
	Secure(ctx context.Context, team paths.TeamID, unit []string, a SecureArgs) ([]string, error)
	// Check runs restore.check: through unit against the staged server, or with a nil unit
	// and Live args against the live one (RC8).
	Check(ctx context.Context, team paths.TeamID, unit []string, a CheckArgs) (Epoch, error)
	// Swap runs restore.swap (RC2).
	Swap(ctx context.Context, team paths.TeamID, a SwapArgs) ([]string, error)
	// Export runs restore.export (RC3).
	Export(ctx context.Context, team paths.TeamID, a ExportArgs) (Export, error)
	// Finish runs restore.finish (RC4).
	Finish(ctx context.Context, team paths.TeamID, teamFile []byte) ([]string, error)
	// Revert runs restore.revert (RC5).
	Revert(ctx context.Context, team paths.TeamID, teamFile []byte) ([]string, error)
}

// SecureArgs are restore.secure's: the staged server's port and the tar's token, its rooms,
// the merged roster, and the handles and humans to retire, revoke and lock (RC1).
type SecureArgs struct {
	TeamFile                           []byte
	Port                               int
	Token                              string
	Rooms                              proto.RoomIDs
	Roster                             apply.RosterFile
	Retire, Revoke, RevokeHumans, Lock []string
}

// CheckArgs are restore.check's (RC8); Port 0 with Token "" is the live server.
type CheckArgs struct {
	TeamFile []byte
	Port     int
	Token    string
}

// Live says the check reads the live server with the current token.
func (a CheckArgs) Live() bool { return a.Port == 0 && a.Token == "" }

// SwapArgs are restore.swap's: the tar's token and rooms and the merged roster (RC2).
type SwapArgs struct {
	TeamFile []byte
	Token    string
	Rooms    proto.RoomIDs
	Roster   apply.RosterFile
}

// ExportArgs are restore.export's: the backup restored and the time it was made (RC3).
type ExportArgs struct {
	TeamFile []byte
	Backup   int64
	SinceMS  int64
}

// Export is restore.export's answer: the events written, the oldest kept, the export's end
// time, and whether the cap cut it (RC3, n2g-D15).
type Export struct {
	Count, FirstMS, EndMS int64
	Truncated             bool
}

// Epoch is restore.check's answer: the served roster event's restore epoch and rev (RC8).
type Epoch struct {
	RestoreEpoch, Rev int64
}
