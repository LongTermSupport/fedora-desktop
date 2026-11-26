package teams

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatcheckouts"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
)

// checkoutsHoldingSeats is every checkout that may keep seat records: the list of seat checkouts
// (which outlives a session), then the checkout of each running session, each once. An unreadable
// list or session record is an error naming it, since a checkout it names would be missed.
func checkoutsHoldingSeats(store *registry.Store) ([]string, error) {
	dirs, err := seatcheckouts.Of(store).Read()
	if err != nil {
		return nil, err
	}
	listing, err := store.List()
	if err != nil {
		return nil, err
	}
	if len(listing.UnreadableFiles) > 0 {
		return nil, fmt.Errorf("the session records %s could not be read, so a checkout they name may be missed",
			strings.Join(listing.UnreadableFiles, ", "))
	}
	for _, rec := range listing.Records {
		if !slices.Contains(dirs, rec.Dir) {
			dirs = append(dirs, rec.Dir)
		}
	}
	return dirs, nil
}

// recordSeats is the seats a session record's --teams names.
func recordSeats(rec registry.Record) ([]membership.Seat, error) {
	seats, err := membership.ParseSeats(strings.Join(strings.Fields(discover.RecordTeams(rec)), ""))
	if err != nil {
		return nil, fmt.Errorf("the --teams of session record %s: %w", rec.Name, err)
	}
	return seats, nil
}

// stoppedOn is every session record that still names team on the agent bus, by name (R36 review
// finding 3). It is asked before the team goes down, once its running sessions were taken off it,
// so each is a stopped session; onBus says whether a record's teams are on the agent bus, since a
// teamchat team of the same name is another team. An unreadable record, or one whose system
// cannot be told, is an error naming it: it may be in the team.
func stoppedOn(store *registry.Store, team string, onBus func(registry.Record) (bool, error)) ([]string, error) {
	listing, err := store.List()
	if err != nil {
		return nil, err
	}
	if len(listing.UnreadableFiles) > 0 {
		return nil, fmt.Errorf("the session records %s could not be read, so a stopped session in %s may be missed",
			strings.Join(listing.UnreadableFiles, ", "), team)
	}
	var names []string
	for _, rec := range listing.Records {
		seats, err := recordSeats(rec)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(seats, func(s membership.Seat) bool { return s.Team == team }) {
			continue
		}
		bus, err := onBus(rec)
		if err != nil {
			return nil, fmt.Errorf("whether session %s's team %s is the agent-bus team: %w", rec.Name, team, err)
		}
		if bus {
			names = append(names, rec.Name)
		}
	}
	slices.Sort(names)
	return names, nil
}

// takeStoppedOff takes team out of each named stopped session: its record keeps its other teams
// (no --teams when team was its only one), so a restore does not rejoin a team made again under
// the name, and its keeper's report is removed (never authority; the keeper writes it again when
// the session starts), so a seat lookup does not find the old team's conversation through it.
func takeStoppedOff(store *registry.Store, team string, names []string) error {
	for _, name := range names {
		rec, err := store.Get(name)
		if err != nil {
			return err
		}
		seats, err := recordSeats(rec)
		if err != nil {
			return err
		}
		if err := removeReport(rec.Dir, name); err != nil {
			return err
		}
		kept := slices.DeleteFunc(seats, func(s membership.Seat) bool { return s.Team == team })
		set := store.SetTeams
		if rec.Prefix == membership.KindCC {
			set = store.SetCCTeams
		}
		if err := set(name, membership.Canonical(kept)); err != nil {
			return fmt.Errorf("could not take session %s off %s: %w", name, team, err)
		}
	}
	return nil
}

// removeReport removes a session's keeper report inside its checkout only: the checkout is opened
// as an os.Root, so a link a container planted above the report (it can write the checkout) is
// refused instead of followed to a file of the host's outside it.
func removeReport(checkout, name string) error {
	report := membership.ReportPath(checkout, name)
	rel, err := filepath.Rel(checkout, report)
	if err != nil {
		return fmt.Errorf("session %s's keeper report %s: %w", name, report, err)
	}
	root, err := os.OpenRoot(checkout)
	if err != nil {
		return fmt.Errorf("could not open session %s's checkout %s: %w", name, checkout, err)
	}
	removeErr := root.Remove(rel)
	if errors.Is(removeErr, fs.ErrNotExist) {
		removeErr = nil
	}
	if removeErr != nil {
		removeErr = fmt.Errorf("could not remove session %s's keeper report %s inside its checkout: %w", name, report, removeErr)
	}
	return errors.Join(removeErr, root.Close())
}

// stoppedOnBus is Ops.StoppedIn over the host's registry: a record's system is read as a restore
// reads it (conversation.SystemOfRecord).
func stoppedOnBus(open func() (*registry.Store, error), getenv func(string) string, team string) ([]string, error) {
	store, err := open()
	if err != nil {
		return nil, err
	}
	return stoppedOn(store, team, func(rec registry.Record) (bool, error) {
		sys, err := conversation.SystemOfRecord(rec, getenv)
		return sys == system.AgentBus, err
	})
}

// takeStoppedOffIn is Ops.TakeStoppedOff over the host's registry.
func takeStoppedOffIn(open func() (*registry.Store, error), team string, names []string) error {
	store, err := open()
	if err != nil {
		return err
	}
	return takeStoppedOff(store, team, names)
}

// forgetTeam removes the seat records of team in every checkout that holds seats (issue 417).
func forgetTeam(open func() (*registry.Store, error), team string) error {
	store, err := open()
	if err != nil {
		return err
	}
	dirs, err := checkoutsHoldingSeats(store)
	if err != nil {
		return err
	}
	return conversation.ForgetTeam(dirs, team)
}
