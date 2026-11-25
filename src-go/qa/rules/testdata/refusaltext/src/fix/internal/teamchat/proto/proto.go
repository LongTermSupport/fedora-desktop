// Package proto is the protocol's refusal, as teamchat's proto package has it.
package proto

// RefusalError is a composer's refusal: a reason code and its detail.
type RefusalError struct {
	Reason, Detail string
}

func (r *RefusalError) Error() string { return "teamchat: refused " + r.Reason }
