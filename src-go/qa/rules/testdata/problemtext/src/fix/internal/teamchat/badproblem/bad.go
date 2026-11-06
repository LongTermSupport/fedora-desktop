package badproblem

import (
	"errors"
	"fmt"

	"fix/internal/teamchat/status"
)

// ErrGone is a sentinel: its text is fixed, so a problem may carry it.
var ErrGone = errors.New("the file is gone")

func class(error) string { return "refused" }

func viaNew(err error) (status.Problem, error) {
	return status.NewProblem("cannot: "+err.Error(), "fix") // want `an error's own text in a status problem`
}

func bad(err error) []status.Problem {
	return []status.Problem{
		{What: err.Error(), Fix: "x"},                      // want `an error's own text in a status problem`
		{What: "the leaf: " + err.Error(), Fix: "x"},       // want `an error's own text in a status problem`
		{What: "a", Fix: fmt.Sprintf("see %v", err)},       // want `an error's own text in a status problem`
		{What: fmt.Sprint("the restart: ", err), Fix: "x"}, // want `an error's own text in a status problem`
		{"positional " + err.Error(), "x"},                 // want `an error's own text in a status problem`
	}
}

func pointer(err error) *status.Problem {
	return &status.Problem{What: "w", Fix: "f" + fmt.Errorf("x: %w", err).Error()} // want `an error's own text in a status problem`
}

func good(err error) []status.Problem {
	return []status.Problem{
		{What: "the view: " + ErrGone.Error(), Fix: "x"},
		{What: "the probe: " + class(err), Fix: "x"},
		{What: fmt.Sprintf("%d restarts", 3), Fix: "x"},
	}
}
