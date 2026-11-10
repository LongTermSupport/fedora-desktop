package teamchatlaunch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/client"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// RegistrySessions is the seat store's session records (seat.Sessions): a ccy session is
// restorable while ccy's session registry holds its record (00203 H5), the record the launcher
// removes before it releases (seam 54).
type RegistrySessions struct {
	Open func() (*registry.Store, error)
}

// Restorable is whether the session's record is still there.
func (r RegistrySessions) Restorable(n paths.SessionName) (bool, error) {
	store, err := r.Open()
	if err != nil {
		return false, fmt.Errorf("open the session registry: %w", err)
	}
	_, err = store.Get(n.String())
	switch {
	case errors.Is(err, registry.ErrNoRecord):
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

// Announcer is the seat store's notice poster (seat.Announcer): area B's
// `teamchat seat disconnected --bundle <held> --since <ms> --seed <run>` (C 3.7.5), run in this
// process as the owner. Its exit 0 with a line on stderr is a notice that can never be posted
// (the why); any other exit is transient, so the store retries it. The verb bounds its own
// call to the homeserver.
type Announcer struct {
	// Verb is the seat disconnected verb; nil is area B's own.
	Verb func(cli.Env, cli.Invocation) term.ExitCode
}

// Disconnected posts the notice.
func (a Announcer) Disconnected(_ context.Context, n seat.Notice) (seat.Outcome, error) {
	inv, err := cli.Parse([]string{
		"seat", "disconnected", "--bundle", n.Bundle,
		"--since", strconv.FormatInt(n.SinceMS, 10), "--seed", n.Run.String(),
	})
	if err != nil {
		return seat.Outcome{}, fmt.Errorf("seat disconnected: %w", err)
	}
	var out, stderr bytes.Buffer
	verb := a.Verb
	if verb == nil {
		verb = client.SeatDisconnected
	}
	code := verb(cli.Env{Stdout: &out, Stderr: &stderr}, inv)
	why := strings.TrimSpace(stderr.String())
	if code != term.ExitOK {
		return seat.Outcome{}, fmt.Errorf("seat disconnected exited %d: %s", code, why)
	}
	return seat.Outcome{Posted: why == "", Why: why}, nil
}

// StoreTaker takes a launch's seats from the real seat store, under one allocation lock; a take
// that fails releases the seats taken before it, so a refused launch holds nothing. A seat the
// launch's session already holds (a relaunch, a restore: its record and seat directory kept) is
// kept as it is and not taken again.
type StoreTaker struct {
	Open func() (*seat.Store, error)
}

// TakeAll takes every seat or none.
func (s StoreTaker) TakeAll(ctx context.Context, takes []seat.Take) (err error) {
	store, err := s.Open()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	alloc, err := store.Lock(ctx)
	if err != nil {
		return err
	}
	holdings, err := store.Holdings()
	if err != nil {
		return errors.Join(err, alloc.Unlock())
	}
	var taken []seat.Take
	for _, t := range takes {
		if heldBy(holdings, t) {
			continue
		}
		if _, err = store.Take(ctx, alloc, t); err != nil {
			err = fmt.Errorf("take seat %s of team %s: %w", t.Seat.Name, t.Seat.Team, err)
			break
		}
		taken = append(taken, t)
	}
	if unlockErr := alloc.Unlock(); unlockErr != nil {
		err = errors.Join(err, unlockErr)
	}
	if err == nil {
		return nil
	}
	for _, t := range taken {
		err = errors.Join(err, store.Release(ctx, seat.Release{Seat: t.Seat, Session: t.Session}))
	}
	return err
}

// heldBy is whether the take's session already holds its seat.
func heldBy(holdings []seat.Holding, t seat.Take) bool {
	for _, h := range holdings {
		if h.Complete && h.Seat == t.Seat && h.Record.Session == t.Session {
			return true
		}
	}
	return false
}
