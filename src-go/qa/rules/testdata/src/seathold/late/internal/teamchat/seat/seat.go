// Package seat is the fixture of project_seat_hold_unrecorded where Take asks the session record
// only after it holds.
package seat

// Sessions is the session records.
type Sessions interface {
	Restorable(session string) (bool, error)
}

// Store is the seat store.
type Store struct{ sessions Sessions }

func (s *Store) hold(_ string) error { return nil }

// Take holds, then asks the record: too late.
func (s *Store) Take(session string) error {
	if err := s.hold(session); err != nil { // want `a seat holding made before the session record is asked \(project_seat_hold_unrecorded\)`
		return err
	}
	_, err := s.sessions.Restorable(session)
	return err
}
