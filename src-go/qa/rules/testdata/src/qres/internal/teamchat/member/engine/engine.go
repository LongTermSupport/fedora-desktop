// Package engine stands for teamchat's engine: the delivery's items and recv's records.
package engine

import (
	"qres/internal/teamchat/member/deliver"
	"qres/internal/teamchat/member/records"
	"qres/internal/teamchat/member/team"
	"qres/internal/teamchat/proto"
)

// The originating defect: the delivery's item and recv's record built with no question asked.
func undelivered(id string) (deliver.Item, records.Human) {
	it := deliver.Item{ID: id} // want `a deliver.Item built without its question \(project_question_unresolved\)`
	h := records.Human{ID: id} // want `a records.Human built without its question \(project_question_unresolved\)`
	return it, h
}

// derived is the question decided by hand, beside the resolver: flagged at each value.
func derived(id string, n int) (deliver.Item, records.Verified) {
	none := "none"
	it := deliver.Item{ID: id, Question: &proto.EntryQuestion{N: n}} // want `a deliver.Item's Question not from team.InQuestion \(project_question_unresolved\)`
	v := records.Verified{ID: id, Question: &none, Headline: nil}    // want `a records.Verified's Question not from team.InQuestion \(project_question_unresolved\)` `a records.Verified's Headline not from team.InQuestion \(project_question_unresolved\)`
	v.Question = &none                                               // want `a records.Verified's Question not from team.InQuestion \(project_question_unresolved\)`
	it.Question = nil                                                // want `a deliver.Item's Question not from team.InQuestion \(project_question_unresolved\)`
	return it, v
}

// ownPath is verify's original form: the question and headline from a derivation of its own.
func ownPath(v *records.Verified) {
	v.Question, v.Headline = byHand() // want `a records.Verified's Question not from team.InQuestion \(project_question_unresolved\)` `a records.Verified's Headline not from team.InQuestion \(project_question_unresolved\)`
}

func byHand() (*string, *string) { return nil, nil }

// resolved asks the one resolver for every value: not flagged, nor are empty literals, another
// record's fields, or a pointer's assignment.
func resolved(id string, q team.InQuestion) (deliver.Item, records.Human, records.Verified, records.Notice) {
	it := deliver.Item{ID: id, Question: q.Entry()}
	h := records.Human{ID: id, Question: q.Name(), Headline: q.HeadlineName()}
	v := records.Verified{}
	v.Question, v.Headline = q.Name(), q.HeadlineName()
	p := &h
	p.Question = q.Name()
	return it, h, v, records.Notice{ID: id}
}
