// Package engine stands for teamchat's engine: memory beside a store written through change.
package engine

import "errors"

// Store is the store copy a change function edits.
type Store struct{ Pending []string }

type held struct {
	content map[string]string
	count   int
	store   *Store
}

// Engine holds the teams in memory.
type Engine struct {
	teams map[string]*held
	note  string
	lost  int
}

var errRefused = errors.New("refused")

func (e *Engine) changeLocked(id string, f func(h *held, s *Store) (bool, error)) error {
	h := e.teams[id]
	s := *h.store
	if _, err := f(h, &s); err != nil {
		return err
	}
	return errRefused
}

func (e *Engine) change(id string, f func(h *held, s *Store) (bool, error)) error {
	return e.changeLocked(id, f)
}

func (e *Engine) logf(string) { e.lost++ }

func (e *Engine) hit() { e.lost++ }

// settle is recv's settle: content forgotten in a helper the change function calls.
func (e *Engine) settle(id string) error {
	return e.changeLocked(id, func(h *held, s *Store) (bool, error) {
		forget(h, s) // want `engine memory written inside a store write's change function \(project_memory_ahead_of_store\)`
		return true, nil
	})
}

func forget(h *held, s *Store) {
	for _, id := range s.Pending {
		delete(h.content, id)
	}
	s.Pending = nil
}

// direct writes memory in the literal itself, through the held team and the engine.
func (e *Engine) direct(id string) error {
	return e.change(id, func(h *held, s *Store) (bool, error) {
		h.count++             // want `engine memory written inside a store write's change function \(project_memory_ahead_of_store\)`
		e.note = "written"    // want `engine memory written inside a store write's change function \(project_memory_ahead_of_store\)`
		clear(h.content)      // want `engine memory written inside a store write's change function \(project_memory_ahead_of_store\)`
		e.teams[id].count = 2 // want `engine memory written inside a store write's change function \(project_memory_ahead_of_store\)`
		s.Pending = s.Pending[:0]
		return true, nil
	})
}

// deep reaches the write through two calls.
func (e *Engine) deep(id string) error {
	return e.changeLocked(id, func(_ *held, s *Store) (bool, error) {
		e.outer() // want `engine memory written inside a store write's change function \(project_memory_ahead_of_store\)`
		return len(s.Pending) > 0, nil
	})
}

func (e *Engine) outer() { e.inner() }

func (e *Engine) inner() { e.note = "" }

// after is the fix: the change function fills closure variables, memory follows the write.
func (e *Engine) after(id string) error {
	var gone []string
	if err := e.changeLocked(id, func(_ *held, s *Store) (bool, error) {
		gone = append(gone, s.Pending...)
		s.Pending = nil
		e.logf("settled")
		e.hit()
		return true, nil
	}); err != nil {
		return err
	}
	for _, g := range gone {
		delete(e.teams[id].content, g)
	}
	return nil
}
