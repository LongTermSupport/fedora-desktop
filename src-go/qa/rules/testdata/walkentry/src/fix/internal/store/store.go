// Package store stands in for the seat store's path helpers: a session's name parsed from a
// folder name, and the file inside that folder.
package store

// Store is a seat store rooted at a directory.
type Store struct{ root string }

// Session is a parsed session folder name.
type Session string

// Parse turns a folder name into a session name.
func Parse(name string) (Session, error) { return Session(name), nil }

// DesiredFile is the session's desired.json, inside its folder under control/.
func (s Store) DesiredFile(session Session) string {
	return s.root + "/control/" + string(session) + "/desired.json"
}
