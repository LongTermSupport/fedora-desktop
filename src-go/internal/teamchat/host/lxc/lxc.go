// Package lxc is `lxc apply` and `lxc restart` run from the host, and the L3 probe the health
// tick asks (Plan 00209 SPEC-n3 section 3, LX1-LX9, SU1-SU3, LP1; DESIGN-v2-C-host.md 3.9). It
// composes the guest root (guestroot, TB1), the lxc programs (lxccmd, TB3), the guest's
// systemd (guestbus, SU2), the unit text (unit), the guest's facts (guestfacts) and the host's
// two LXC records (lxcstate); the child's member work reaches it through Roster.
//
// An offline member (n3b-D1) never fails, refuses or waits for anything: a container that is
// stopped, frozen or missing, one that stops during the phase, or a session unit that is down
// is shown offline and the phase goes on for the rest. Only an operation that names that one
// member (`--member`, `lxc restart`) reports it, as an *OfflineError. A running member whose
// facts are wrong fails that member alone; the rest of the team still applies.
package lxc

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/guest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// Phase is which half of the play's LXC work a run is (C 3.9.2).
type Phase string

// The two phases of a run (C 3.9.2).
const (
	PhaseAbsent  Phase = "absent"
	PhasePresent Phase = "present"
)

// Target is one seat `--member C/K` names: its container and its seat key (n3b-D2).
type Target struct {
	Container paths.ContainerName
	Key       paths.SeatKey
}

// Request is one `lxc apply`: the phase, the --only-teams scope (nil: every team), the
// --member scope (nil: every entry), and the declaration read from stdin (n3-D21).
type Request struct {
	Phase     Phase
	OnlyTeams []paths.TeamID
	Members   []Target
	Decl      declare.Declaration
}

// LineKind is an output line's first field (C 3.9.3).
type LineKind string

// The kinds of output line (C 3.9.3).
const (
	Changed  LineKind = "CHANGED"
	Skipped  LineKind = "SKIPPED"
	Deferred LineKind = "DEFERRED"
)

// Line is one output line, one per seat, every field the host's own (TB2.4).
type Line struct {
	Kind      LineKind
	Team      paths.TeamID
	User      paths.GuestUser
	Container paths.ContainerName
	Checkout  string
	Seat      int
	Key       paths.SeatKey
	Why       string
}

// String is `<kind>\t<team>\t<user>@<container>/<repo>-<seat>-<key>\t<why>` (C 3.9.3).
func (l Line) String() string {
	member := l.User.String() + "@" + l.Container.String() + "/" + paths.LXCRepoLabel(l.Checkout) + "-" +
		strconv.Itoa(l.Seat) + "-" + l.Key.String()
	return string(l.Kind) + "\t" + l.Team.String() + "\t" + member + "\t" + l.Why
}

// Report is a phase's lines and its summary, printed `LXC-SUMMARY\t<Summary>`.
type Report struct {
	Lines   []Line
	Summary string
}

func report(lines []Line) Report {
	n := map[LineKind]int{}
	for _, l := range lines {
		n[l.Kind]++
	}
	return Report{Lines: lines, Summary: fmt.Sprintf("%d changed, %d skipped, %d deferred", n[Changed], n[Skipped], n[Deferred])}
}

// Roster is the child's member work for one team, called while Apply holds that team's lock;
// admin's adapter implements it (gap 20).
type Roster interface {
	// Members is the team's roster states by handle (the child's team.view).
	Members(ctx context.Context, team paths.TeamID) (map[string]string, error)
	// Join is member add|return|reissue with --credential token: the answer's handle and the
	// seat.Issued bundle (JSON) for place-bundle. The child's error class retired (SPEC-n2d
	// MO4) is an error that errors.Is apply.ErrRetired.
	Join(ctx context.Context, team paths.TeamID, j Join) (Joined, error)
	Park(ctx context.Context, team paths.TeamID, handle string) error
	// Reach is guest check's stdin for the team: {"v":1,"team","base_url","plain_http_hosts"}.
	Reach(ctx context.Context, team paths.TeamID) ([]byte, error)
}

// Join is one join's arguments.
type Join struct {
	Op        string // apply.OpMemberAdd, OpMemberReturn or OpMemberReissue
	Prefix    proto.Prefix
	Seat      int // the entry's own seat, add only
	HumanText bool
	Handle    string // return and reissue
}

// Joined is a join's answer.
type Joined struct {
	Handle string
	Bundle []byte // never logged, never in an error
}

// Containers is lxccmd.LXC as Apply uses it.
type Containers interface {
	Containers(ctx context.Context) ([]string, error)
	State(ctx context.Context, c string) (lxccmd.State, error)
	InitPID(ctx context.Context, c string) (int, error)
	Addresses(ctx context.Context, c string) ([]netip.Addr, error)
	Config(ctx context.Context, c string, keys ...string) (map[string]string, error)
	Guest(ctx context.Context, a lxccmd.Attach, op guest.Op, args []string, stdin []byte) (lxccmd.GuestResult, error)
}

// Root is a pinned guest root as the phases use it: *guestroot.Root in production.
type Root interface {
	Alive() error
	Close() error
	Stat(p string) (guestroot.Info, error)
	ReadFile(p string, limit int64) ([]byte, guestroot.Info, bool, error)
	ReadDir(p string, valid func(string) bool, limit int) ([]string, bool, error)
	ReadProcStat(pid int) ([]byte, bool, error)
	ProbeLock(p string) (bool, []byte, error)
	DialSocket(ctx context.Context, p string) (*net.UnixConn, error)
	InitExe() (guestroot.Info, error)
	Writer() *guestroot.Writer
}

// V1Holdback is D's adapter (a seam until N6): whether the guest still runs this member on v1.
// It is handed the guest root, never a bare descriptor.
type V1Holdback interface {
	Holds(ctx context.Context, r Root, u paths.GuestUser, team paths.TeamID) (bool, error)
}

// Clock is the time the lock waits and the absent phase's L3 wait are measured by.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// Deps is what a phase reaches.
type Deps struct {
	Root      string // host path prefix ("" on a host)
	LockDir   string // paths.AdminRunDir
	Clock     Clock
	LXC       Containers
	Pin       func(ctx context.Context, container string) (Root, error)
	Bus       func(ctx context.Context, r Root) (guestbus.Systemd, error)
	Roster    Roster
	Approvals declare.Approvals
	Origin    func(ctx context.Context, team paths.TeamID) (string, error) // the stamp's origin
	V1        V1Holdback
	Kit       paths.Digest                                       // the host kit's digest (n3-D26)
	HostKit   string                                             // the host kit directory, walked by the host's trust reads
	HostUIDs  map[int]bool                                       // every uid of the host's passwd (n3-D29)
	OwnerGID  int                                                // the LXC record's group
	Bridge    string                                             // wire.Bridge
	Refresh   func(ctx context.Context, team paths.TeamID) error // the status record, after a change
}

// The absent phase's L3 wait (C 3.9.2 step 4).
const (
	SeatFreeWait = 60 * time.Second
	SeatFreeGap  = time.Second
)

// verb is the team lock's holder and every error's verb.
const verb = "lxc apply"

// lockVerb is the team lock's holder word for the phases.
const lockVerb = "lxc-apply"

// OfflineError is an explicitly targeted member whose container is offline (exit 69).
type OfflineError struct {
	Container paths.ContainerName
	Why       string
}

func (e *OfflineError) Error() string {
	c := e.Container.String()
	return fmt.Sprintf("lxc: container %s is offline (%s); start it with lxc-start -n %s", c, e.Why, c)
}
