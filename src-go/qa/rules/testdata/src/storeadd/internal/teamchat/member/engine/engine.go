// Package engine stands for teamchat's engine: the writes that queue into the seat store.
package engine

import (
	"errors"
	"fmt"
	"log"

	"storeadd/internal/teamchat/member/state"
)

// logged is the originating proof's receipt: the refusal is a log line, the entry lost.
func logged(s *state.Store, r state.Receipt) {
	if err := s.AddReceipt(r); err != nil { // want `a seat-store add's refusal that reaches no return \(project_store_refusal_lost\)`
		log.Printf("no receipt: %v", err)
	}
}

// twoStatements is the same loss, the error held in a variable first.
func twoStatements(s *state.Store, r state.Receipt) {
	err := s.AddReceipt(r) // want `a seat-store add's refusal that reaches no return \(project_store_refusal_lost\)`
	if err != nil {
		log.Print(err)
	}
}

// discarded drops the result outright, as a statement or into the blank identifier.
func discarded(s *state.Store, r state.Receipt) {
	s.AddReceipt(r)     // want `a seat-store add's refusal that reaches no return \(project_store_refusal_lost\)`
	_ = s.AddReceipt(r) // want `a seat-store add's refusal that reaches no return \(project_store_refusal_lost\)`
}

// matched answers ErrFull with something other than a refusal.
func matched(s *state.Store, r state.Receipt) error {
	if err := s.AddReceipt(r); errors.Is(err, state.ErrFull) { // want `a seat-store add's refusal that reaches no return \(project_store_refusal_lost\)`
		return nil
	}
	return nil
}

// inClosure loses it inside a function literal whose enclosing function returns an error.
func inClosure(s *state.Store, r state.Receipt) error {
	f := func() {
		if err := s.AddReceipt(r); err != nil { // want `a seat-store add's refusal that reaches no return \(project_store_refusal_lost\)`
			log.Print(err)
		}
	}
	f()
	return nil
}

// unchanged answers the refusal with a change func's "no change, no error": the write is
// skipped and the entry lost all the same.
func unchanged(s *state.Store, r state.Receipt) (bool, error) {
	if err := s.AddReceipt(r); err != nil { // want `a seat-store add's refusal that reaches no return \(project_store_refusal_lost\)`
		return false, nil
	}
	return true, nil
}

// translated answers the refusal with a refusal of its own: not flagged.
func translated(s *state.Store, r state.Receipt) (bool, error) {
	if err := s.AddReceipt(r); err != nil {
		return false, errors.New("the queue is full")
	}
	return true, nil
}

// refused returns the refusal, directly, wrapped or held first: none is flagged.
func refused(s *state.Store, r state.Receipt) error {
	if err := s.AddReceipt(r); err != nil {
		return fmt.Errorf("queue: %w", err)
	}
	err := s.AddReceipt(r)
	if err != nil {
		return err
	}
	return s.AddReceipt(r)
}

// rolled adds to a list whose add has no refusal: not flagged.
func rolled(s *state.Store) { s.AddLedger("$a") }
