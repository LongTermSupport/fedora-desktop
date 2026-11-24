// Package seat is the clean fixture of project_seat_hold_unrecorded.
package seat

import "errors"

// Sessions is the session records.
type Sessions interface {
	Restorable(session string) (bool, error)
}

// Store is the seat store.
type Store struct{ sessions Sessions }

func (s *Store) hold(_ string) error { return nil }

// Take asks the session record, then holds.
func (s *Store) Take(session string) error {
	recorded, err := s.sessions.Restorable(session)
	if err != nil {
		return err
	}
	if !recorded {
		return errors.New("no record")
	}
	return s.hold(session)
}
