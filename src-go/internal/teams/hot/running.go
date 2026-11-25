package hot

import (
	"errors"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
)

// Bus is which generation of the agent team bus a running session's membership is on.
type Bus int

const (
	// BusNone is a session with no membership of either bus: launched off the bus, or by a ccy
	// older than the kit.
	BusNone Bus = iota
	// BusV1 is agent-bus: the membership directory under $XDG_RUNTIME_DIR.
	BusV1
	// BusV2 is teamchat: the session's desired.json in the owner's seat store.
	BusV2
)

// Running is a running session's own membership as the lookup found it: v1's desired membership
// (BusV1), or a teamchat session's seats (BusV2). Dir is the session's v1 membership directory,
// whichever bus it is on.
type Running struct {
	Bus Bus
	Dir string
	V1  membership.Desired
	V2  TeamchatSeats
}

// Lookup is the one lookup of a running session's membership (register R51): every host reader
// of one (session list and add, the picker's view, SessionsIn, ccy-teams restart) asks it, so
// none of them can see a session the others cannot.
type Lookup struct {
	// Teamchat opens a session's v2 seats in its checkout, found false for none; nil finds v1
	// sessions only.
	Teamchat func(checkout, session string) (TeamchatSeats, bool, error)
}

// Lookup is the lookup over this command's teamchat opener.
func (x *Hot) Lookup() Lookup { return Lookup{Teamchat: x.Teamchat} }

// Running is the membership of the session of rec, of the kind (ccy, cc): its v1 membership
// directory's, or its v2 desired.json in the record's checkout, else BusNone. With both on disk
// (an earlier launch of the name on the other bus left its file, register R65) it is the one
// written later, which is this launch's: each launch writes its own fresh. A record whose own
// launch left the bus out owns nothing under its name (Fable's M6 review B1). A membership that
// cannot be read is an error, with Bus naming whose.
func (l Lookup) Running(runtime, kind string, rec registry.Record) (Running, error) {
	run := Running{Dir: membership.DirOf(runtime, kind, rec.Name)}
	if !discover.LaunchedWithBus(rec) {
		return run, nil
	}
	if info, err := os.Lstat(run.Dir); err == nil && info.IsDir() {
		d, readErr := membership.Read(run.Dir)
		switch {
		case errors.Is(readErr, os.ErrNotExist):
			return run, nil
		case readErr != nil:
			run.Bus = BusV1
			return run, readErr
		}
		run.Bus, run.V1 = BusV1, d
	}
	if l.Teamchat == nil {
		return run, nil
	}
	seats, found, err := l.Teamchat(rec.Dir, rec.Name)
	if err != nil {
		run.Bus = BusV2
		return run, err
	}
	if found && (run.Bus == BusNone || seats.Desired().WrittenMS > run.V1.WrittenMS) {
		run.Bus, run.V1, run.V2 = BusV2, membership.Desired{}, seats
	}
	return run, nil
}
