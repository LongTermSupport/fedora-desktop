// Package engine stands for teamchat's engine: the writes that settle a delivery.
package engine

import "ledger/internal/teamchat/member/state"

// written is the originating written-yes write: the verb is dropped.
func written(p state.Pending, now int64) state.Ledger {
	return state.Ledger{ID: p.ID, Item: p.Item, AtMS: now} // want `a ledger entry built field by field \(project_ledger_built_by_hand\)`
}

// proven builds the entry as an element of a slice, and through a pointer.
func proven(p state.Pending, now int64) ([]state.Ledger, *state.Ledger) {
	list := []state.Ledger{{ID: p.ID, Item: p.Item, AtMS: now, Verb: p.Verb}} // want `a ledger entry built field by field \(project_ledger_built_by_hand\)`
	return list, &state.Ledger{ID: p.ID} // want `a ledger entry built field by field \(project_ledger_built_by_hand\)`
}

// positional builds it without keys.
func positional(p state.Pending, now int64) state.Ledger {
	return state.Ledger{p.ID, p.Item, now, p.Verb} // want `a ledger entry built field by field \(project_ledger_built_by_hand\)`
}

// assigned fills a declared entry one field at a time.
func assigned(p state.Pending) state.Ledger {
	var l state.Ledger
	l.ID = p.ID // want `a ledger entry built field by field \(project_ledger_built_by_hand\)`
	return l
}

// settled uses the constructor, and an empty entry stands for "none": neither is flagged.
func settled(p state.Pending, now int64, ok bool) state.Ledger {
	if !ok {
		return state.Ledger{}
	}
	return state.LedgerOf(p, now)
}
