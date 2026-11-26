package admin

import (
	"context"
	"errors"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore/drill"
)

// Floors implements drill.Probe: each token member's floor marker in the main room, read with
// its current token; none is "".
func (r *DrillRig) Floors(ctx context.Context, _ paths.TeamID) (map[string]string, error) {
	out := map[string]string{}
	for _, h := range r.handles() {
		err := r.asMember(ctx, h, func(c *matrix.Client, user string) error {
			raw, found, err := c.RoomAccountData(ctx, user, r.main, paths.FloorAccountData)
			out[h] = ""
			if found {
				out[h] = string(raw)
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// LiveEpoch implements drill.Probe: restore.check of the live server through the child (RC8).
func (r *DrillRig) LiveEpoch(ctx context.Context, team paths.TeamID) (string, error) {
	tf, err := r.teamFile(team)
	if err != nil {
		return "", err
	}
	e, err := r.Engine.Child.Check(ctx, team, nil, restore.CheckArgs{TeamFile: tf})
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(e.RestoreEpoch, 10), nil
}

// MissingEvents implements drill.Probe: each event the kept member cannot fetch from the main
// room because the server does not hold it.
func (r *DrillRig) MissingEvents(ctx context.Context, _ paths.TeamID, events []string) ([]string, error) {
	var missing []string
	err := r.asMember(ctx, r.kept, func(c *matrix.Client, _ string) error {
		for _, id := range events {
			_, err := c.GetEvent(ctx, r.main, id)
			switch {
			case errors.Is(err, matrix.ErrNotFound):
				missing = append(missing, id)
			case err != nil:
				return err
			}
		}
		return nil
	})
	return missing, err
}

// Accepts implements drill.Probe: a password login for the drill's human, signed out again, or
// whoami for a token; a refusal is false, any other failure an error.
func (r *DrillRig) Accepts(ctx context.Context, _ paths.TeamID, c drill.Credential) (bool, error) {
	if c.Handle == drillHuman {
		return r.login(ctx, c.Secret)
	}
	m, err := r.member(c.Secret)
	if err != nil {
		return false, err
	}
	defer m.Close()
	_, err = m.Whoami(ctx)
	if errors.Is(err, matrix.ErrAuthRefused) {
		return false, nil
	}
	return err == nil, err
}

// login is a password login as the drill's human through a client of the kept member's (the
// login sends no token), its new device signed out again.
func (r *DrillRig) login(ctx context.Context, password string) (bool, error) {
	anon, err := r.member(r.tokens[r.kept])
	if err != nil {
		return false, err
	}
	defer anon.Close()
	sess, err := anon.Login(ctx, drillHuman, password, drillDeviceName)
	if errors.Is(err, matrix.ErrForbidden) || errors.Is(err, matrix.ErrAuthRefused) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	signed, err := anon.WithToken(sess.AccessToken.Reveal())
	if err != nil {
		return false, err
	}
	defer signed.Close()
	return true, signed.Logout(ctx)
}
