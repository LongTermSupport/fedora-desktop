package lxc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestfacts"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/guest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// restartVerb and startVerb are every `lxc restart` and `lxc start` error's verb.
const (
	restartVerb = "lxc restart"
	startVerb   = "lxc start"
)

// The offline causes only `lxc restart` and `lxc start` meet.
const (
	whyStoppedRestart = "offline: container stopped during the restart"
	whyStoppedStart   = "offline: container stopped during the start"
	whyBusDown        = "offline: the guest's systemd is unreachable"
)

// seatJob is what `lxc restart` or `lxc start` does to the seat's unit over the guest's systemd,
// with the words its errors use.
// A job that ends a session asks nothing of claude (noLookup).
type seatJob struct {
	verb, step, whyStopped string
	noLookup               bool
	run                    func(ctx context.Context, s guestbus.Systemd, unit string) error
}

var (
	restartJob = seatJob{verb: restartVerb, step: "restart", whyStopped: whyStoppedRestart, run: restartUnit}
	startJob   = seatJob{verb: startVerb, step: "start", whyStopped: whyStoppedStart, run: startUnit}
	stopJob    = seatJob{verb: stopVerb, step: "stop", whyStopped: whyStoppedStop, noLookup: true, run: stopUnit}
)

// Restart is `lxc restart --member C/K` (SU3): the seat's session unit reset, then restarted,
// over the guest's systemd (SU2); no other seat's unit is touched. The seat is the record entry
// not left whose key in container is key, in any team's record; none is refused by name (70).
// It is an explicit target, so a container that is missing, not running, or that stops on the
// way, and a systemd that cannot be dialled, is an *OfflineError (69). A seat with no unit
// file, or one TB1 refuses, is refused by name (70). A restart leaves the record of started seats
// as it is: only Start adds a seat, Stop takes one out, and RecordStarted adds the live ones.
func Restart(ctx context.Context, d Deps, container paths.ContainerName, key paths.SeatKey) error {
	return runSeatJob(ctx, d, container, key, restartJob)
}

// Start is `lxc start --member C/K` (SU4): the seat's session unit reset, then started with
// systemd's StartUnit, which leaves a unit that already runs as it is, so a running session is
// never ended. The seat, the refusals and the offline causes are Restart's, in the start's words.
// A seat started is recorded as started (Plan 00266 C.2), for `lxc reconcile`.
func Start(ctx context.Context, d Deps, container paths.ContainerName, key paths.SeatKey) error {
	if err := runSeatJob(ctx, d, container, key, startJob); err != nil {
		return err
	}
	return recordStarted(d, startVerb, lxcstate.StartedSeat{Container: container, Key: key}, true)
}

// runSeatJob finds the seat, pins its container and runs job on its unit alone.
func runSeatJob(ctx context.Context, d Deps, container paths.ContainerName, key paths.SeatKey, job seatJob) (err error) {
	id, user, err := restartSeat(d, job.verb, container, key)
	if err != nil {
		return err
	}
	root, err := restartRoot(ctx, d, job, container)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	seat := container.String() + "/" + key.String()
	file := paths.GuestUnitFile(id.Team, id.Checkout, id.Seat)
	_, _, found, err := root.ReadFile(file, maxUnit)
	if err == nil && !found {
		err = fmt.Errorf("%w: %s has no session unit %s: the seat has service: false, or no apply has placed its unit yet",
			guestfacts.ErrRefused, seat, file)
	}
	if err != nil {
		return &host.Error{Verb: job.verb, Step: "unit", Drift: refused(err), Err: err}
	}
	if !job.noLookup {
		if err := sessionPath(ctx, d, root, job, container, key, user); err != nil {
			return err
		}
	}
	s, err := d.Bus(ctx, root)
	if err != nil && !refused(err) {
		return &OfflineError{Container: container, Why: whyBusDown}
	}
	if err == nil {
		err = errors.Join(job.run(ctx, s, paths.SessionUnit(id.Team, id.Checkout, id.Seat)), s.Close())
	}
	if errors.Is(err, guestbus.ErrGone) {
		return &OfflineError{Container: container, Why: job.whyStopped}
	}
	if err != nil {
		return &host.Error{Verb: job.verb, Step: job.step, Drift: refused(err), Err: fmt.Errorf("%s: %w", seat, err)}
	}
	return nil
}

// restartSeat is the seat of key in container from the teams' records under the host root, and
// its user: the one entry not left whose seat has that key.
func restartSeat(d Deps, verb string, container paths.ContainerName, key paths.SeatKey) (lxcstate.SeatID, paths.GuestUser, error) {
	teams, err := recordTeams(d.Root)
	if err != nil {
		return lxcstate.SeatID{}, paths.GuestUser{}, &host.Error{Verb: verb, Step: stepRecord, Err: err}
	}
	var seats []lxcstate.SeatID
	var users []paths.GuestUser
	for _, team := range teams {
		rec, _, err := lxcstate.ReadRecord(d.Root, team)
		if err != nil {
			return lxcstate.SeatID{}, paths.GuestUser{}, &host.Error{Verb: verb, Step: stepRecord, Err: err}
		}
		for _, e := range rec.Members {
			if e.Container == container && e.Status != lxcstate.StatusLeft && rec.SeatOf(e).Key() == key {
				seats, users = append(seats, rec.SeatOf(e)), append(users, e.User)
			}
		}
	}
	seat := container.String() + "/" + key.String()
	switch len(seats) {
	case 1:
		return seats[0], users[0], nil
	case 0:
		return lxcstate.SeatID{}, paths.GuestUser{}, &host.Error{
			Verb: verb, Step: stepRecord, Drift: true,
			Err: fmt.Errorf("--member %s names no seat of any team's LXC record; lxc apply places it", seat),
		}
	}
	return lxcstate.SeatID{}, paths.GuestUser{}, &host.Error{
		Verb: verb, Step: stepRecord, Drift: true,
		Err: fmt.Errorf("--member %s names a seat key two records hold; lxc apply refuses it", seat),
	}
}

