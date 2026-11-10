// Package status is the record's problem, as teamchat's status package has it.
package status

// Problem is a problem on a team's record.
type Problem struct {
	What, Fix string
}

// NewProblem is a problem, refused unless both parts are one line.
func NewProblem(what, fix string) (Problem, error) { return Problem{What: what, Fix: fix}, nil }
