package lxc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestfacts"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
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
type seatJob struct {
	verb, step, whyStopped string
	run                    func(ctx context.Context, s guestbus.Systemd, unit string) error
}

var (
	restartJob = seatJob{verb: restartVerb, step: "restart", whyStopped: whyStoppedRestart, run: restartUnit}
	startJob   = seatJob{verb: startVerb, step: "start", whyStopped: whyStoppedStart, run: startUnit}
)

// Restart is `lxc restart --member C/K` (SU3): the seat's session unit reset, then restarted,
// over the guest's systemd (SU2); no other seat's unit is touched. The seat is the record entry
// not left whose key in container is key, in any team's record; none is refused by name (70).
// It is an explicit target, so a container that is missing, not running, or that stops on the
// way, and a systemd that cannot be dialled, is an *OfflineError (69). A seat with no unit
// file, or one TB1 refuses, is refused by name (70).
func Restart(ctx context.Context, d Deps, container paths.ContainerName, key paths.SeatKey) error {
	return runSeatJob(ctx, d, container, key, restartJob)
}

// Start is `lxc start --member C/K` (SU4): the seat's session unit reset, then started with
// systemd's StartUnit, which leaves a unit that already runs as it is, so a running session is
// never ended. The seat, the refusals and the offline causes are Restart's, in the start's words.
func Start(ctx context.Context, d Deps, container paths.ContainerName, key paths.SeatKey) error {
	return runSeatJob(ctx, d, container, key, startJob)
}

// runSeatJob finds the seat, pins its container and runs job on its unit alone.
func runSeatJob(ctx context.Context, d Deps, container paths.ContainerName, key paths.SeatKey, job seatJob) (err error) {
	id, err := restartSeat(d, job.verb, container, key)
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

// restartSeat is the seat of key in container from the teams' records under the host root: the
// one entry not left whose seat has that key.
func restartSeat(d Deps, verb string, container paths.ContainerName, key paths.SeatKey) (lxcstate.SeatID, error) {
	teams, err := recordTeams(d.Root)
	if err != nil {
		return lxcstate.SeatID{}, &host.Error{Verb: verb, Step: stepRecord, Err: err}
	}
	var seats []lxcstate.SeatID
	for _, team := range teams {
		rec, _, err := lxcstate.ReadRecord(d.Root, team)
		if err != nil {
			return lxcstate.SeatID{}, &host.Error{Verb: verb, Step: stepRecord, Err: err}
		}
		for _, e := range rec.Members {
			if e.Container == container && e.Status != lxcstate.StatusLeft && rec.SeatOf(e).Key() == key {
				seats = append(seats, rec.SeatOf(e))
			}
		}
	}
	seat := container.String() + "/" + key.String()
	switch len(seats) {
	case 1:
		return seats[0], nil
	case 0:
		return lxcstate.SeatID{}, &host.Error{
			Verb: verb, Step: stepRecord, Drift: true,
			Err: fmt.Errorf("--member %s names no seat of any team's LXC record; lxc apply places it", seat),
		}
	}
	return lxcstate.SeatID{}, &host.Error{
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