// recordTeams is every team with an LXC record under the host root: each name of the record
// directory that is a team's record name. Another name is not a record.
func recordTeams(root string) ([]paths.TeamID, error) {
	entries, err := os.ReadDir(root + paths.LXCRecordDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []paths.TeamID
	for _, e := range entries {
		stem, ok := strings.CutSuffix(e.Name(), ".json")
		team, teamErr := paths.ParseTeamID(stem)
		if ok && teamErr == nil && paths.LXCRecord(team) == paths.LXCRecordDir+"/"+e.Name() {
			out = append(out, team)
		}
	}
	return out, nil
}

// restartRoot is the target container's pinned root, or why it is offline.
func restartRoot(ctx context.Context, d Deps, job seatJob, container paths.ContainerName) (Root, error) {
	c := container.String()
	names, err := d.LXC.Containers(ctx)
	if err != nil {
		return nil, &host.Error{Verb: job.verb, Step: stepState, Err: err}
	}
	why, err := boxState(ctx, d.LXC, names, container)
	if err != nil {
		return nil, &host.Error{Verb: job.verb, Step: stepState, Err: err}
	}
	if why != "" {
		return nil, &OfflineError{Container: container, Why: why}
	}
	root, err := d.Pin(ctx, c)
	if errors.Is(err, guestroot.ErrMoved) || errors.Is(err, guestroot.ErrGone) {
		return nil, &OfflineError{Container: container, Why: job.whyStopped}
	}
	if err != nil {
		return nil, &host.Error{Verb: job.verb, Step: "pin", Err: err}
	}
	return root, nil
}

func restartUnit(ctx context.Context, s guestbus.Systemd, name string) error {
	if err := s.ResetFailed(ctx, name); err != nil {
		return err
	}
	return s.Restart(ctx, name)
}

func startUnit(ctx context.Context, s guestbus.Systemd, name string) error {
	if err := s.ResetFailed(ctx, name); err != nil {
		return err
	}
	return s.Start(ctx, name)
}

// stepClaude is the step of a start whose lookup of claude was refused (R77).
const stepClaude = "claude"

// sessionPath is R77's lookup for one start: the seat user's login shell asked, through the
// host kit's own binary (TB3), where claude and node are; their directories go first in the PATH
// the start writes to the seat's environment file, which the unit names. Nothing found is kept
// anywhere else, so a node bump is found at the next start.
func sessionPath(ctx context.Context, d Deps, root Root, job seatJob, container paths.ContainerName, key paths.SeatKey, user paths.GuestUser) error {
	seat := container.String() + "/" + key.String()
	passwd, err := readGuest(root, guestPasswd)
	var acct guestfacts.Account
	if err == nil {
		acct, err = guestfacts.Passwd(passwd, user, d.HostUIDs)
	}
	if err != nil {
		return &host.Error{Verb: job.verb, Step: stepReads, Drift: refused(err), Err: fmt.Errorf("%s: %w", seat, err)}
	}
	kit := paths.HostKitDir(d.Kit)
	info, err := root.Stat(kit.String())
	if err == nil && (!info.Mode.IsDir() || info.UID != 0 || info.Mode.Perm()&0o022 != 0) || errors.Is(err, fs.ErrNotExist) {
		err = fmt.Errorf("%w: %s has no kit %s only root writes; lxc apply places it", guestfacts.ErrRefused, seat, kit)
	}
	if err != nil {
		return &host.Error{Verb: job.verb, Step: "kit", Drift: refused(err), Err: err}
	}
	res, err := d.LXC.Guest(ctx, lxccmd.Attach{
		Container: container.String(), User: user, Key: key, UID: acct.UID, GID: acct.GID, Home: acct.Home, Kit: kit,
	}, guest.OpFind, nil, nil)
	if ge, isGuest := errors.AsType[*lxccmd.GuestError](err); isGuest {
		return &host.Error{Verb: job.verb, Step: stepClaude, Drift: true, Err: fmt.Errorf(
			"%s: %w; the interactive login shell of %s must find claude (command -v claude): run teamchat guest find-claude --user %s in the container as %s to see why",
			seat, ge, user, user, user)}
	}
	if err != nil {
		return &host.Error{Verb: job.verb, Step: stepClaude, Err: fmt.Errorf("%s: %w", seat, err)}
	}
	text := []byte("PATH=" + startPath(res.Found, acct.Home) + "\n")
	file := paths.GuestSessionEnvFile(key)
	w := root.Writer()
	if _, err = w.MkdirAll(path.Dir(file)); err == nil {
		_, err = w.WriteFile(path.Dir(file), path.Base(file), text, 0o644)
	}
	if err != nil {
		return &host.Error{Verb: job.verb, Step: "path", Drift: refused(err), Err: fmt.Errorf("%s: %w", seat, err)}
	}
	return nil
}

// startPath is claude's directory, node's when it is another, then the unit's own PATH.
func startPath(found map[string]string, home string) string {
	var dirs []string
	for _, program := range []string{guest.ProgramClaude, guest.ProgramNode} {
		if p := found[program]; p != "" && !slices.Contains(dirs, path.Dir(p)) {
			dirs = append(dirs, path.Dir(p))
		}
	}
	return strings.Join(dirs, ":") + ":" + guest.UnitPath(home, guest.SystemDirs)
}
