// Package records stands for teamchat's records: the two that name a question.
package records

// Human is a HUMAN item.
type Human struct {
	ID       string
	Question *string
	Headline *string
}

// Verified is verify's record.
type Verified struct {
	ID       string
	Question *string
	Headline *string
}

// Notice names no question: never flagged.
type Notice struct{ ID string }
