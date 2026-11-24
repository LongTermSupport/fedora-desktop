// Package otherstore has a Store with a Remove and a Dir that are not the registry's.
package otherstore

// Store is not the session registry.
type Store struct{}

// Dir is not the registry directory.
func (Store) Dir() string { return "/elsewhere" }

// Remove is not the registry's.
func (Store) Remove(_ string) error { return nil }
