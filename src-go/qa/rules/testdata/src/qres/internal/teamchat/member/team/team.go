// Package team stands for teamchat's team seam: the one resolver's answer.
package team

import "qres/internal/teamchat/proto"

// InQuestion is which question an event is in.
type InQuestion struct{ n int64 }

// Entry is the delivery's form.
func (q InQuestion) Entry() *proto.EntryQuestion { return nil }

// Name is a record's question.
func (q InQuestion) Name() *string { return nil }

// HeadlineName is a record's headline.
func (q InQuestion) HeadlineName() *string { return nil }
