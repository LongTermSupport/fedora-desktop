package admin

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const verbRotateCA = "team rotate-ca"

// rotateHelp is `team rotate-ca --help`, its payload.
const rotateHelp = `usage: ` + paths.AdminBinary + ` team rotate-ca --team <team>

Makes a new CA for the team and a new leaf signed by it, for a CA key that is lost or exposed,
and restarts the TLS terminator with the new leaf. A member trusts only the CA its bundle
carries, so the cut is hard: every seat is cut off until its bundle carries the new CA.

stdout is the handles of the seats this host does not place itself (placed by the owner's
` + "`seat place-issued`" + ` pipe, here or over ssh), one per line: place each again with
  sudo ` + paths.AdminBinary + ` member reissue --team <team> --handle <handle> --credential password|token | [ssh <alias>] teamchat seat place-issued --checkout <dir>
The LXC seats are reissued with the new CA by the next lxc apply (` + paths.PlayFile + `).
Run the Element play (` + paths.ElementPlayFile + `) again afterwards: the CA it placed is the old one.

The team's backups hold its CA. A backup taken before a rotation holds the old CA and leaf, and
restoring it undoes the rotation: run team rotate-ca again after restoring one.
`

// rotation is what the verb did: the fingerprints, and the seats placed again by others.
type rotation struct {
	ca         host.RotatedCA
	byOwner    []string
	lxcReissue []string
}

// teamRotateCA is Plan 00246 SPEC-cross-network X33: under the team lock (refused while a
// restore is recorded), the CA and leaf replaced by host.CARotation together with the LXC
// record's mark for a reissue, then stdout the handles the owner places again and stderr what
// to do next, the old and the new CA's fingerprints first.
func teamRotateCA(ctx context.Context, e Env, args []string) term.ExitCode {
	flags, err := parseFlags(args, []string{flagTeam}, "help")
	if err != nil {
		return failWith(e, err)
	}
	if flags["help"] == flagTrue {
		if len(flags) > 1 {
			return fail(e.Stderr, term.ExitUsage, "--help takes no other flag")
		}
		return answer(e, rotateHelp)
	}
	sys, code := e.system()
	if code != term.ExitOK {
		return code
	}
	team, err := teamFlag(flags)
	if err != nil {
		return failWith(e, err)
	}
	lock, err := teamlock.Take(ctx, sys.Host.LockDir, team, "team.rotate-ca", host.PlayWait, sys.Host.Clock, sys.Host.Guard)
	if err != nil {
		return failWith(e, err)
	}
	r, err := rotateCA(ctx, sys, team)
	err = errors.Join(err, lock.Release())
	if r.ca.New != "" {
		if code = answer(e, lines(r.byOwner)); code != term.ExitOK {
			return code
		}
		if _, werr := io.WriteString(e.Stderr, rotatedText(team, r)); werr != nil {
			err = errors.Join(err, werr)
		}
	}
	if err != nil {
		return failWith(e, err)
	}
	return term.ExitOK
}

// rotateCA is the verb's work under the lock. A zero ca.New is a rotation that replaced nothing.
func rotateCA(ctx context.Context, sys *System, team paths.TeamID) (rotation, error) {
	if _, _, err := readTeamFile(sys.Host.Root, team); err != nil {
		return rotation{}, err
	}
	names, err := host.AppliedLeafNames(sys.Host.Root)(team)
	if err != nil {
		return rotation{}, err
	}
	view, err := ChildViewer{Child: sys.Child, Root: sys.Host.Root}.View(ctx, team)
	if err != nil {
		return rotation{}, err
	}
	var r rotation
	for _, m := range view.Members {
		if m.State == apply.MemberEnrolled && m.Kind != status.KindLXC {
			r.byOwner = append(r.byOwner, m.Handle)
		}
	}
	slices.Sort(r.byOwner)
	var also []host.Replacement
	if also, r.lxcReissue, err = lxcMarks(sys, team); err != nil {
		return rotation{}, err
	}
	rot := host.CARotation{Root: sys.Host.Root, Clock: sys.Host.Clock, Rand: sys.Host.Rand, Units: sys.Host.Systemd, Write: sys.RotateWrite, StateOwnerUID: sys.Host.StateOwnerUID}
	r.ca, err = rot.Rotate(ctx, team, names, also)
	if r.ca.New == "" {
		return rotation{}, err
	}
	if serr := sys.Host.Status.Refresh(ctx, team, false, nil); serr != nil {
		err = errors.Join(err, serr)
	}
	return r, err
}

// lxcMarks is the LXC record with each seat holding a handle marked for a reissue, as the
// replacement the rotation makes with its own files, and those handles; none when no seat is.
func lxcMarks(sys *System, team paths.TeamID) ([]host.Replacement, []string, error) {
	rec, found, err := lxcstate.ReadRecord(sys.Host.Root, team)
	if err != nil || !found {
		return nil, nil, err
	}
	marked, handles := lxc.CARotated(rec, sys.Host.Clock.Now().UnixMilli())
	if len(handles) == 0 {
		return nil, nil, nil
	}
	path := sys.Host.Root + paths.LXCRecord(team)
	old, _, err := state.ReadFile(path, lxcstate.MaxRecord)
	if err != nil {
		return nil, nil, err
	}
	data, err := marked.Encode()
	if err != nil {
		return nil, nil, err
	}
	gid, err := sys.OwnerGroup()
	if err != nil {
		return nil, nil, err
	}
	return []host.Replacement{{Path: path, Old: old, New: data, Mode: lxcstate.RecordMode, GID: gid}}, handles, nil
}

func lines(ss []string) string {
	var b strings.Builder
	for _, s := range ss {
		b.WriteString(s + "\n")
	}
	return b.String()
}

// rotatedText is the verb's stderr after a rotation: the fingerprints, then each next step.
func rotatedText(team paths.TeamID, r rotation) string {
	var b strings.Builder
	say := func(s string) { b.WriteString(paths.AdminBinary + ": " + s + "\n") }
	say("team " + team.String() + "'s CA is rotated: old CA sha256:" + r.ca.Old + ", new CA sha256:" + r.ca.New)
	if len(r.byOwner) > 0 {
		say("each handle on stdout holds a bundle with the old CA: place it again with the pipe it was placed by: " +
			"sudo " + paths.AdminBinary + " member reissue --team " + team.String() + " --handle <handle> --credential password|token" +
			" | [ssh <alias>] teamchat seat place-issued --checkout <dir>")
	}
	if len(r.lxcReissue) > 0 {
		say("the LXC seats " + strings.Join(r.lxcReissue, ", ") + " are reissued with the new CA by the next lxc apply (" + paths.PlayFile + ")")
	}
	say("run the Element play again (" + paths.ElementPlayFile + "): the CA it placed for this team is the old one")
	say("a backup taken before this rotation holds the old CA: after restoring one, run " + verbRotateCA + " again")
	return b.String()
}
