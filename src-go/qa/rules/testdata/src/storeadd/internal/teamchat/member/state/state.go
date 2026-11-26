// Package state stands for teamchat's seat store: a capped list whose add refuses when full,
// and a rolling list whose add cannot refuse.
package state

import "errors"

// ErrFull is a list with no room.
var ErrFull = errors.New("full")

// Receipt is a queued entry.
type Receipt struct{ ID string }

// Store is the seat store.
type Store struct {
	Receipts []Receipt
	Ledger   []string
}

// AddReceipt refuses when the list is full.
func (s *Store) AddReceipt(r Receipt) error {
	if len(s.Receipts) >= 2 {
		return ErrFull
	}
	s.Receipts = append(s.Receipts, r)
	return nil
}

// AddLedger rolls: it has no refusal to lose.
func (s *Store) AddLedger(id string) { s.Ledger = append(s.Ledger, id) }

// Fill is the store's own use: package state is not judged.
func Fill(s *Store) {
	if err := s.AddReceipt(Receipt{}); err != nil {
		s.Ledger = nil
	}
}
