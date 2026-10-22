package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/guest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// identify is GU1's check of the caller against --user, with the user's home opened as the
// ops' root; a seam, so a test need not run as another user.
var identify = identifyCaller

// memberLayout is the caller's own layout, whose state root `seat set-aside` acts on without
// --state.
var memberLayout = processLayout

const (
	msgGuestUser  exits.Template = "teamchat guest takes --user U"
	msgGuestFlags exits.Template = "teamchat guest check takes --user U and --team T"
)

// guestUser is --user, which Parse has checked; an Invocation without it is refused rather
// than trusted.
func guestUser(inv Invocation) (paths.GuestUser, error) {
	name, given := inv.Flag(userFlag.Name)
	u, err := paths.ParseGuestUser(name)
	if err != nil || !given {
		return paths.GuestUser{}, usage(msgGuestUser)
	}
	return u, nil
}

// identifyCaller is guest.Establish on this process for --user.
func identifyCaller(inv Invocation) (*guest.Identity, error) {
	u, err := guestUser(inv)
	if err != nil {
		return nil, err
	}
	return guest.Establish(guest.OS{}, u)
}

// processLayout is the home layout of this process's HOME and XDG_RUNTIME_DIR.
func processLayout() (paths.Layout, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return paths.Layout{}, fmt.Errorf("cli: %w", err)
	}
	return paths.HomeLayout(home, os.Getenv("XDG_RUNTIME_DIR"))
}

// systemClock is the ops' time: the wall clock, and the seat lock's real waits.
type systemClock struct{ lock.Clock }

func (systemClock) Now() time.Time { return time.Now() }

func clock() guest.Clock { return systemClock{lock.SystemClock()} }

// asGuest runs op as the identified guest, with its output contract, the home closed after.
func asGuest(e Env, inv Invocation, op guest.Op, run func(*guest.Identity, *guest.Output) error) term.ExitCode {
	id, err := identify(inv)
	if err == nil {
		err = errors.Join(run(id, guest.NewOutput(e.Stdout, op)), id.Close())
	}
	return guest.Finish(e.Stderr, op, err)
}

// runGuestCheck is `guest check --user U --team T` on the real guest (GU2); the checker
// establishes the identity itself.
func runGuestCheck(e Env, inv Invocation) term.ExitCode {
	u, err := guestUser(inv)
	if err != nil || inv.Team == nil {
		return guest.Finish(e.Stderr, guest.OpCheck, usage(msgGuestFlags))
	}
	return guest.Finish(e.Stderr, guest.OpCheck, guest.NewChecker().Check(context.Background(), u, *inv.Team, e.Stdin))
}

func runPlaceBundle(e Env, inv Invocation) term.ExitCode {
	return asGuest(e, inv, guest.OpPlaceBundle, func(id *guest.Identity, out *guest.Output) error {
		return guest.PlaceBundle(context.Background(), id, clock(), e.Stdin, out)
	})
}

func runMembership(e Env, inv Invocation) term.ExitCode {
	return asGuest(e, inv, guest.OpMembership, func(id *guest.Identity, out *guest.Output) error {
		return guest.Membership(id, e.Stdin, out)
	})
}

func runGuestRemove(e Env, inv Invocation) term.ExitCode {
	return asGuest(e, inv, guest.OpRemove, func(id *guest.Identity, out *guest.Output) error {
		return guest.Remove(context.Background(), id, clock(), *inv.Team, out)
	})
}

func runSetAside(e Env, inv Invocation) term.ExitCode {
	dir, given := inv.Flag("state")
	if !given {
		l, err := memberLayout()
		if err != nil {
			return guest.Finish(e.Stderr, guest.OpSetAside, err)
		}
		dir = l.State()
	}
	return guest.Finish(e.Stderr, guest.OpSetAside, guest.SetAside(context.Background(), clock(), dir, *inv.Team, guest.NewOutput(e.Stdout, guest.OpSetAside)))
}
