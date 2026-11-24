package seat

import (
	"context"
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// ErrRunning is a seat whose member link is running for its team: an abandon refuses it.
var ErrRunning = errors.New("seat: its member link is running for its team")

// Abandoned is what an abandon did: Held is false for a seat no session held (nothing done),
// and Session is the session that held it.
type Abandoned struct {
	Held    bool
	Session paths.SessionName
}

// Abandon releases a held seat without its logout, on the owner's word (issue 386).
func (s *Store) Abandon(_ context.Context, _ Seat, _, _ string) (Abandoned, error) {
	return Abandoned{}, errors.New("seat: abandon is not built yet")
}
