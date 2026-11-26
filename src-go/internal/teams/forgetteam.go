package teams

import (
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/seatcheckouts"
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
