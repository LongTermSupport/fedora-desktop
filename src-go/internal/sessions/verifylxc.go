package sessions

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcset"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The LXC half of verify-restore (Plan 00266 C.3). The record of the containers running at the
// last shutdown, and the list of containers, are root's alone, so the boot unit's verb (`lxc
// boot`, or `lxc reconcile` where the host's own IaC starts the containers) writes what it found
// to paths.LXCSetStatusFile, 0640 to the teamchat owner group, and this reads that as the user,
// with no privilege. It is read only where the play enabled the LXC boot unit, the one place an
// LXC set is restored.
const (
	lxcEnabled = "enabled"
	// lxcHostRootEnv is a host path prefix the status is read under; unset is the host itself.
	lxcHostRootEnv = "CCY_TEAMCHAT_HOST_ROOT"
)

// lxcNotSetUp are the is-enabled words of a host with no LXC seat restore: the play leaves the
// boot unit disabled while no LXC member is declared, and a host without teamchat has none.
var lxcNotSetUp = []string{"disabled", "not-found"}

// The state verify-restore gives each kind of line.
var lxcStates = map[lxcset.Kind]string{lxcset.Up: "RUNNING", lxcset.Down: "NOT-RUNNING", lxcset.Unrecorded: "NOT-RECORDED", lxcset.NoRecord: "NO-RECORD"}

func lxcLineText(l lxcset.Line) string {
	if l.Kind == lxcset.NoRecord {
		return "lxc NO-RECORD"
	}
	s := "lxc:" + l.Container + " " + lxcStates[l.Kind]
	if l.Why != "" {
		s += " " + l.Why
	}
	return s
}

// lxcLook is one read of the status: none, one of another boot, or this boot's.
type lxcLook struct {
	status lxcset.Status
	found  bool
	stale  bool
}

// final is a status this boot's boot unit wrote; it is written once, so no wait changes it.
func (l lxcLook) final() bool { return l.found && !l.stale }

// verifyLXC prints the LXC set the boot unit was to bring back, one line a container on stdout:
// "lxc:<c> RUNNING" (recorded at the last shutdown and running), "lxc:<c> NOT-RUNNING <state>"
// (recorded, not running; ABSENT when the host has no such container), "lxc:<c> NOT-RECORDED"
// (running, not recorded: told, and it passes) and "lxc NO-RECORD" (the shutdown recorded
// nothing); or "lxc NO-STATUS" (the boot unit has written no status) or "lxc STALE" (its status
// is of another boot). With time left before deadline it looks again until a status of this boot
// is there. Exit 0 when nothing is down and a record exists, or no LXC restore is set up here.
func (a *App) verifyLXC(ctx context.Context, deadline time.Time) term.ExitCode {
	word, err := (&systemd.Systemd{Runner: a.Runner}).UnitEnabled(ctx, paths.LXCBootService)
	switch {
	case err != nil:
		a.fail("cannot tell whether the LXC containers are restored here: " + err.Error())
		return term.ExitFailure
	case slices.Contains(lxcNotSetUp, word):
		a.sayf("%s is %s, so there is no LXC set to verify.\n", paths.LXCBootService, word)
		return term.ExitOK
	case word != lxcEnabled:
		a.fail(fmt.Sprintf("%s is %s, neither enabled nor disabled, so whether the LXC containers are restored here is unknown; re-run play-teamchat.yml.", paths.LXCBootService, word))
		return term.ExitFailure
	}
	look, err := a.settledLXC(deadline)
	if err != nil {
		return a.failed(err)
	}
	return a.judgeLXC(look)
}

// settledLXC looks until a status of this boot is there or the deadline has passed; with no
// time to wait it looks once.
func (a *App) settledLXC(deadline time.Time) (lxcLook, error) {
	for {
		look, err := a.lxcStatus()
		if err != nil {
			return lxcLook{}, err
		}
		if look.final() || !a.Now().Before(deadline) {
			return look, nil
		}
		a.Pause(a.cfg.Poll, nil)
	}
}

// lxcStatus reads the status and compares its boot with this one. Could not read is a failure,
// said, never a finding.
func (a *App) lxcStatus() (lxcLook, error) {
	boot, err := (&restore.Store{BootIDFile: a.cfg.BootIDFile}).BootID()
	if err != nil {
		a.fail(err.Error() + ", so the LXC set's status cannot be dated.")
		return lxcLook{}, errReported
	}
	s, found, err := lxcset.Read(a.Getenv(lxcHostRootEnv))
	if err != nil {
		a.fail(fmt.Sprintf("the LXC set cannot be read: %v; verify-restore reads it as a member of the teamchat owner group (teamchat_owner_group in play-teamchat.yml).", err))
		return lxcLook{}, errReported
	}
	return lxcLook{status: s, found: found, stale: found && s.BootID != boot}, nil
}

// judgeLXC prints the report and says on stderr what each line that is not plainly fine means;
// it is the verdict.
func (a *App) judgeLXC(look lxcLook) term.ExitCode {
	unit := strings.TrimSuffix(paths.LXCBootService, ".service")
	switch {
	case !look.found:
		a.out("lxc NO-STATUS\n")
		a.sayf("%s is enabled but has written no status of the LXC set (%s); journalctl -u %s -b --no-pager | cat says whether it ran and why it wrote none.\n", paths.LXCBootService, paths.LXCSetStatusFile, unit)
		return term.ExitFailure
	case look.stale:
		a.out("lxc STALE\n")
		a.sayf("The status of the LXC set (%s) was written in another boot, at %s, so it says nothing of this one; %s has not finished this boot, or failed before it wrote: journalctl -u %s -b --no-pager | cat.\n", paths.LXCSetStatusFile, lxcWritten(look.status), paths.LXCBootService, unit)
		return term.ExitFailure
	}
	var b strings.Builder
	for _, l := range look.status.Lines {
		b.WriteString(lxcLineText(l) + "\n")
	}
	a.out(b.String())
	a.sayf("The LXC set as %s found it at %s.\n", paths.LXCBootService, lxcWritten(look.status))
	code := term.ExitOK
	for _, l := range look.status.Lines {
		switch l.Kind {
		case lxcset.Unrecorded:
			a.sayf("lxc:%s runs but the last shutdown did not record it; something other than the boot unit started it (lxc.start.auto, or the host's own IaC).\n", l.Container)
		case lxcset.NoRecord:
			a.sayf("No record of the containers running at the last shutdown exists (%s), so the set this boot was to bring back is unknown; journalctl -u %s -b -1 --no-pager | cat says why it was not written.\n", l.Why, paths.LXCRecordService)
			code = term.ExitFailure
		case lxcset.Down:
			code = term.ExitFailure
		case lxcset.Up:
		}
	}
	if slices.ContainsFunc(look.status.Lines, func(l lxcset.Line) bool { return l.Kind == lxcset.Down }) {
		a.sayf("Not every LXC container running at the last shutdown ran again when the boot unit looked; journalctl -u %s -b --no-pager | cat says why.\n", unit)
	}
	return code
}

// lxcWritten is when the status was written, in local time.
func lxcWritten(s lxcset.Status) string {
	return time.UnixMilli(s.WrittenMS).Format(time.RFC3339)
}
