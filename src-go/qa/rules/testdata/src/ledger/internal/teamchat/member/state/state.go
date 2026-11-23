// Package state stands for teamchat's seat store: a pending item, a ledger entry and the one
// constructor between them.
package state

// Pending is an item the store owes.
type Pending struct {
	ID   string
	Item string
	Verb *string
}

// Ledger is a delivered item.
type Ledger struct {
	ID   string
	Item string
	AtMS int64
	Verb *string
}

// LedgerOf is the one place an entry is built from an item: not flagged here.
func LedgerOf(p Pending, at int64) Ledger {
	l := Ledger{ID: p.ID, Item: p.Item, AtMS: at, Verb: p.Verb}
	l.AtMS = at
	return l
}
