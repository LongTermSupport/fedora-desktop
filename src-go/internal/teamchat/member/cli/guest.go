package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/guest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// identify is GU1's check of the caller against --user, with the user's home opened as the
// ops' root.
var identify = identifyUnbuilt

// memberLayout is the caller's own layout, whose state root `seat set-aside` acts on without
// --state.
var memberLayout = processLayout

func identifyUnbuilt(Invocation) (guest.Home, error) {
	return guest.Home{}, errors.New("cli: the guest identity check is not joined to this binary")
}

// processLayout is the home layout of this process's HOME and XDG_RUNTIME_DIR.
func processLayout() (paths.Layout, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return paths.Layout{}, fmt.Errorf("cli: %w", err)
	}
	return paths.HomeLayout(home, os.Getenv("XDG_RUNTIME_DIR"))
}

// changedLines prints each change as CHANGED<TAB><what> on stdout (GU1).
type changedLines struct{ w io.Writer }

func (c changedLines) Changed(what string) error {
	_, err := fmt.Fprintf(c.w, "CHANGED\t%s\n", what)
	return err
}

// systemClock is the ops' time: the wall clock, and the seat lock's real waits.
type systemClock struct{ lock.Clock }

func (systemClock) Now() time.Time { return time.Now() }

func clock() guest.Clock { return systemClock{lock.SystemClock()} }

// asGuest runs op with the identified guest's home, closed after it.
func asGuest(e Env, inv Invocation, op func(guest.Home) error) term.ExitCode {
	h, err := identify(inv)
	if err == nil {
		err = errors.Join(op(h), h.Root.Close())
	}
	return exits.Write(e.Stderr, err)
}

func runPlaceBundle(e Env, inv Invocation) term.ExitCode {
	return asGuest(e, inv, func(h guest.Home) error {
		return guest.PlaceBundle(context.Background(), h, clock(), e.Stdin, changedLines{e.Stdout})
	})
}

func runMembership(e Env, inv Invocation) term.ExitCode {
	return asGuest(e, inv, func(h guest.Home) error {
		return guest.Membership(h, e.Stdin, changedLines{e.Stdout})
	})
}

func runGuestRemove(e Env, inv Invocation) term.ExitCode {
	return asGuest(e, inv, func(h guest.Home) error {
		return guest.Remove(context.Background(), h, clock(), *inv.Team, changedLines{e.Stdout})
	})
}

func runSetAside(e Env, inv Invocation) term.ExitCode {
	dir, given := inv.Flag("state")
	if !given {
		l, err := memberLayout()
		if err != nil {
			return exits.Write(e.Stderr, err)
		}
		dir = l.State()
	}
	return exits.Write(e.Stderr, guest.SetAside(context.Background(), clock(), dir, *inv.Team, changedLines{e.Stdout}))
}
