// Package seat is the fixture of project_seat_hold_unrecorded: holdings made without the
// session record.
package seat

// Sessions is the session records.
type Sessions interface {
	Restorable(session string) (bool, error)
}

// Store is the seat store.
type Store struct{ sessions Sessions }

func (s *Store) hold(_ string) error { return nil }

// Take is the originating form: it holds without asking the session record.
func (s *Store) Take(session string) error {
	return s.hold(session) // want `a seat holding made before the session record is asked \(project_seat_hold_unrecorded\)`
}

// Adopt makes a holding outside Take.
func (s *Store) Adopt(session string) error {
	if ok, err := s.sessions.Restorable(session); err != nil || !ok {
		return err
	}
	return s.hold(session) // want `a seat holding made outside Store.Take \(project_seat_hold_unrecorded\)`
}

// Late asks the record only after it holds.
func (s *Store) Late(session string) error {
	if err := s.hold(session); err != nil { // want `a seat holding made outside Store.Take \(project_seat_hold_unrecorded\)`
		return err
	}
	_, err := s.sessions.Restorable(session)
	return err
}
