package lxc

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestfacts"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// restartVerb is every `lxc restart` error's verb.
const restartVerb = "lxc restart"

// The offline causes only `lxc restart` meets.
const (
	whyStoppedRestart = "offline: container stopped during the restart"
	whyBusDown        = "offline: the guest's systemd is unreachable"
)

// Restart is `lxc restart --container C --user U` (SU3): the user's session unit reset, then
// restarted, over the guest's systemd (SU2). It is an explicit target, so a container that is
// missing, not running, or that stops on the way, and a systemd that cannot be dialled, is an
// *OfflineError (69). A member with no unit file, or one TB1 refuses, is refused by name (70).
func Restart(ctx context.Context, d Deps, container paths.ContainerName, user paths.GuestUser) (err error) {
	root, err := restartRoot(ctx, d, container)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	file := paths.GuestUnitFile(user)
	_, _, found, err := root.ReadFile(file, maxUnit)
	if err == nil && !found {
		err = fmt.Errorf("%w: %s@%s has no session unit %s; lxc apply places it", guestfacts.ErrRefused, user, container, file)
	}
	if err != nil {
		return &host.Error{Verb: restartVerb, Step: "unit", Drift: refused(err), Err: err}
	}
	s, err := d.Bus(ctx, root)
	if err != nil && !refused(err) {
		return &OfflineError{Container: container, Why: whyBusDown}
	}
	if err == nil {
		err = errors.Join(restartUnit(ctx, s, user), s.Close())
	}
	if errors.Is(err, guestbus.ErrGone) {
		return &OfflineError{Container: container, Why: whyStoppedRestart}
	}
	if err != nil {
		return &host.Error{Verb: restartVerb, Step: "restart", Drift: refused(err), Err: fmt.Errorf("%s@%s: %w", user, container, err)}
	}
	return nil
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

func restartUnit(ctx context.Context, s guestbus.Systemd, user paths.GuestUser) error {
	if err := s.ResetFailed(ctx, user); err != nil {
		return err
	}
	return s.Restart(ctx, user)
}
