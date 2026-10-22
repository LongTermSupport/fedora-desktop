package lxc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestfacts"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// restartVerb is every `lxc restart` error's verb.
const restartVerb = "lxc restart"

// The offline causes only `lxc restart` meets.
const (
	whyStoppedRestart = "offline: container stopped during the restart"
	whyBusDown        = "offline: the guest's systemd is unreachable"
)

// Restart is `lxc restart --member C/K` (SU3): the seat's session unit reset, then restarted,
// over the guest's systemd (SU2); no other seat's unit is touched. The seat is the record entry
// not left whose key in container is key, in any team's record; none is refused by name (70).
// It is an explicit target, so a container that is missing, not running, or that stops on the
// way, and a systemd that cannot be dialled, is an *OfflineError (69). A seat with no unit
// file, or one TB1 refuses, is refused by name (70).
func Restart(ctx context.Context, d Deps, container paths.ContainerName, key paths.SeatKey) (err error) {
	id, err := restartSeat(d, container, key)
	if err != nil {
		return err
	}
	root, err := restartRoot(ctx, d, container)
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
		err = fmt.Errorf("%w: %s has no session unit %s; lxc apply places it", guestfacts.ErrRefused, seat, file)
	}
	if err != nil {
		return &host.Error{Verb: restartVerb, Step: "unit", Drift: refused(err), Err: err}
	}
	s, err := d.Bus(ctx, root)
	if err != nil && !refused(err) {
		return &OfflineError{Container: container, Why: whyBusDown}
	}
	if err == nil {
		err = errors.Join(restartUnit(ctx, s, paths.SessionUnit(id.Team, id.Checkout, id.Seat)), s.Close())
	}
	if errors.Is(err, guestbus.ErrGone) {
		return &OfflineError{Container: container, Why: whyStoppedRestart}
	}
	if err != nil {
		return &host.Error{Verb: restartVerb, Step: "restart", Drift: refused(err), Err: fmt.Errorf("%s: %w", seat, err)}
	}
	return nil
}

// restartSeat is the seat of key in container from the teams' records under the host root: the
// one entry not left whose seat has that key.
func restartSeat(d Deps, container paths.ContainerName, key paths.SeatKey) (lxcstate.SeatID, error) {
	teams, err := recordTeams(d.Root)
	if err != nil {
		return lxcstate.SeatID{}, &host.Error{Verb: restartVerb, Step: "record", Err: err}
	}
	var seats []lxcstate.SeatID
	for _, team := range teams {
		rec, _, err := lxcstate.ReadRecord(d.Root, team)
		if err != nil {
			return lxcstate.SeatID{}, &host.Error{Verb: restartVerb, Step: "record", Err: err}
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
		return lxcstate.SeatID{}, &host.Error{Verb: restartVerb, Step: "record", Drift: true,
			Err: fmt.Errorf("--member %s names no seat of any team's LXC record; lxc apply places it", seat)}
	}
	return lxcstate.SeatID{}, &host.Error{Verb: restartVerb, Step: "record", Drift: true,
		Err: fmt.Errorf("--member %s names a seat key two records hold; lxc apply refuses it", seat)}
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
func restartRoot(ctx context.Context, d Deps, container paths.ContainerName) (Root, error) {
	c := container.String()
	names, err := d.LXC.Containers(ctx)
	if err != nil {
		return nil, &host.Error{Verb: restartVerb, Step: "state", Err: err}
	}
	if !slices.Contains(names, c) {
		return nil, &OfflineError{Container: container, Why: whyMissing}
	}
	if st, stateErr := d.LXC.State(ctx, c); stateErr != nil || st != lxccmd.Running {
		return nil, &OfflineError{Container: container, Why: whyStopped}
	}
	root, err := d.Pin(ctx, c)
	if errors.Is(err, guestroot.ErrMoved) || errors.Is(err, guestroot.ErrGone) {
		return nil, &OfflineError{Container: container, Why: whyStoppedRestart}
	}
	if err != nil {
		return nil, &host.Error{Verb: restartVerb, Step: "pin", Err: err}
	}
	return root, nil
}

func restartUnit(ctx context.Context, s guestbus.Systemd, name string) error {
	if err := s.ResetFailed(ctx, name); err != nil {
		return err
	}
	return s.Restart(ctx, name)
}
