package sessions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The LXC half of verify-restore (Plan 00266 C.3). The record of the containers running at the
// last shutdown, and the list of containers, are root's alone, so the set is asked of
// `teamchat-admin lxc check-running` through sudo -n, which the sudoers rule play-teamchat.yml
// installs (lxcSudoersRule) lets this user run with no password and no other argument. It is
// asked only where the play enabled the LXC boot unit, the one place an LXC set is restored.
const (
	lxcCheckVerb   = "lxc check-running"
	lxcSudoersRule = "/etc/sudoers.d/teamchat-lxc-check-running"
	lxcEnabled     = "enabled"
)

// lxcNotSetUp are the is-enabled words of a host with no LXC seat restore: the play leaves the
// boot unit disabled while no LXC member is declared, and a host without teamchat has none.
var lxcNotSetUp = []string{"disabled", "not-found"}

// lxcLine is one container line of check-running, as verify-restore prints it.
type lxcLine struct {
	kind, container, why string
}

// The kinds check-running prints (lxc.RunningKind), and the state verify-restore gives each.
var lxcStates = map[string]string{"UP": "RUNNING", "DOWN": "NOT-RUNNING", "UNRECORDED": "NOT-RECORDED", "NO-RECORD": "NO-RECORD"}

func (l lxcLine) String() string {
	if l.kind == "NO-RECORD" {
		return "lxc NO-RECORD"
	}
	s := "lxc:" + l.container + " " + lxcStates[l.kind]
	if l.why != "" {
		s += " " + l.why
	}
	return s
}

// bad is a line that does not pass: a recorded container down, or no record at all.
func (l lxcLine) bad() bool { return l.kind == "DOWN" || l.kind == "NO-RECORD" }

// verifyLXC prints the LXC set the boot unit was to bring back, one line a container on stdout:
// "lxc:<c> RUNNING" (recorded at the last shutdown and running), "lxc:<c> NOT-RUNNING <state>"
// (recorded, not running; ABSENT when the host has no such container), "lxc:<c> NOT-RECORDED"
// (running, not recorded: told, and it passes) and "lxc NO-RECORD" (the shutdown recorded
// nothing). With time left before deadline it looks again while a recorded container is not
// running. Exit 0 when nothing is down and a record exists, or no LXC restore is set up here.
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
	lines, err := a.settledLXC(ctx, deadline)
	if err != nil {
		return a.failed(err)
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.String() + "\n")
	}
	a.out(b.String())
	return a.judgeLXC(lines)
}

// settledLXC looks until no recorded container is down or the deadline has passed; with no time
// to wait it looks once.
func (a *App) settledLXC(ctx context.Context, deadline time.Time) ([]lxcLine, error) {
	for {
		lines, err := a.lxcSet(ctx)
		if err != nil {
			return nil, err
		}
		down := slices.ContainsFunc(lines, func(l lxcLine) bool { return l.kind == "DOWN" })
		if !down || !a.Now().Before(deadline) {
			return lines, nil
		}
		a.Pause(a.cfg.Poll, nil)
	}
}

// judgeLXC says on stderr what each line that is not plainly fine means, and is the verdict.
func (a *App) judgeLXC(lines []lxcLine) term.ExitCode {
	code := term.ExitOK
	for _, l := range lines {
		switch l.kind {
		case "UNRECORDED":
			a.sayf("%s runs but the last shutdown did not record it; something other than the boot unit started it (lxc.start.auto, or the host's own IaC).\n", "lxc:"+l.container)
		case "NO-RECORD":
			a.sayf("No record of the containers running at the last shutdown exists (%s), so the set this boot was to bring back is unknown; journalctl -u %s -b -1 --no-pager | cat says why it was not written.\n", l.why, paths.LXCRecordService)
		}
		if l.bad() {
			code = term.ExitFailure
		}
	}
	if slices.ContainsFunc(lines, func(l lxcLine) bool { return l.kind == "DOWN" }) {
		a.sayf("Not every LXC container running at the last shutdown runs again; journalctl -u %s -b --no-pager | cat says why.\n", strings.TrimSuffix(paths.LXCBootService, ".service"))
	}
	return code
}

// lxcSet asks check-running as root and reads its lines strictly.
func (a *App) lxcSet(ctx context.Context) ([]lxcLine, error) {
	res, err := (&sudo.Sudo{Runner: a.Runner}).Capture(ctx, paths.AdminBinaryPath, "lxc", "check-running")
	if errors.Is(err, sudo.ErrNeedsPassword) {
		a.fail(fmt.Sprintf("sudo wants a password for %s %s, so the LXC set cannot be read; play-teamchat.yml installs %s, which lets the teamchat owner group run it without one.", paths.AdminBinaryPath, lxcCheckVerb, lxcSudoersRule))
		return nil, errReported
	}
	if err != nil {
		detail := strings.TrimSpace(string(res.Stderr))
		if detail == "" {
			detail = err.Error()
		}
		a.fail(fmt.Sprintf("%s %s failed, so the LXC set cannot be read: %s", paths.AdminBinary, lxcCheckVerb, detail))
		return nil, errReported
	}
	return a.parseLXC(string(res.Stdout))
}

// parseLXC reads check-running's `<kind>\t<container>\t<why>` lines; anything else is refused,
// never guessed at.
func (a *App) parseLXC(out string) ([]lxcLine, error) {
	if out == "" {
		return nil, nil
	}
	var lines []lxcLine
	for text := range strings.SplitSeq(strings.TrimSuffix(out, "\n"), "\n") {
		fields := strings.Split(text, "\t")
		if len(fields) != 3 || !validLXC(fields) {
			a.fail(fmt.Sprintf("%s %s printed a line it never prints, so the LXC set is not judged: %q", paths.AdminBinary, lxcCheckVerb, text))
			return nil, errReported
		}
		lines = append(lines, lxcLine{kind: fields[0], container: fields[1], why: fields[2]})
	}
	return lines, nil
}

// validLXC is a line's fields in the form its kind takes: a container with no why, a down one
// with its state word, or no container and the record's path.
func validLXC(f []string) bool {
	kind, container, why := f[0], f[1], f[2]
	if _, known := lxcStates[kind]; !known || strings.ContainsAny(container+why, " ") {
		return false
	}
	switch kind {
	case "NO-RECORD":
		return container == "" && why != ""
	case "DOWN":
		return container != "" && why != ""
	}
	return container != "" && why == ""
}
